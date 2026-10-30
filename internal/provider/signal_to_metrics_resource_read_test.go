package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testSignalToMetricsClient is a minimal stand-in for the API client: the
// drift table only needs to control what a GET returns. Like the real wrapper,
// it decodes the server's document into dash0.SignalToMetricsDefinition and
// re-encodes it, so fields the typed round trip would drop or reshape are
// covered.
type testSignalToMetricsClient struct {
	client.Client
	serverResponse string
}

func (c *testSignalToMetricsClient) GetSignalToMetrics(_ context.Context, _, _ string) (string, error) {
	var rule dash0.SignalToMetricsDefinition
	if err := json.Unmarshal([]byte(c.serverResponse), &rule); err != nil {
		return "", err
	}
	b, err := json.Marshal(rule)
	return string(b), err
}

// TestSignalToMetricsResource_ReadWithDiffs is the drift-detection table. It
// exercises Read end to end — the typed round trip, the default normalization,
// FieldsAbsentFromYAML and ResourceYAMLEquivalent — because that combination
// decides whether a plan comes back clean.
//
// The server responses mirror what the live API returned when probed: it
// stores spec.output.interval verbatim (60s stays 60s, 1m stays 1m) and echoes
// no default for an omitted spec.targetDatasetMode, spec.output.monotonic,
// spec.output.maxSize or spec.output.count.
func TestSignalToMetricsResource_ReadWithDiffs(t *testing.T) {
	const configYaml = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-errors
spec:
  enabled: true
  display:
    name: Checkout errors
  match:
    signal: spans
    filters:
      - key: service.name
        operator: is
        value: checkout
  output:
    name: checkout_errors
    interval: 60s
`
	const sumConfigYaml = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-amount
spec:
  enabled: true
  match:
    signal: logs
    filters: []
  output:
    name: checkout_amount
    interval: 60s
    type: sum
    value: log.attributes["amount"]
`
	const histogramConfigYaml = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-latency
spec:
  enabled: true
  match:
    signal: spans
    filters: []
  output:
    name: checkout_latency
    interval: 60s
    type: exponential-histogram
    value: "1"
`
	const serverLabels = `"labels":{"dash0.com/origin":"tf_abc","dash0.com/id":"signal_to_metrics_01abc","dash0.com/dataset":"default","dash0.com/source":"terraform","dash0.com/version":"3"}`
	const serverTimestamps = `"dash0.com/created-at":"2026-10-30T09:13:08.959631Z","dash0.com/updated-at":"2026-10-30T09:13:51.514134Z"`
	const metadata = `"metadata":{"name":"checkout-errors",` + serverLabels + `,"annotations":{` + serverTimestamps + `}}`
	const match = `"match":{"signal":"spans","filters":[{"key":"service.name","operator":"is","value":"checkout"}]}`
	const display = `"display":{"name":"Checkout errors"}`
	response := func(specExtra, output string) string {
		return `{"kind":"Dash0SignalToMetrics",` + metadata + `,"spec":{"enabled":true,` + display + `,` + match + specExtra + `,"output":` + output + `}}`
	}
	const output = `{"name":"checkout_errors","interval":"60s"}`

	tests := []struct {
		name              string
		stateYaml         string
		serverResponse    string
		expectYamlUpdated bool
		why               string
	}{
		{
			name:              "server labels and timestamps added - no drift",
			stateYaml:         configYaml,
			serverResponse:    response("", output),
			expectYamlUpdated: false,
			why:               "server-managed labels and timestamp annotations must not cause drift",
		},
		{
			name:              "metadata.name absent from config, set by the server - no drift",
			stateYaml:         "kind: Dash0SignalToMetrics\nspec:\n  enabled: true\n  display:\n    name: Checkout errors\n  match:\n    signal: spans\n    filters:\n      - key: service.name\n        operator: is\n        value: checkout\n  output:\n    name: checkout_errors\n    interval: 60s\n",
			serverResponse:    `{"kind":"Dash0SignalToMetrics","metadata":{"name":"generated",` + serverLabels + `},"spec":{"enabled":true,` + display + `,` + match + `,"output":` + output + `}}`,
			expectYamlUpdated: false,
			why:               "metadata.name is conditionally ignored when the config omits it",
		},
		{
			name:              "disabled out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    `{"kind":"Dash0SignalToMetrics",` + metadata + `,"spec":{"enabled":false,` + display + `,` + match + `,"output":` + output + `}}`,
			expectYamlUpdated: true,
			why:               "a rule switched off in the UI must show up in the plan, since spec.enabled is required in the config",
		},
		{
			name:              "interval stored verbatim - no drift",
			stateYaml:         configYaml,
			serverResponse:    response("", `{"name":"checkout_errors","interval":"60s"}`),
			expectYamlUpdated: false,
			why:               "the server returns 60s as 60s",
		},
		{
			name:              "interval spelled differently - no drift",
			stateYaml:         configYaml,
			serverResponse:    response("", `{"name":"checkout_errors","interval":"1m"}`),
			expectYamlUpdated: false,
			why: "the server does not rewrite 60s as 1m today, and the comparison is duration-aware, so " +
				"it would not drift if a future server did",
		},
		{
			name:              "interval changed out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response("", `{"name":"checkout_errors","interval":"5m"}`),
			expectYamlUpdated: true,
			why:               "a different duration is a real change, not a different spelling",
		},
		{
			name:              "targetDatasetMode omitted on both sides - no drift",
			stateYaml:         configYaml,
			serverResponse:    response("", output),
			expectYamlUpdated: false,
			why:               "the server echoes no default for an omitted targetDatasetMode",
		},
		{
			name:              "targetDatasetMode omitted in config, original on the server - no drift",
			stateYaml:         configYaml,
			serverResponse:    response(`,"targetDatasetMode":"original"`, output),
			expectYamlUpdated: false,
			why:               "original is the server default and is normalized away on both sides",
		},
		{
			name:              "targetDatasetMode original in config, omitted on the server - no drift",
			stateYaml:         configYaml + "  targetDatasetMode: original\n",
			serverResponse:    response("", output),
			expectYamlUpdated: false,
			why:               "an explicit default is equivalent to an omitted field",
		},
		{
			name:              "targetDatasetMode switched to alternative out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(`,"targetDatasetMode":"alternative","alternativeDatasetId":"metrics-only"`, output),
			expectYamlUpdated: true,
			why: "rerouting the metric to another dataset is a real change; only the default is normalized, " +
				"so an omitted field does not hide it",
		},
		{
			name:              "sum without monotonic, server echoes monotonic false - no drift",
			stateYaml:         sumConfigYaml,
			serverResponse:    `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-amount",` + serverLabels + `},"spec":{"enabled":true,"match":{"signal":"logs","filters":[]},"output":{"name":"checkout_amount","interval":"60s","type":"sum","value":"log.attributes[\"amount\"]","monotonic":false}}}`,
			expectYamlUpdated: false,
			why:               "false is the default for monotonic, and normalization drops zero values",
		},
		{
			name:              "sum made monotonic out of band - drift",
			stateYaml:         sumConfigYaml,
			serverResponse:    `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-amount",` + serverLabels + `},"spec":{"enabled":true,"match":{"signal":"logs","filters":[]},"output":{"name":"checkout_amount","interval":"60s","type":"sum","value":"log.attributes[\"amount\"]","monotonic":true}}}`,
			expectYamlUpdated: true,
			why:               "a non-default monotonic the config does not declare is drift",
		},
		{
			name:              "histogram without maxSize or count, server echoes neither - no drift",
			stateYaml:         histogramConfigYaml,
			serverResponse:    `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-latency",` + serverLabels + `},"spec":{"enabled":true,"match":{"signal":"spans","filters":[]},"output":{"name":"checkout_latency","interval":"60s","type":"exponential-histogram","value":"1"}}}`,
			expectYamlUpdated: false,
			why:               "the server stores no default maxSize (160) or count (\"1\") when they are omitted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &testSignalToMetricsClient{serverResponse: tc.serverResponse}
			r := &SignalToMetricsResource{client: c}

			state := signalToMetricsState(strPtr("tf_abc"), strPtr("an-id"), strPtr("default"), strPtr(tc.stateYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			assert.Equal(t, 0, resp.Diagnostics.WarningsCount(), "a well-formed response must not warn")

			var result signalToMetricsModel
			require.False(t, resp.State.Get(context.Background(), &result).HasError())

			if tc.expectYamlUpdated {
				assert.NotEqual(t, tc.stateYaml, result.SignalToMetricsYaml.ValueString(), tc.why)
			} else {
				assert.Equal(t, tc.stateYaml, result.SignalToMetricsYaml.ValueString(), tc.why)
			}
		})
	}
}

// TestSignalToMetricsResource_PlanModifierNormalizesDefaults pins the plan
// side of the targetDatasetMode normalization. Read alone is not enough: after
// an import the state holds the API document verbatim, and the plan modifier
// is what compares it with a config that omits the default.
func TestSignalToMetricsResource_PlanModifierNormalizesDefaults(t *testing.T) {
	r := &SignalToMetricsResource{}
	schemaResp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	modifiers := schemaResp.Schema.Attributes["signal_to_metrics_yaml"].(schema.StringAttribute).PlanModifiers
	require.Len(t, modifiers, 1)

	const configYaml = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-errors
spec:
  enabled: true
  match:
    signal: spans
    filters: []
  output:
    name: checkout_errors
    interval: 60s
`
	for _, tc := range []struct {
		name      string
		stateYaml string
		keepState bool
	}{
		{
			name:      "state has the default original",
			stateYaml: `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-errors"},"spec":{"enabled":true,"targetDatasetMode":"original","match":{"signal":"spans","filters":[]},"output":{"name":"checkout_errors","interval":"60s"}}}`,
			keepState: true,
		},
		{
			name:      "state routes to another dataset",
			stateYaml: `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-errors"},"spec":{"enabled":true,"targetDatasetMode":"both","alternativeDatasetId":"metrics-only","match":{"signal":"spans","filters":[]},"output":{"name":"checkout_errors","interval":"60s"}}}`,
			keepState: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.StringRequest{
				ConfigValue: types.StringValue(configYaml),
				StateValue:  types.StringValue(tc.stateYaml),
				PlanValue:   types.StringValue(configYaml),
			}
			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
			modifiers[0].PlanModifyString(context.Background(), req, resp)

			if tc.keepState {
				assert.Equal(t, tc.stateYaml, resp.PlanValue.ValueString(), "an equivalent state must suppress the diff")
			} else {
				assert.Equal(t, configYaml, resp.PlanValue.ValueString(), "a real difference must reach the plan")
			}
		})
	}
}

func TestNormalizeSignalToMetricsDefaults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		document string
		removed  bool
	}{
		{"original is removed", "spec:\n  targetDatasetMode: original\n", true},
		{"alternative is kept", "spec:\n  targetDatasetMode: alternative\n", false},
		{"both is kept", "spec:\n  targetDatasetMode: both\n", false},
		{"absent stays absent", "spec:\n  enabled: true\n", false},
		{"no spec", "kind: Dash0SignalToMetrics\n", false},
		{"invalid YAML is returned unchanged", "spec: [", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSignalToMetricsDefaults(tc.document)
			if tc.removed {
				assert.NotContains(t, got, "targetDatasetMode")
			} else {
				assert.Equal(t, tc.document, got)
			}
		})
	}
}
