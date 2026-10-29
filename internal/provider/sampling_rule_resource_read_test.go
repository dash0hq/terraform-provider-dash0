package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testSamplingRuleClient is a minimal stand-in for the API client: the drift
// table only needs to control what a GET returns. Like the real wrapper, it
// decodes the server's document into dash0.SamplingDefinition and re-encodes
// it, so fields the typed round trip would drop or reshape are covered.
type testSamplingRuleClient struct {
	client.Client
	serverResponse string
}

func (c *testSamplingRuleClient) GetSamplingRule(_ context.Context, _, _ string) (string, error) {
	var rule dash0.SamplingDefinition
	if err := json.Unmarshal([]byte(c.serverResponse), &rule); err != nil {
		return "", err
	}
	b, err := json.Marshal(rule)
	return string(b), err
}

// TestSamplingRuleResource_ReadWithDiffs is the drift-detection table. It
// exercises Read end to end — the typed round trip, FieldsAbsentFromYAML and
// ResourceYAMLEquivalent — because that combination decides whether a plan
// comes back clean.
func TestSamplingRuleResource_ReadWithDiffs(t *testing.T) {
	const configYaml = `kind: Dash0Sampling
metadata:
  name: keep-production-sample
spec:
  enabled: true
  display:
    name: Keep a production sample
  conditions:
    kind: and
    spec:
      conditions:
        - kind: ottl
          spec:
            ottl: resource.attributes["deployment.environment.name"] == "production"
        - kind: probabilistic
          spec:
            rate: 0.333333333
`
	const serverLabels = `"labels":{"dash0.com/origin":"tf_abc","dash0.com/id":"11111111-1111-1111-1111-111111111111","dash0.com/dataset":"default","dash0.com/source":"terraform","dash0.com/version":"3"}`
	const conditionsJSON = `{"kind":"and","spec":{"conditions":[{"kind":"ottl","spec":{"ottl":"resource.attributes[\"deployment.environment.name\"] == \"production\""}},{"kind":"probabilistic","spec":{"rate":0.333333333}}]}}`

	tests := []struct {
		name              string
		stateYaml         string
		serverResponse    string
		expectYamlUpdated bool
		why               string
	}{
		{
			name:              "server labels added - no drift",
			stateYaml:         configYaml,
			serverResponse:    `{"kind":"Dash0Sampling","metadata":{"name":"keep-production-sample",` + serverLabels + `},"spec":{"enabled":true,"display":{"name":"Keep a production sample"},"conditions":` + conditionsJSON + `}}`,
			expectYamlUpdated: false,
			why: "server-managed labels must not cause drift, and the probabilistic rate must survive the typed " +
				"round trip unrounded: SamplingCondition holds raw JSON, so the rate is never narrowed to float32",
		},
		{
			name:              "metadata.name absent from config, set by the server - no drift",
			stateYaml:         `kind: Dash0Sampling` + "\n" + `spec:` + "\n" + `  enabled: true` + "\n" + `  conditions:` + "\n" + `    kind: error` + "\n" + `    spec: {}` + "\n",
			serverResponse:    `{"kind":"Dash0Sampling","metadata":{"name":"generated-name",` + serverLabels + `},"spec":{"enabled":true,"conditions":{"kind":"error","spec":{}}}}`,
			expectYamlUpdated: false,
			why:               "metadata.name is conditionally ignored when the config omits it",
		},
		{
			name:              "disabled out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    `{"kind":"Dash0Sampling","metadata":{"name":"keep-production-sample",` + serverLabels + `},"spec":{"enabled":false,"display":{"name":"Keep a production sample"},"conditions":` + conditionsJSON + `}}`,
			expectYamlUpdated: true,
			why:               "a rule switched off in the UI must show up in the plan, since spec.enabled is required in the config",
		},
		{
			name:              "condition changed out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    `{"kind":"Dash0Sampling","metadata":{"name":"keep-production-sample",` + serverLabels + `},"spec":{"enabled":true,"display":{"name":"Keep a production sample"},"conditions":{"kind":"error","spec":{}}}}`,
			expectYamlUpdated: true,
			why:               "a real change to the condition tree must be reflected in state",
		},
		{
			name:              "rate limit added out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    `{"kind":"Dash0Sampling","metadata":{"name":"keep-production-sample",` + serverLabels + `},"spec":{"enabled":true,"display":{"name":"Keep a production sample"},"conditions":` + conditionsJSON + `,"rateLimit":{"rate":100}}}`,
			expectYamlUpdated: true,
			why:               "spec.rateLimit is not server-defaulted, so a rate limit the config does not declare is drift",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &testSamplingRuleClient{serverResponse: tc.serverResponse}
			r := &SamplingRuleResource{client: c}

			state := samplingRuleState(strPtr("tf_abc"), strPtr("an-id"), strPtr("default"), strPtr(tc.stateYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			require.False(t, resp.Diagnostics.HasError())
			assert.Equal(t, 0, resp.Diagnostics.WarningsCount(), "a well-formed response must not warn")

			var result samplingRuleModel
			require.False(t, resp.State.Get(context.Background(), &result).HasError())

			if tc.expectYamlUpdated {
				assert.NotEqual(t, tc.stateYaml, result.SamplingRuleYaml.ValueString(), tc.why)
			} else {
				assert.Equal(t, tc.stateYaml, result.SamplingRuleYaml.ValueString(), tc.why)
			}
		})
	}
}
