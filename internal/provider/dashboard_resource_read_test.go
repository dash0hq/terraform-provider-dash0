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
type testDashboardClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testDashboardClient) GetDashboard(_ context.Context, _, _ string) (string, error) {
	return c.getResponse, c.getError
}

func TestDashboardResource_ReadWithDiffs(t *testing.T) {
	// Create test data
	testOrigin := "test-dashboard"
	testDataset := "test-dataset"

	// Original dashboard YAML in state
	originalYaml := `
kind: Dashboard
metadata:
  name: test-dashboard
  dash0Extensions:
    projectId: test-project
spec:
  title: Test Dashboard
  description: Original description
`

	// Test cases for different types of API responses
	tests := []struct {
		name              string
		apiResponseYaml   string
		expectYamlUpdated bool
		expectWarning     bool
	}{
		{
			name: "metadata changes only - no significant diff",
			apiResponseYaml: `
kind: Dashboard
metadata:
  name: test-dashboard
  createdAt: "2023-01-01T00:00:00Z"
  updatedAt: "2023-01-02T00:00:00Z"
  version: 3
  dash0Extensions:
    projectId: different-project
spec:
  title: Test Dashboard
  description: Original description
`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
		{
			name: "significant changes - should update state",
			apiResponseYaml: `
kind: Dashboard
metadata:
  name: test-dashboard
  createdAt: "2023-01-01T00:00:00Z"
  updatedAt: "2023-01-02T00:00:00Z"
  version: 3
spec:
  title: Updated Title
  description: Updated description
`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
		{
			name:              "invalid YAML response - should update and warn",
			apiResponseYaml:   "invalid: : yaml: that: will: fail",
			expectYamlUpdated: true,
			expectWarning:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create the test schema
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
					"dashboard_yaml": schema.StringAttribute{
						Required: true,
					},
					"url": schema.StringAttribute{
						Computed: true,
					},
				},
			}

			// Create a test client that returns the JSON string directly
			testClient := &testDashboardClient{
				getResponse: tc.apiResponseYaml,
			}

			// Create the resource with the test client
			r := &DashboardResource{client: testClient}

			// Create the state object
			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":         tftypes.String,
						"id":             tftypes.String,
						"dataset":        tftypes.String,
						"dashboard_yaml": tftypes.String,
						"url":            tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":         tftypes.NewValue(tftypes.String, testOrigin),
					"id":             tftypes.NewValue(tftypes.String, nil),
					"dataset":        tftypes.NewValue(tftypes.String, testDataset),
					"dashboard_yaml": tftypes.NewValue(tftypes.String, originalYaml),
					"url":            tftypes.NewValue(tftypes.String, "https://app.dash0.com/goto/dashboards?dashboard_id=internal-uuid"),
				},
			)

			// Create the request state
			state := tfsdk.State{
				Raw:    raw,
				Schema: testSchema,
			}

			// Create the request
			req := resource.ReadRequest{
				State: state,
			}

			// Create the response with a copy of the state
			resp := resource.ReadResponse{
				State: state,
			}

			// Call the Read function
			ctx := context.Background()
			r.Read(ctx, req, &resp)

			// Extract the resulting state
			var resultState dashboardModel
			resp.State.Get(ctx, &resultState)

			// Check if the result matches expectations
			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponseYaml, resultState.DashboardYaml.ValueString())
			} else {
				assert.Equal(t, originalYaml, resultState.DashboardYaml.ValueString())
			}

			// Check for warnings
			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
		})
	}
}

// A 404 means the dashboard was deleted out-of-band; Read must drop it from state.
func TestDashboardResource_ReadNotFoundClearsState(t *testing.T) {
	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":         schema.StringAttribute{Computed: true},
			"id":             schema.StringAttribute{Computed: true},
			"dataset":        schema.StringAttribute{Required: true},
			"dashboard_yaml": schema.StringAttribute{Required: true},
			"url":            schema.StringAttribute{Computed: true},
		},
	}
	r := &DashboardResource{client: &testDashboardClient{getError: &dash0.APIError{StatusCode: 404, Status: "404 Not Found"}}}
	raw := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"origin":         tftypes.String,
			"id":             tftypes.String,
			"dataset":        tftypes.String,
			"dashboard_yaml": tftypes.String,
			"url":            tftypes.String,
		}},
		map[string]tftypes.Value{
			"origin":         tftypes.NewValue(tftypes.String, "tf_test"),
			"id":             tftypes.NewValue(tftypes.String, nil),
			"dataset":        tftypes.NewValue(tftypes.String, "x"),
			"dashboard_yaml": tftypes.NewValue(tftypes.String, "x"),
			"url":            tftypes.NewValue(tftypes.String, nil),
		},
	)
	state := tfsdk.State{Raw: raw, Schema: testSchema}
	resp := resource.ReadResponse{State: state}

	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared on 404")
}

// Non-404 errors must still fail the read.
func TestDashboardResource_ReadNonNotFoundStillErrors(t *testing.T) {
	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":         schema.StringAttribute{Computed: true},
			"id":             schema.StringAttribute{Computed: true},
			"dataset":        schema.StringAttribute{Required: true},
			"dashboard_yaml": schema.StringAttribute{Required: true},
			"url":            schema.StringAttribute{Computed: true},
		},
	}
	r := &DashboardResource{client: &testDashboardClient{getError: &dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"}}}
	raw := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"origin":         tftypes.String,
			"id":             tftypes.String,
			"dataset":        tftypes.String,
			"dashboard_yaml": tftypes.String,
			"url":            tftypes.String,
		}},
		map[string]tftypes.Value{
			"origin":         tftypes.NewValue(tftypes.String, "tf_test"),
			"id":             tftypes.NewValue(tftypes.String, nil),
			"dataset":        tftypes.NewValue(tftypes.String, "x"),
			"dashboard_yaml": tftypes.NewValue(tftypes.String, "x"),
			"url":            tftypes.NewValue(tftypes.String, nil),
		},
	)
	state := tfsdk.State{Raw: raw, Schema: testSchema}
	resp := resource.ReadResponse{State: state}

	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	assert.True(t, resp.Diagnostics.HasError(), "500 must surface as an error")
}
