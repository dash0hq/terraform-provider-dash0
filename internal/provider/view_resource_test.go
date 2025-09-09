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

func TestViewResource_Metadata(t *testing.T) {
	r := &ViewResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)

	assert.Equal(t, "dash0_view", resp.TypeName)
}

func TestViewResource_Schema(t *testing.T) {
	r := &ViewResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)
	assert.Equal(t, "Manages a Dash0 View.", resp.Schema.Description)

	// Verify schema attributes
	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "dataset")
	assert.Contains(t, resp.Schema.Attributes, "view_yaml")

	// Check specific attribute properties
	assert.True(t, resp.Schema.Attributes["origin"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Required)
	assert.True(t, resp.Schema.Attributes["view_yaml"].(schema.StringAttribute).Required)
}

func TestViewResource_Configure(t *testing.T) {
	r := &ViewResource{}
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

func TestViewResource_Create(t *testing.T) {
	mockClient := new(MockClient)
	r := &ViewResource{client: mockClient}

	// Setup test data
	testYaml := "kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View"
	testDataset := "test-dataset"

	// Setup plan
	plan := tfsdk.Plan{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, ""),
			"dataset":   tftypes.NewValue(tftypes.String, testDataset),
			"view_yaml": tftypes.NewValue(tftypes.String, testYaml),
		}),
		Schema: schema.Schema{
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

	// Setup mock expectations
	mockClient.On("CreateView", mock.Anything, mock.Anything).Return(nil)

	// Execute the create operation
	r.Create(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
}

func TestViewResource_Read(t *testing.T) {
	mockClient := new(MockClient)
	r := &ViewResource{client: mockClient}

	// Setup test data
	testOrigin := "test-origin"
	testDataset := "test-dataset"
	testYaml := "kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View"

	// Create state schema
	stateSchema := schema.Schema{
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

	// Setup state
	state := tfsdk.State{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, testOrigin),
			"dataset":   tftypes.NewValue(tftypes.String, testDataset),
			"view_yaml": tftypes.NewValue(tftypes.String, "old yaml"),
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
	mockClient.On("GetView", mock.Anything, testDataset, testOrigin).Return(
		&model.ViewResource{
			Origin:   types.StringValue(testOrigin),
			Dataset:  types.StringValue(testDataset),
			ViewYaml: types.StringValue(testYaml),
		},
		nil,
	)

	// Execute the read operation
	r.Read(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	// Create a new state object to verify
	var resultState model.ViewResource
	diags := resp.State.Get(context.Background(), &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")

	assert.Equal(t, testOrigin, resultState.Origin.ValueString())
	assert.Equal(t, testDataset, resultState.Dataset.ValueString())
	assert.Equal(t, testYaml, resultState.ViewYaml.ValueString())

	// Test with API error
	mockClient = new(MockClient)
	r = &ViewResource{client: mockClient}
	mockClient.On("GetView", mock.Anything, testDataset, testOrigin).Return(
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

func TestViewResource_Update(t *testing.T) {
	mockClient := new(MockClient)
	_ = mockClient

	// Setup test data
	testOrigin := "test-origin"
	testDataset := "test-dataset"
	newDataset := "new-dataset"
	testYaml := "kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View"
	updatedYaml := testYaml + "\n  description: Updated view"

	// Test 1: Update view YAML only (no dataset change)
	t.Run("update yaml only", func(t *testing.T) {
		mockClient := new(MockClient)
		r := &ViewResource{client: mockClient}

		// Create state
		state := tfsdk.State{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":   tftypes.NewValue(tftypes.String, testDataset),
				"view_yaml": tftypes.NewValue(tftypes.String, testYaml),
			}),
			Schema: schema.Schema{
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
			},
		}

		// Create plan with updated YAML
		plan := tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":   tftypes.NewValue(tftypes.String, testDataset),
				"view_yaml": tftypes.NewValue(tftypes.String, updatedYaml),
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

		// Setup mock expectations - UpdateView should be called
		mockClient.On("UpdateView", mock.Anything, mock.MatchedBy(func(m model.ViewResource) bool {
			return m.Origin.ValueString() == testOrigin &&
				m.Dataset.ValueString() == testDataset
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
		r := &ViewResource{client: mockClient}

		// Create state
		state := tfsdk.State{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":   tftypes.NewValue(tftypes.String, testDataset),
				"view_yaml": tftypes.NewValue(tftypes.String, testYaml),
			}),
			Schema: schema.Schema{
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
			},
		}

		// Create plan with new dataset
		plan := tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":   tftypes.NewValue(tftypes.String, newDataset),
				"view_yaml": tftypes.NewValue(tftypes.String, updatedYaml),
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

		// Setup mock expectations - DeleteView followed by CreateView
		mockClient.On("DeleteView", mock.Anything, testOrigin, testDataset).Return(nil)
		mockClient.On("CreateView", mock.Anything, mock.MatchedBy(func(viewModel model.ViewResource) bool {
			return viewModel.Origin.ValueString() == testOrigin &&
				viewModel.Dataset.ValueString() == newDataset
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
		r := &ViewResource{client: mockClient}
		_ = r

		// Create state
		state := tfsdk.State{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":   tftypes.NewValue(tftypes.String, testDataset),
				"view_yaml": tftypes.NewValue(tftypes.String, testYaml),
			}),
			Schema: schema.Schema{
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
			},
		}

		// Create plan with invalid YAML
		plan := tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, testOrigin),
				"dataset":   tftypes.NewValue(tftypes.String, testDataset),
				"view_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: : :"),
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

func TestViewResource_Delete(t *testing.T) {
	mockClient := new(MockClient)
	r := &ViewResource{client: mockClient}

	// Setup test data
	testOrigin := "test-origin"
	testDataset := "test-dataset"
	testYaml := "kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View"

	// Create a state with test data
	state := tfsdk.State{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, testOrigin),
			"dataset":   tftypes.NewValue(tftypes.String, testDataset),
			"view_yaml": tftypes.NewValue(tftypes.String, testYaml),
		}),
		Schema: schema.Schema{
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
		},
	}

	// Setup request and response
	req := resource.DeleteRequest{
		State: state,
	}
	resp := resource.DeleteResponse{}

	// Setup mock expectations for the delete operation
	mockClient.On("DeleteView", mock.Anything, testOrigin, testDataset).Return(nil)

	// Execute the delete operation
	r.Delete(context.Background(), req, &resp)

	// Verify expectations
	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	// Test with API error
	mockClient = new(MockClient)
	r = &ViewResource{client: mockClient}
	mockClient.On("DeleteView", mock.Anything, testOrigin, testDataset).Return(errors.New("API error"))

	resp = resource.DeleteResponse{}
	r.Delete(context.Background(), req, &resp)
	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}
