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

// Custom mock client implementation for check rule read tests
type testCheckRuleClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testCheckRuleClient) GetCheckRule(_ context.Context, _, _ string) (string, error) {
	return c.getResponse, c.getError
}

func TestCheckRuleResource_ReadWithDiffs(t *testing.T) {
	testOrigin := "test-check-rule"
	testDataset := "test-dataset"

	// Original check rule YAML in state (user's config, no metadata)
	originalYaml := `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: "vector(1)"
          for: 1m0s
          annotations:
            summary: "test"
          labels:
            severity: warning
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
    dash0.com/origin: test-check-rule
    dash0.com/dataset: test-dataset
    dash0.com/version: "3"
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: "vector(1)"
          for: 1m0s
          annotations:
            summary: "test"
          labels:
            severity: warning
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
    dash0.com/origin: test-check-rule
    dash0.com/dataset: test-dataset
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: "vector(0)"
          for: 1m0s
          annotations:
            summary: "test"
          labels:
            severity: warning
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
					"check_rule_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			}

			testClient := &testCheckRuleClient{
				getResponse: tc.apiResponseYaml,
			}

			r := &CheckRuleResource{client: testClient}

			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":          tftypes.String,
						"dataset":         tftypes.String,
						"check_rule_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":          tftypes.NewValue(tftypes.String, testOrigin),
					"dataset":         tftypes.NewValue(tftypes.String, testDataset),
					"check_rule_yaml": tftypes.NewValue(tftypes.String, originalYaml),
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

			var resultState checkRuleModel
			resp.State.Get(ctx, &resultState)

			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponseYaml, resultState.CheckRuleYaml.ValueString())
			} else {
				assert.Equal(t, originalYaml, resultState.CheckRuleYaml.ValueString())
			}

			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
		})
	}
}
