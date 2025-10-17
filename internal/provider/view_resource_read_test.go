package provider

import (
	"context"
	"testing"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
)

// Custom mock client implementation for this test
type testViewClient struct {
	client.Client
	getResponse *model.ViewResource
	getError    error
}

func (c *testViewClient) GetView(_ context.Context, _, _ string) (*model.ViewResource, error) {
	return c.getResponse, c.getError
}

func TestViewResource_ReadWithDiffs(t *testing.T) {
	// Create test data
	testOrigin := "test-view"
	testDataset := "test-dataset"

	// Original view YAML in state
	originalYaml := `
kind: View
metadata:
  name: test-view
  dash0Extensions: 
    projectId: test-project
spec:
  title: Test View
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
kind: View
metadata:
  name: test-view
  createdAt: "2023-01-01T00:00:00Z"
  updatedAt: "2023-01-02T00:00:00Z"
  version: 3
  dash0Extensions:
    projectId: different-project
spec:
  title: Test View
  description: Original description
`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
		{
			name: "significant changes - should update state",
			apiResponseYaml: `
kind: View
metadata:
  name: test-view
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
					"dataset": schema.StringAttribute{
						Required: true,
					},
					"view_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			}

			// Create a test client
			testClient := &testViewClient{
				getResponse: &model.ViewResource{
					Origin:   types.StringValue(testOrigin),
					Dataset:  types.StringValue(testDataset),
					ViewYaml: types.StringValue(tc.apiResponseYaml),
				},
			}

			// Create the resource with the test client
			r := &ViewResource{client: testClient}

			// Create the state object
			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":    tftypes.String,
						"dataset":   tftypes.String,
						"view_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":    tftypes.NewValue(tftypes.String, testOrigin),
					"dataset":   tftypes.NewValue(tftypes.String, testDataset),
					"view_yaml": tftypes.NewValue(tftypes.String, originalYaml),
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
			var resultState model.ViewResource
			resp.State.Get(ctx, &resultState)

			// Check if the result matches expectations
			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponseYaml, resultState.ViewYaml.ValueString())
			} else {
				assert.Equal(t, originalYaml, resultState.ViewYaml.ValueString())
			}

			// Check for warnings
			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
		})
	}
}
