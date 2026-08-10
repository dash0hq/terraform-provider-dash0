package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Custom mock client implementation for this test
type testSLOClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testSLOClient) GetSLO(_ context.Context, _, _ string) (string, error) {
	return c.getResponse, c.getError
}

func TestSLOResource_ReadWithDiffs(t *testing.T) {
	// Create test data
	baseYAML := `apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-availability
spec:
  service: checkout
  budgetingMethod: Occurrences
  objectives:
    - displayName: 99% availability
      target: 0.99
`

	yamlWithMetadataChanges := `apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-availability
  labels:
    dash0.com/id: "test-uuid"
    dash0.com/version: "2"
  annotations:
    dash0.com/created-at: "2024-01-01T00:00:00Z"
    dash0.com/updated-at: "2024-01-02T00:00:00Z"
spec:
  service: checkout
  budgetingMethod: Occurrences
  objectives:
    - displayName: 99% availability
      target: 0.99
`

	yamlWithSignificantChanges := `apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-availability
spec:
  service: checkout
  budgetingMethod: Occurrences
  objectives:
    - displayName: 99.5% availability
      target: 0.995
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

			// Create mock client that returns string directly
			mockClient := &testSLOClient{
				getResponse: tt.apiResponse,
			}

			// Create resource with mock client
			r := &SLOResource{
				client: mockClient,
			}

			testURL := "https://app.dash0.com/goto/alerting/slos/details?slo_id=internal-uuid"

			// Setup request with current state
			req := resource.ReadRequest{
				State: tfsdk.State{
					Raw: tftypes.NewValue(tftypes.Object{
						AttributeTypes: map[string]tftypes.Type{
							"origin":   tftypes.String,
							"id":       tftypes.String,
							"dataset":  tftypes.String,
							"slo_yaml": tftypes.String,
							"url":      tftypes.String,
						},
					}, map[string]tftypes.Value{
						"origin":   tftypes.NewValue(tftypes.String, "test-origin"),
						"id":       tftypes.NewValue(tftypes.String, "test-id"),
						"dataset":  tftypes.NewValue(tftypes.String, "test-dataset"),
						"slo_yaml": tftypes.NewValue(tftypes.String, tt.currentState),
						"url":      tftypes.NewValue(tftypes.String, testURL),
					}),
					Schema: testSLOSchema(),
				},
			}

			resp := &resource.ReadResponse{
				State: tfsdk.State{
					Schema: testSLOSchema(),
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
				var state sloModel
				resp.State.Get(ctx, &state)

				if tt.expectStateUpdate {
					assert.Equal(t, tt.apiResponse, state.SLOYaml.ValueString(),
						"State should have been updated with API response")
				} else {
					assert.Equal(t, tt.currentState, state.SLOYaml.ValueString(),
						"State should not have been updated")
				}

				// id and URL are carried over from prior state (Read does not re-resolve them).
				assert.Equal(t, "test-id", state.ID.ValueString())
				assert.Equal(t, testURL, state.URL.ValueString())
			}
		})
	}
}
