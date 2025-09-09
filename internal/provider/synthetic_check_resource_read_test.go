package provider

import (
	"context"
	"testing"

	"github.com/dash0/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
)

// Custom mock client implementation for this test
type testSyntheticCheckClient struct {
	client.Client
	getResponse *model.SyntheticCheckResourceModel
	getError    error
}

func (c *testSyntheticCheckClient) GetSyntheticCheck(_ context.Context, _, _ string) (*model.SyntheticCheckResourceModel, error) {
	return c.getResponse, c.getError
}

func TestSyntheticCheckResource_ReadWithDiffs(t *testing.T) {
	// Create test data
	baseYAML := `
kind: Dash0SyntheticCheck
metadata:
  name: test-check
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://test.example.com
`

	yamlWithMetadataChanges := `
kind: Dash0SyntheticCheck
metadata:
  name: test-check
  createdAt: "2024-01-01T00:00:00Z"
  updatedAt: "2024-01-02T00:00:00Z"
  version: 2
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://test.example.com
`

	yamlWithSignificantChanges := `
kind: Dash0SyntheticCheck
metadata:
  name: test-check
spec:
  enabled: false
  plugin:
    kind: http
    spec:
      request:
        url: https://different.example.com
`

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

			// Create mock client
			mockClient := &testSyntheticCheckClient{
				getResponse: &model.SyntheticCheckResourceModel{
					Origin:             types.StringValue("test-origin"),
					Dataset:            types.StringValue("test-dataset"),
					SyntheticCheckYaml: types.StringValue(tt.apiResponse),
				},
			}

			// Create resource with mock client
			r := &SyntheticCheckResource{
				client: mockClient,
			}

			// Setup request with current state
			req := resource.ReadRequest{
				State: tfsdk.State{
					Raw: tftypes.NewValue(tftypes.Object{
						AttributeTypes: map[string]tftypes.Type{
							"origin":               tftypes.String,
							"dataset":              tftypes.String,
							"synthetic_check_yaml": tftypes.String,
						},
					}, map[string]tftypes.Value{
						"origin":               tftypes.NewValue(tftypes.String, "test-origin"),
						"dataset":              tftypes.NewValue(tftypes.String, "test-dataset"),
						"synthetic_check_yaml": tftypes.NewValue(tftypes.String, tt.currentState),
					}),
					Schema: testSyntheticCheckSchema(),
				},
			}

			resp := &resource.ReadResponse{
				State: tfsdk.State{
					Schema: testSyntheticCheckSchema(),
				},
			}

			// Execute Read
			r.Read(ctx, req, resp)

			// Check diagnostics
			if tt.expectWarning {
				assert.True(t, resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() > 0,
					"Expected warning or error in diagnostics")
			} else {
				assert.False(t, resp.Diagnostics.HasError(),
					"Unexpected error in diagnostics")
			}

			// Verify state update behavior
			if !resp.Diagnostics.HasError() {
				var state model.SyntheticCheckResourceModel
				resp.State.Get(ctx, &state)

				if tt.expectStateUpdate {
					assert.Equal(t, tt.apiResponse, state.SyntheticCheckYaml.ValueString(),
						"State should have been updated with API response")
				} else {
					assert.Equal(t, tt.currentState, state.SyntheticCheckYaml.ValueString(),
						"State should not have been updated")
				}
			}
		})
	}
}
