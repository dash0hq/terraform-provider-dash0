package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testTimeSeriesAggregationClient is a minimal stand-in for the API client: the
// drift table only needs to control what a GET returns.
type testTimeSeriesAggregationClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testTimeSeriesAggregationClient) GetTimeSeriesAggregation(_ context.Context, _, _ string) (string, error) {
	return c.getResponse, c.getError
}

// TestTimeSeriesAggregationResource_ReadWithDiffs is the drift-detection table.
// It exercises Read end to end — FieldsAbsentFromYAML plus
// ResourceYAMLEquivalent against the resource-scoped conditionally-ignored
// field list — because that combination, not either half alone, is what decides
// whether a plan comes back clean.
func TestTimeSeriesAggregationResource_ReadWithDiffs(t *testing.T) {
	tests := []struct {
		name              string
		stateYaml         string
		apiResponse       string
		expectYamlUpdated bool
		why               string
	}{
		{
			// The silent-revert scenario this resource exists to avoid. The
			// provider always sends a value for spec.enabled (non-pointer
			// bool, no omitempty), so an aggregation enabled outside Terraform
			// must surface as drift rather than be switched back off by the
			// next unrelated apply.
			//
			// The fixture uses `true` deliberately: ResourceYAMLEquivalent
			// already strips zero-valued fields absent from the config, so an
			// API response of `enabled: false` is equivalent either way. Only
			// a non-zero value discriminates — and only while spec.enabled
			// stays OUT of the conditionally-ignored list.
			name: "spec.enabled absent from config, enabled out of band - drift",
			stateYaml: `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`,
			apiResponse:       `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"rollup"},"spec":{"enabled":true,"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"5m"}}}`,
			expectYamlUpdated: true,
			why:               "an out-of-band enable must be reported, not silently reverted by the next apply",
		},
		{
			// Same story for spec.priority, which the server defaults to 2.
			// (It is a *int with omitempty, so it is absent on the wire yet
			// still defaulted — the API client's "defaults to 0" doc comment is
			// wrong.)
			name: "spec.priority omitted from config, defaulted to 2 by the server - equivalent",
			stateYaml: `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`,
			apiResponse:       `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"rollup"},"spec":{"enabled":true,"priority":2,"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"5m"}}}`,
			expectYamlUpdated: false,
			why:               "a server default for a field the user never declared must not overwrite state",
		},
		{
			// The conditional ignore is conditional: once the user declares
			// spec.enabled, an out-of-band disable must still be detected.
			name: "spec.enabled declared as true, server reports false - drift",
			stateYaml: `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`,
			apiResponse:       `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"rollup"},"spec":{"enabled":false,"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"5m"}}}`,
			expectYamlUpdated: true,
			why:               "a declared spec.enabled must keep participating in drift detection",
		},
		{
			name: "server-managed metadata.labels differ - equivalent",
			stateYaml: `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`,
			apiResponse:       `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"rollup","labels":{"dash0.com/origin":"tf_abc","dash0.com/id":"11111111-1111-1111-1111-111111111111","dash0.com/dataset":"default","dash0.com/version":"3","dash0.com/source":"terraform"}},"spec":{"enabled":true,"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"5m"}}}`,
			expectYamlUpdated: false,
			why:               "the whole metadata.labels subtree is server-managed and stripped before comparison",
		},
		{
			name: "spec.sample.interval changed out of band - drift",
			stateYaml: `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`,
			apiResponse:       `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"rollup"},"spec":{"enabled":true,"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"30m"}}}`,
			expectYamlUpdated: true,
			why:               "a real spec change must be reflected in state",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &TimeSeriesAggregationResource{
				client: &testTimeSeriesAggregationClient{getResponse: tc.apiResponse},
			}

			state := tsaState(strPtr("tf_abc"), strPtr("an-id"), strPtr("default"), strPtr(tc.stateYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			require.False(t, resp.Diagnostics.HasError())
			assert.Equal(t, 0, resp.Diagnostics.WarningsCount(), "a well-formed response must not warn")

			var result timeSeriesAggregationModel
			require.False(t, resp.State.Get(context.Background(), &result).HasError())

			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponse, result.TimeSeriesAggregationYaml.ValueString(), tc.why)
			} else {
				assert.Equal(t, tc.stateYaml, result.TimeSeriesAggregationYaml.ValueString(), tc.why)
			}
		})
	}
}

// TestTimeSeriesAggregation_ConditionallyIgnoredFieldsAreResourceScoped is a
// guard, not a behavior test. converter.ConditionallyIgnoredFields is read by
// seven other resources and by the shared plan modifier. dash0_synthetic_check's
// spec.enabled is a required non-pointer bool, so widening the global with
// "spec.enabled" would silently stop that resource from detecting an
// out-of-band disable. The time series aggregation list must stay local.
func TestTimeSeriesAggregation_ConditionallyIgnoredFieldsAreResourceScoped(t *testing.T) {
	assert.NotContains(t, converter.ConditionallyIgnoredFields, "spec.enabled",
		"spec.enabled must not be added to the shared global; it would disable drift detection for dash0_synthetic_check")
	assert.NotContains(t, converter.ConditionallyIgnoredFields, "spec.priority",
		"spec.priority must not be added to the shared global; scope it to dash0_time_series_aggregation instead")

	// ...and the resource-local list must be the global plus spec.priority alone.
	assert.Subset(t, timeSeriesAggregationConditionallyIgnoredFields, converter.ConditionallyIgnoredFields)
	assert.Contains(t, timeSeriesAggregationConditionallyIgnoredFields, "spec.priority")
	assert.NotContains(t, timeSeriesAggregationConditionallyIgnoredFields, "spec.enabled",
		"spec.enabled is required by ValidateConfig and must stay in drift detection: the provider always "+
			"sends a value for it, so ignoring it would let an unrelated apply silently disable an "+
			"aggregation someone enabled outside Terraform")
	assert.Len(t, timeSeriesAggregationConditionallyIgnoredFields, len(converter.ConditionallyIgnoredFields)+1)
}
