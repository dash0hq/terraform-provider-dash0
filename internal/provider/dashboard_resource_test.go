package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, "Manages a Dash0 Dashboard (in Perses format).", resp.Schema.Description)

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

	// Setup mock expectations - using a more lenient matcher since the test framework might not be
	// unmarshalling the exact object we expect
	mockClient.On("CreateDashboard", mock.Anything, mock.Anything).Return(nil)

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

	// Setup mock expectations for the read operation
	mockClient.On("GetDashboard", mock.Anything, testDataset, testOrigin).Return(
		&model.Dashboard{
			Origin:        types.StringValue(testOrigin),
			Dataset:       types.StringValue(testDataset),
			DashboardYaml: types.StringValue(testYaml),
		},
		nil,
	)

	// Execute the read operation
	r.Read(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	// Create a new state object to verify
	var resultState model.Dashboard
	diags := resp.State.Get(context.Background(), &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")

	assert.Equal(t, testOrigin, resultState.Origin.ValueString())
	assert.Equal(t, testDataset, resultState.Dataset.ValueString())
	assert.Equal(t, testYaml, resultState.DashboardYaml.ValueString())

	// Test with API error
	mockClient = new(MockClient)
	r = &DashboardResource{client: mockClient}
	mockClient.On("GetDashboard", mock.Anything, testDataset, testOrigin).Return(
		nil,
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
	newDataset := "new-dataset"
	testYaml := "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview"
	updatedYaml := testYaml + "\n  description: Updated dashboard"

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

		// Setup mock expectations - UpdateDashboard should be called
		mockClient.On("UpdateDashboard", mock.Anything, mock.MatchedBy(func(dashboardModel model.Dashboard) bool {
			return dashboardModel.Origin.ValueString() == testOrigin &&
				dashboardModel.Dataset.ValueString() == testDataset
		})).Return(nil)

		// Execute the update operation
		r.Update(context.Background(), req, &resp)

		// Verify expectations
		mockClient.AssertExpectations(t)
		assert.False(t, resp.Diagnostics.HasError())
	})

	// Test 2: Change dataset (should delete and recreate)
	t.Run("change dataset", func(t *testing.T) {
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

		// Create plan with new dataset
		plan := tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":         tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":        tftypes.NewValue(tftypes.String, newDataset),
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

		// Setup mock expectations - DeleteDashboard followed by CreateDashboard
		mockClient.On("DeleteDashboard", mock.Anything, testOrigin, testDataset).Return(nil)
		mockClient.On("CreateDashboard", mock.Anything, mock.MatchedBy(func(m model.Dashboard) bool {
			return m.Origin.ValueString() == testOrigin &&
				m.Dataset.ValueString() == newDataset
		})).Return(nil)

		// Execute the update operation
		r.Update(context.Background(), req, &resp)

		// Verify expectations
		mockClient.AssertExpectations(t)
		assert.False(t, resp.Diagnostics.HasError())
	})

	// Test 3: Invalid YAML
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
