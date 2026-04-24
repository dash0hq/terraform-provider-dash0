package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Custom mock client implementation for recording rule read tests
type testRecordingRuleClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testRecordingRuleClient) GetRecordingRule(_ context.Context, _, _ string) (string, error) {
	return c.getResponse, c.getError
}

func TestRecordingRuleResource_ReadWithDiffs(t *testing.T) {
	testOrigin := "test-recording-rule"
	testDataset := "test-dataset"

	// Original recording rule YAML in state (user's config, no metadata)
	originalYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - record: test_metric
          expr: sum(rate(http_requests_total[5m]))
          labels:
            env: production
`

	tests := []struct {
		name              string
		apiResponseYaml   string
		expectYamlUpdated bool
		expectWarning     bool
	}{
		{
			name: "metadata changes only - no significant diff",
			apiResponseYaml: `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  labels:
    dash0.com/origin: test-recording-rule
    dash0.com/dataset: test-dataset
    dash0.com/version: "3"
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - record: test_metric
          expr: sum(rate(http_requests_total[5m]))
          labels:
            env: production
`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
		{
			name: "significant content change - should update state",
			apiResponseYaml: `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  labels:
    dash0.com/origin: test-recording-rule
    dash0.com/dataset: test-dataset
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - record: test_metric_updated
          expr: sum(rate(http_requests_total[10m]))
          labels:
            env: production
`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
		{
			name:              "invalid YAML response - should update and warn",
			apiResponseYaml:   `not valid yaml {`,
			expectYamlUpdated: true,
			expectWarning:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testSchema := schema.Schema{
				Attributes: map[string]schema.Attribute{
					"origin": schema.StringAttribute{
						Computed: true,
					},
					"dataset": schema.StringAttribute{
						Required: true,
					},
					"recording_rule_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			}

			testClient := &testRecordingRuleClient{
				getResponse: tc.apiResponseYaml,
			}

			r := &RecordingRuleResource{client: testClient}

			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":              tftypes.String,
						"dataset":             tftypes.String,
						"recording_rule_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":              tftypes.NewValue(tftypes.String, testOrigin),
					"dataset":             tftypes.NewValue(tftypes.String, testDataset),
					"recording_rule_yaml": tftypes.NewValue(tftypes.String, originalYaml),
				},
			)

			state := tfsdk.State{
				Raw:    raw,
				Schema: testSchema,
			}

			req := resource.ReadRequest{
				State: state,
			}

			resp := resource.ReadResponse{
				State: state,
			}

			ctx := context.Background()
			r.Read(ctx, req, &resp)

			var resultState recordingRuleModel
			resp.State.Get(ctx, &resultState)

			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponseYaml, resultState.RecordingRuleYaml.ValueString())
			} else {
				assert.Equal(t, originalYaml, resultState.RecordingRuleYaml.ValueString())
			}

			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
		})
	}
}
