package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	customplanmodifier "github.com/dash0hq/terraform-provider-dash0/internal/provider/planmodifier"
)

func TestDashboardResource_Metadata(t *testing.T) {
	r := &DashboardResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)

	assert.Equal(t, "dash0_dashboard", resp.TypeName)
}

func TestDashboardResource_Schema(t *testing.T) {
	r := &DashboardResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)
	assert.Contains(t, resp.Schema.Description, "Manages a Dash0 Dashboard.")

	// Verify schema attributes
	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "dataset")
	assert.Contains(t, resp.Schema.Attributes, "dashboard_yaml")

	// Check specific attribute properties
	assert.True(t, resp.Schema.Attributes["origin"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Required)
	assert.True(t, resp.Schema.Attributes["dashboard_yaml"].(schema.StringAttribute).Required)
}

func TestDashboardResource_Configure(t *testing.T) {
	r := &DashboardResource{}
	client := &MockClient{}

	// Test with nil provider data
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)
	assert.Nil(t, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// Test with valid provider data
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client}, resp)
	assert.Equal(t, client, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// Test with invalid provider data
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "invalid"}, resp)
	assert.True(t, resp.Diagnostics.HasError())
}

func TestDashboardResource_Create(t *testing.T) {
	mockClient := new(MockClient)
	r := &DashboardResource{client: mockClient}

	// Setup test data
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"
	testDataset := "test-dataset"

	// Setup plan
	plan := tfsdk.Plan{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":         tftypes.NewValue(tftypes.String, ""),
			"dataset":        tftypes.NewValue(tftypes.String, testDataset),
			"dashboard_yaml": tftypes.NewValue(tftypes.String, testYaml),
		}),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin": schema.StringAttribute{
					Computed: true,
				},
				"dataset": schema.StringAttribute{
					Required: true,
				},
				"dashboard_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	// Setup state
	state := tfsdk.State{
		Schema: plan.Schema,
	}

	// Setup request and response
	req := resource.CreateRequest{
		Plan: plan,
	}
	resp := resource.CreateResponse{
		State: state,
	}

	// Setup mock expectations - CreateDashboard(ctx, origin, jsonBody, dataset)
	mockClient.On("CreateDashboard", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Execute the create operation
	r.Create(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
}

func TestDashboardResource_Read(t *testing.T) {
	mockClient := new(MockClient)
	r := &DashboardResource{client: mockClient}

	// Setup test data
	testOrigin := "test-origin"
	testDataset := "test-dataset"
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"

	// Create state schema

	stateSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Computed: true,
			},
			"dataset": schema.StringAttribute{
				Required: true,
			},
			"dashboard_yaml": schema.StringAttribute{
				Required: true,
			},
		},
	}

	// Setup state
	state := tfsdk.State{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":         tftypes.NewValue(tftypes.String, testOrigin),
			"dataset":        tftypes.NewValue(tftypes.String, testDataset),
			"dashboard_yaml": tftypes.NewValue(tftypes.String, "old yaml"),
		}),
		Schema: stateSchema,
	}

	// Setup request and response
	req := resource.ReadRequest{
		State: state,
	}
	resp := resource.ReadResponse{
		State: state,
	}

	// Setup mock expectations - GetDashboard(ctx, origin, dataset) returns (string, error)
	mockClient.On("GetDashboard", mock.Anything, testOrigin, testDataset).Return(
		testYaml,
		nil,
	)

	// Execute the read operation
	r.Read(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	// Create a new state object to verify
	var resultState dashboardModel
	diags := resp.State.Get(context.Background(), &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")

	assert.Equal(t, testOrigin, resultState.Origin.ValueString())
	assert.Equal(t, testDataset, resultState.Dataset.ValueString())
	assert.Equal(t, testYaml, resultState.DashboardYaml.ValueString())

	// Test with API error
	mockClient = new(MockClient)
	r = &DashboardResource{client: mockClient}
	mockClient.On("GetDashboard", mock.Anything, testOrigin, testDataset).Return(
		"",
		errors.New("API error"),
	)

	resp = resource.ReadResponse{
		State: state,
	}
	r.Read(context.Background(), req, &resp)
	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

func TestDashboardResource_Update(t *testing.T) {
	mockClient := new(MockClient)
	_ = mockClient

	// Setup test data
	testOrigin := "test-origin"
	testDataset := "test-dataset"
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"
	_ = testYaml

	// Test 1: Update dashboard YAML only (no dataset change)
	t.Run("update yaml only", func(t *testing.T) {
		mockClient := new(MockClient)
		r := &DashboardResource{client: mockClient}

		// Create state
		state := tfsdk.State{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":         tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":        tftypes.NewValue(tftypes.String, testDataset),
				"dashboard_yaml": tftypes.NewValue(tftypes.String, testYaml),
			}),
			Schema: schema.Schema{
				Attributes: map[string]schema.Attribute{
					"origin": schema.StringAttribute{
						Computed: true,
					},
					"dataset": schema.StringAttribute{
						Required: true,
					},
					"dashboard_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			},
		}

		updatedYaml := testYaml + "\n  description: Updated dashboard"

		// Create plan with updated YAML
		plan := tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":         tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":        tftypes.NewValue(tftypes.String, testDataset),
				"dashboard_yaml": tftypes.NewValue(tftypes.String, updatedYaml),
			}),
			Schema: state.Schema,
		}

		// Setup request and response
		req := resource.UpdateRequest{
			State: state,
			Plan:  plan,
		}
		resp := resource.UpdateResponse{
			State: state,
		}

		// Setup mock expectations - UpdateDashboard(ctx, origin, jsonBody, dataset)
		mockClient.On("UpdateDashboard", mock.Anything, testOrigin, mock.Anything, testDataset).Return(nil)

		// Execute the update operation
		r.Update(context.Background(), req, &resp)

		// Verify expectations
		mockClient.AssertExpectations(t)
		assert.False(t, resp.Diagnostics.HasError())
	})

	// Test 2: Invalid YAML
	t.Run("invalid yaml", func(t *testing.T) {
		mockClient := new(MockClient)
		r := &DashboardResource{client: mockClient}
		_ = r

		// Create state
		state := tfsdk.State{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":         tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":        tftypes.NewValue(tftypes.String, testDataset),
				"dashboard_yaml": tftypes.NewValue(tftypes.String, testYaml),
			}),
			Schema: schema.Schema{
				Attributes: map[string]schema.Attribute{
					"origin": schema.StringAttribute{
						Computed: true,
					},
					"dataset": schema.StringAttribute{
						Required: true,
					},
					"dashboard_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			},
		}

		// Create plan with invalid YAML
		plan := tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":         tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":        tftypes.NewValue(tftypes.String, testDataset),
				"dashboard_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: : :"),
			}),
			Schema: state.Schema,
		}

		// Setup request and response
		req := resource.UpdateRequest{
			State: state,
			Plan:  plan,
		}
		resp := resource.UpdateResponse{
			State: state,
		}

		// Execute the update operation - should fail due to invalid YAML
		r.Update(context.Background(), req, &resp)

		// Verify expectations
		assert.True(t, resp.Diagnostics.HasError())
	})
}

func TestDashboardResource_SharingAnnotationTriggersReplan(t *testing.T) {
	tests := []struct {
		name         string
		configValue  types.String
		stateValue   types.String
		expectedPlan types.String
		description  string
	}{
		{
			name: "dash0.com/sharing changed - should trigger replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: private
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/sharing annotation changed",
		},
		{
			name: "dash0.com/sharing same - should suppress replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			description: "Should use state value when dash0.com/sharing annotation is the same",
		},
		{
			name: "dash0.com/sharing added in config - should trigger replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/sharing annotation is added",
		},
		{
			name: "dash0.com/sharing removed in config - should trigger replan",
			configValue: types.StringValue(`
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/sharing annotation is removed",
		},
		{
			name: "other metadata annotations still ignored",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
    some-server-annotation: server-value
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
    some-server-annotation: server-value
spec:
  display:
    name: My Dashboard
`),
			description: "Should use state value when only non-preserved annotations differ",
		},
		{
			name: "dash0.com/sharing changed alongside server annotations - should trigger replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: private
    some-server-annotation: server-value
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/sharing changed, even with server-added annotations",
		},
		{
			name: "metadata.labels still ignored",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
  labels:
    dash0.com/dataset: test
    dash0.com/origin: tf_123
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
  labels:
    dash0.com/dataset: test
    dash0.com/origin: tf_123
spec:
  display:
    name: My Dashboard
`),
			description: "Should use state value when only metadata.labels differ (still always ignored)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := customplanmodifier.YAMLSemanticEqual(converter.AnnotationSharing, converter.AnnotationFolderPath)

			req := planmodifier.StringRequest{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
				PlanValue:   tt.configValue,
			}
			resp := &planmodifier.StringResponse{
				PlanValue: tt.configValue,
			}

			modifier.PlanModifyString(context.Background(), req, resp)

			assert.Equal(t, tt.expectedPlan, resp.PlanValue, tt.description)
		})
	}
}

func TestDashboardResource_FolderPathAnnotationTriggersReplan(t *testing.T) {
	tests := []struct {
		name         string
		configValue  types.String
		stateValue   types.String
		expectedPlan types.String
		description  string
	}{
		{
			name: "dash0.com/folder-path changed - should trigger replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-b/dashboards
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/folder-path annotation changed",
		},
		{
			name: "dash0.com/folder-path same - should suppress replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			description: "Should use state value when dash0.com/folder-path annotation is the same",
		},
		{
			name: "dash0.com/folder-path added in config - should trigger replan",
			configValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/folder-path annotation is added",
		},
		{
			name: "dash0.com/folder-path removed in config - should trigger replan",
			configValue: types.StringValue(`
spec:
  display:
    name: My Dashboard
`),
			stateValue: types.StringValue(`
metadata:
  annotations:
    dash0.com/folder-path: /team-a/dashboards
spec:
  display:
    name: My Dashboard
`),
			expectedPlan: types.StringValue(`
spec:
  display:
    name: My Dashboard
`),
			description: "Should use config value when dash0.com/folder-path annotation is removed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := customplanmodifier.YAMLSemanticEqual(converter.AnnotationSharing, converter.AnnotationFolderPath)

			req := planmodifier.StringRequest{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
				PlanValue:   tt.configValue,
			}
			resp := &planmodifier.StringResponse{
				PlanValue: tt.configValue,
			}

			modifier.PlanModifyString(context.Background(), req, resp)

			assert.Equal(t, tt.expectedPlan, resp.PlanValue, tt.description)
		})
	}
}

func TestDashboardResource_Delete(t *testing.T) {
	mockClient := new(MockClient)
	r := &DashboardResource{client: mockClient}

	// Setup test data
	testOrigin := "test-origin"
	testDataset := "test-dataset"
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"

	// Create a state with test data
	state := tfsdk.State{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":         tftypes.NewValue(tftypes.String, testOrigin),
			"dataset":        tftypes.NewValue(tftypes.String, testDataset),
			"dashboard_yaml": tftypes.NewValue(tftypes.String, testYaml),
		}),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin": schema.StringAttribute{
					Computed: true,
				},
				"dataset": schema.StringAttribute{
					Required: true,
				},
				"dashboard_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	// Setup request and response
	req := resource.DeleteRequest{
		State: state,
	}
	resp := resource.DeleteResponse{}

	// Setup mock expectations for the delete operation
	mockClient.On("DeleteDashboard", mock.Anything, testOrigin, testDataset).Return(nil)

	// Execute the delete operation
	r.Delete(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	// Test with API error
	mockClient = new(MockClient)
	r = &DashboardResource{client: mockClient}
	mockClient.On("DeleteDashboard", mock.Anything, testOrigin, testDataset).Return(errors.New("API error"))

	resp = resource.DeleteResponse{}
	r.Delete(context.Background(), req, &resp)
	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}
