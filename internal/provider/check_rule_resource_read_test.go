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
		stateYaml         string
		apiResponseYaml   string
		expectYamlUpdated bool
		expectWarning     bool
	}{
		{
			// Regression test for the plan-stability bug this same rollout
			// surfaced: the API always returns top-level metadata.annotations
			// already merged into the rule (dash0hq/dash0-api-client-go#29).
			// Without merging on this side too before comparing,
			// ResourceYAMLEquivalent sees the top-level annotation on one side
			// and the rule-level annotation on the other and reports drift,
			// so state gets overwritten with the API's rule-annotation shape.
			// The *next* plan then diffs that against the config, which still
			// has the annotation at the top level, forever. Confirmed this
			// case fails without converter.MoveTopLevelAnnotationsIntoRules in Read.
			name: "top-level annotation merged by the API - no significant diff",
			stateYaml: `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  annotations:
    runbook_url: "https://runbooks.example.com/checkout"
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: "vector(1)"
          for: 1m0s
          labels:
            severity: warning
`,
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
          expr: "vector(1)"
          for: 1m0s
          annotations:
            runbook_url: "https://runbooks.example.com/checkout"
          labels:
            severity: warning
`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
		{
			// dash0.com/sharing is the one metadata annotation normalization
			// preserves, so before MoveTopLevelAnnotationsIntoRules removed the
			// top-level map it stayed on both sides in different shapes and
			// never compared equal, drifting on every plan.
			name: "top-level dash0.com/sharing - no significant diff",
			stateYaml: `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  annotations:
    dash0.com/sharing: "team:team_01abc"
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: "vector(1)"
          for: 1m0s
          labels:
            severity: warning
`,
			apiResponseYaml: `
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  labels:
    dash0.com/origin: test-check-rule
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: "vector(1)"
          for: 1m0s
          annotations:
            dash0.com/sharing: "team:team_01abc"
          labels:
            severity: warning
`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
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
					"id": schema.StringAttribute{
						Computed: true,
					},
					"dataset": schema.StringAttribute{
						Required: true,
					},
					"check_rule_yaml": schema.StringAttribute{
						Required: true,
					},
					"url": schema.StringAttribute{
						Computed: true,
					},
				},
			}

			testClient := &testCheckRuleClient{
				getResponse: tc.apiResponseYaml,
			}

			r := &CheckRuleResource{client: testClient}

			testURL := "https://app.dash0.com/goto/alerting/check-rules?check_rule_id=internal-uuid"

			stateYaml := tc.stateYaml
			if stateYaml == "" {
				stateYaml = originalYaml
			}

			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":          tftypes.String,
						"id":              tftypes.String,
						"dataset":         tftypes.String,
						"check_rule_yaml": tftypes.String,
						"url":             tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":          tftypes.NewValue(tftypes.String, testOrigin),
					"id":              tftypes.NewValue(tftypes.String, nil),
					"dataset":         tftypes.NewValue(tftypes.String, testDataset),
					"check_rule_yaml": tftypes.NewValue(tftypes.String, stateYaml),
					"url":             tftypes.NewValue(tftypes.String, testURL),
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
				// When equivalent, Read must keep the original state verbatim
				// (the merge in mergeTopLevelAnnotationsIntoRules is
				// comparison-only and must not leak into what's written back).
				assert.Equal(t, stateYaml, resultState.CheckRuleYaml.ValueString())
			}

			// URL is carried over from prior state (Read does not re-resolve it).
			assert.Equal(t, testURL, resultState.URL.ValueString())

			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
		})
	}
}
