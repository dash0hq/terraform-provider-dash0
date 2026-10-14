package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Custom mock client implementation for this test
type testSyntheticCheckClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testSyntheticCheckClient) GetSyntheticCheck(_ context.Context, _, _ string) (string, error) {
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

	// API response with permissions added by the API (JSON format, matching real API behavior).
	// The API stores permissions in a separate table and enriches the response on retrieval.
	apiResponseWithPermissions := `{"kind":"Dash0SyntheticCheck","metadata":{"annotations":{},"labels":{"dash0.com/dataset":"test-dataset","dash0.com/id":"test-uuid","dash0.com/origin":"tf_test-origin","dash0.com/version":"1"},"name":"test-check"},"spec":{"enabled":true,"permissions":[{"actions":["synthetic_check:read","synthetic_check:delete"],"role":"admin"},{"actions":["synthetic_check:read"],"role":"basic_member"}],"plugin":{"kind":"http","spec":{"request":{"url":"https://test.example.com"}}}}}`

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

			// Create mock client that returns string directly
			mockClient := &testSyntheticCheckClient{
				getResponse: tt.apiResponse,
			}

			// Create resource with mock client
			r := &SyntheticCheckResource{
				client: mockClient,
			}

			testURL := "https://app.dash0.com/goto/alerting/synthetics?check_id=internal-uuid"

			// Setup request with current state
			req := resource.ReadRequest{
				State: tfsdk.State{
					Raw: tftypes.NewValue(tftypes.Object{
						AttributeTypes: map[string]tftypes.Type{
							"origin":               tftypes.String,
							"id":                   tftypes.String,
							"dataset":              tftypes.String,
							"synthetic_check_yaml": tftypes.String,
							"url":                  tftypes.String,
						},
					}, map[string]tftypes.Value{
						"origin":               tftypes.NewValue(tftypes.String, "test-origin"),
						"id":                   tftypes.NewValue(tftypes.String, nil),
						"dataset":              tftypes.NewValue(tftypes.String, "test-dataset"),
						"synthetic_check_yaml": tftypes.NewValue(tftypes.String, tt.currentState),
						"url":                  tftypes.NewValue(tftypes.String, testURL),
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
				var state syntheticCheckModel
				resp.State.Get(ctx, &state)

				if tt.expectStateUpdate {
					assert.Equal(t, tt.apiResponse, state.SyntheticCheckYaml.ValueString(),
						"State should have been updated with API response")
				} else {
					assert.Equal(t, tt.currentState, state.SyntheticCheckYaml.ValueString(),
						"State should not have been updated")
				}

				// URL is carried over from prior state (Read does not re-resolve it).
				assert.Equal(t, testURL, state.URL.ValueString())
			}
		})
	}
}

func testSyntheticCheckReadWithError(t *testing.T, getErr error) resource.ReadResponse {
	t.Helper()
	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":               schema.StringAttribute{Computed: true},
			"id":                   schema.StringAttribute{Computed: true},
			"dataset":              schema.StringAttribute{Required: true},
			"synthetic_check_yaml": schema.StringAttribute{Required: true},
			"url":                  schema.StringAttribute{Computed: true},
		},
	}
	raw := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"origin":               tftypes.String,
			"id":                   tftypes.String,
			"dataset":              tftypes.String,
			"synthetic_check_yaml": tftypes.String,
			"url":                  tftypes.String,
		}},
		map[string]tftypes.Value{
			"origin":               tftypes.NewValue(tftypes.String, "x"),
			"id":                   tftypes.NewValue(tftypes.String, nil),
			"dataset":              tftypes.NewValue(tftypes.String, "x"),
			"synthetic_check_yaml": tftypes.NewValue(tftypes.String, "x"),
			"url":                  tftypes.NewValue(tftypes.String, "x"),
		},
	)
	state := tfsdk.State{Raw: raw, Schema: testSchema}
	r := &SyntheticCheckResource{client: &testSyntheticCheckClient{getError: getErr}}
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	return resp
}

// A 404 means the asset was deleted out-of-band; Read must clear state, not error.
func TestSyntheticCheckResource_ReadNotFoundClearsState(t *testing.T) {
	resp := testSyntheticCheckReadWithError(t, &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})
	assert.False(t, resp.Diagnostics.HasError(), "404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared on 404")
}

// Other errors must still surface.
func TestSyntheticCheckResource_ReadNonNotFoundStillErrors(t *testing.T) {
	resp := testSyntheticCheckReadWithError(t, &dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"})
	assert.True(t, resp.Diagnostics.HasError())
}
