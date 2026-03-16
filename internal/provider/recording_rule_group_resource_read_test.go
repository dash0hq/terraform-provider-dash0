// SPDX-FileCopyrightText: Copyright 2023-2026 Dash0 Inc.

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

type testRecordingRuleGroupClient struct {
	client.Client
	getResponse *model.RecordingRuleGroup
	getError    error
}

func (c *testRecordingRuleGroupClient) GetRecordingRuleGroup(_ context.Context, _, _ string) (*model.RecordingRuleGroup, error) {
	return c.getResponse, c.getError
}

func TestRecordingRuleGroupResource_ReadWithDiffs(t *testing.T) {
	baseYAML := `
kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: true
  display:
    name: HTTP Metrics
  interval: 1m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])
      labels:
        env: production
`

	yamlWithMetadataChanges := `
kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
  createdAt: "2024-01-01T00:00:00Z"
  updatedAt: "2024-01-02T00:00:00Z"
  version: 3
spec:
  enabled: true
  display:
    name: HTTP Metrics
  interval: 1m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])
      labels:
        env: production
`

	yamlWithSignificantChanges := `
kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: false
  display:
    name: Updated HTTP Metrics
  interval: 2m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[10m])
`

	// API response with permissions added by the API (JSON format, matching real API behavior).
	apiResponseWithPermissions := `{"kind":"Dash0RecordingRuleGroup","metadata":{"annotations":{},"labels":{"dash0.com/dataset":"test-dataset","dash0.com/origin":"tf_test-origin","dash0.com/version":"1"},"name":"http_metrics"},"spec":{"enabled":true,"display":{"name":"HTTP Metrics"},"interval":"1m","permissions":[{"actions":["recording_rule_group:read","recording_rule_group:delete"],"role":"admin"}],"rules":[{"record":"http_requests_total:rate5m","expression":"rate(http_requests_total[5m])","labels":{"env":"production"}}]}}`

	tests := []struct {
		name              string
		currentState      string
		apiResponse       string
		expectStateUpdate bool
		expectWarning     bool
	}{
		{
			name:              "metadata changes only - no significant diff",
			currentState:      baseYAML,
			apiResponse:       yamlWithMetadataChanges,
			expectStateUpdate: false,
			expectWarning:     false,
		},
		{
			name:              "API adds permissions - no significant diff",
			currentState:      baseYAML,
			apiResponse:       apiResponseWithPermissions,
			expectStateUpdate: false,
			expectWarning:     false,
		},
		{
			name:              "significant changes - should update state",
			currentState:      baseYAML,
			apiResponse:       yamlWithSignificantChanges,
			expectStateUpdate: true,
			expectWarning:     false,
		},
		{
			name:              "invalid YAML response - should update and warn",
			currentState:      baseYAML,
			apiResponse:       "invalid: : : yaml",
			expectStateUpdate: true,
			expectWarning:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			mockClient := &testRecordingRuleGroupClient{
				getResponse: &model.RecordingRuleGroup{
					Origin:                 types.StringValue("test-origin"),
					Dataset:                types.StringValue("test-dataset"),
					RecordingRuleGroupYaml: types.StringValue(tt.apiResponse),
				},
			}

			r := &RecordingRuleGroupResource{
				client: mockClient,
			}

			testSchema := schema.Schema{
				Attributes: map[string]schema.Attribute{
					"origin": schema.StringAttribute{
						Computed: true,
					},
					"dataset": schema.StringAttribute{
						Required: true,
					},
					"recording_rule_group_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			}

			req := resource.ReadRequest{
				State: tfsdk.State{
					Raw: tftypes.NewValue(tftypes.Object{
						AttributeTypes: map[string]tftypes.Type{
							"origin":                    tftypes.String,
							"dataset":                   tftypes.String,
							"recording_rule_group_yaml": tftypes.String,
						},
					}, map[string]tftypes.Value{
						"origin":                    tftypes.NewValue(tftypes.String, "test-origin"),
						"dataset":                   tftypes.NewValue(tftypes.String, "test-dataset"),
						"recording_rule_group_yaml": tftypes.NewValue(tftypes.String, tt.currentState),
					}),
					Schema: testSchema,
				},
			}

			resp := &resource.ReadResponse{
				State: tfsdk.State{
					Schema: testSchema,
				},
			}

			r.Read(ctx, req, resp)

			if tt.expectWarning {
				assert.True(t, resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() > 0,
					"Expected warning or error in diagnostics")
			} else {
				assert.False(t, resp.Diagnostics.HasError(),
					"Unexpected error in diagnostics")
			}

			if !resp.Diagnostics.HasError() {
				var state model.RecordingRuleGroup
				resp.State.Get(ctx, &state)

				if tt.expectStateUpdate {
					assert.Equal(t, tt.apiResponse, state.RecordingRuleGroupYaml.ValueString(),
						"State should have been updated with API response")
				} else {
					assert.Equal(t, tt.currentState, state.RecordingRuleGroupYaml.ValueString(),
						"State should not have been updated")
				}
			}
		})
	}
}
