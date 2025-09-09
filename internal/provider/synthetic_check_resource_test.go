package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Tests for syntheticCheckResource
func TestSyntheticCheckResource_Metadata(t *testing.T) {
	r := &SyntheticCheckResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{
		ProviderTypeName: "dash0",
	}

	r.Metadata(context.Background(), req, resp)

	assert.Equal(t, "dash0_synthetic_check", resp.TypeName)
}

func TestSyntheticCheckResource_Schema(t *testing.T) {
	r := &SyntheticCheckResource{}
	resp := &resource.SchemaResponse{}
	req := resource.SchemaRequest{}

	r.Schema(context.Background(), req, resp)

	assert.NotNil(t, resp.Schema)
	assert.Equal(t, "Manages a Dash0 Synthetic Check.", resp.Schema.Description)

	// Check attributes
	attrs := resp.Schema.Attributes
	assert.Contains(t, attrs, "origin")
	assert.Contains(t, attrs, "dataset")
	assert.Contains(t, attrs, "synthetic_check_yaml")

	// Check origin is computed
	originAttr := attrs["origin"].(schema.StringAttribute)
	assert.True(t, originAttr.Computed)

	// Check dataset is required
	datasetAttr := attrs["dataset"].(schema.StringAttribute)
	assert.True(t, datasetAttr.Required)

	// Check synthetic_check_yaml is required
	checkYamlAttr := attrs["synthetic_check_yaml"].(schema.StringAttribute)
	assert.True(t, checkYamlAttr.Required)
}

func TestSyntheticCheckResource_Create(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SyntheticCheckResource{
		client: mockClient,
	}

	// Setup request
	req := resource.CreateRequest{
		Plan: tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":               tftypes.String,
					"dataset":              tftypes.String,
					"synthetic_check_yaml": tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":  tftypes.NewValue(tftypes.String, nil),
				"dataset": tftypes.NewValue(tftypes.String, "test-dataset"),
				"synthetic_check_yaml": tftypes.NewValue(tftypes.String, `
kind: Dash0SyntheticCheck
metadata:
  name: examplecom
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com`),
			}),
			Schema: testSyntheticCheckSchema(),
		},
	}

	resp := &resource.CreateResponse{
		State: tfsdk.State{
			Schema: testSyntheticCheckSchema(),
		},
	}

	// Setup mock expectations
	mockClient.On("CreateSyntheticCheck", ctx, mock.MatchedBy(func(check model.SyntheticCheckResourceModel) bool {
		return check.Dataset.ValueString() == "test-dataset" &&
			check.Origin.ValueString() != "" && // Should have generated UUID
			check.SyntheticCheckYaml.ValueString() != ""
	})).Return(nil)

	// Execute
	r.Create(ctx, req, resp)

	// Verify
	assert.False(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

func TestSyntheticCheckResource_CreateWithError(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SyntheticCheckResource{
		client: mockClient,
	}

	// Setup request
	req := resource.CreateRequest{
		Plan: tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":               tftypes.String,
					"dataset":              tftypes.String,
					"synthetic_check_yaml": tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":  tftypes.NewValue(tftypes.String, nil),
				"dataset": tftypes.NewValue(tftypes.String, "test-dataset"),
				"synthetic_check_yaml": tftypes.NewValue(tftypes.String, `
kind: Dash0SyntheticCheck
metadata:
  name: examplecom`),
			}),
			Schema: testSyntheticCheckSchema(),
		},
	}

	resp := &resource.CreateResponse{
		State: tfsdk.State{
			Schema: testSyntheticCheckSchema(),
		},
	}

	// Setup mock to return error
	mockClient.On("CreateSyntheticCheck", ctx, mock.Anything).Return(errors.New("API error"))

	// Execute
	r.Create(ctx, req, resp)

	// Verify error was added to diagnostics
	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

func TestSyntheticCheckResource_Delete(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SyntheticCheckResource{
		client: mockClient,
	}

	// Setup request
	req := resource.DeleteRequest{
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
				"synthetic_check_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
			}),
			Schema: testSyntheticCheckSchema(),
		},
	}

	resp := &resource.DeleteResponse{}

	// Setup mock expectations
	mockClient.On("DeleteSyntheticCheck", ctx, "test-origin", "test-dataset").Return(nil)

	// Execute
	r.Delete(ctx, req, resp)

	// Verify
	assert.False(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

// Helper function to create test schema
func testSyntheticCheckSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Computed: true,
			},
			"dataset": schema.StringAttribute{
				Required: true,
			},
			"synthetic_check_yaml": schema.StringAttribute{
				Required: true,
			},
		},
	}
}

func TestSyntheticCheckResource_Update(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SyntheticCheckResource{
		client: mockClient,
	}

	// Test regular update (same dataset)
	t.Run("Update same dataset", func(t *testing.T) {
		req := resource.UpdateRequest{
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
					"synthetic_check_yaml": tftypes.NewValue(tftypes.String, "old-yaml"),
				}),
				Schema: testSyntheticCheckSchema(),
			},
			Plan: tfsdk.Plan{
				Raw: tftypes.NewValue(tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":               tftypes.String,
						"dataset":              tftypes.String,
						"synthetic_check_yaml": tftypes.String,
					},
				}, map[string]tftypes.Value{
					"origin":  tftypes.NewValue(tftypes.String, "test-origin"),
					"dataset": tftypes.NewValue(tftypes.String, "test-dataset"),
					"synthetic_check_yaml": tftypes.NewValue(tftypes.String, `
kind: Dash0SyntheticCheck
metadata:
  name: updated`),
				}),
				Schema: testSyntheticCheckSchema(),
			},
		}

		resp := &resource.UpdateResponse{
			State: tfsdk.State{
				Schema: testSyntheticCheckSchema(),
			},
		}

		mockClient.On("UpdateSyntheticCheck", ctx, mock.MatchedBy(func(check model.SyntheticCheckResourceModel) bool {
			return check.Origin.ValueString() == "test-origin" &&
				check.Dataset.ValueString() == "test-dataset"
		})).Return(nil).Once()

		r.Update(ctx, req, resp)

		assert.False(t, resp.Diagnostics.HasError())
		mockClient.AssertExpectations(t)
	})
}
