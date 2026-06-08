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
	assert.Contains(t, resp.Schema.Description, "Manages a Dash0 Synthetic Check.")

	// Check attributes
	attrs := resp.Schema.Attributes
	assert.Contains(t, attrs, "origin")
	assert.Contains(t, attrs, "dataset")
	assert.Contains(t, attrs, "synthetic_check_yaml")
	assert.Contains(t, attrs, "url")

	// Check origin is computed
	originAttr := attrs["origin"].(schema.StringAttribute)
	assert.True(t, originAttr.Computed)

	// Check dataset is required
	datasetAttr := attrs["dataset"].(schema.StringAttribute)
	assert.True(t, datasetAttr.Required)

	// Check synthetic_check_yaml is required
	checkYamlAttr := attrs["synthetic_check_yaml"].(schema.StringAttribute)
	assert.True(t, checkYamlAttr.Required)

	// Check url is computed
	urlAttr := attrs["url"].(schema.StringAttribute)
	assert.True(t, urlAttr.Computed)
}

func TestSyntheticCheckResource_Create(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SyntheticCheckResource{
		client: mockClient,
	}

	testURL := "https://app.dash0.com/goto/alerting/synthetics?check_id=internal-uuid"

	// Setup request
	req := resource.CreateRequest{
		Plan: tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":               tftypes.String,
					"id":                   tftypes.String,
					"dataset":              tftypes.String,
					"synthetic_check_yaml": tftypes.String,
					"url":                  tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":  tftypes.NewValue(tftypes.String, nil),
				"id":      tftypes.NewValue(tftypes.String, nil),
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
				"url": tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSyntheticCheckSchema(),
		},
	}

	resp := &resource.CreateResponse{
		State: tfsdk.State{
			Schema: testSyntheticCheckSchema(),
		},
	}

	// Setup mock expectations - CreateSyntheticCheck(ctx, origin, jsonBody, dataset)
	mockClient.On("CreateSyntheticCheck", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// After create, the URL is resolved by origin (generated tf_-prefixed value).
	mockClient.On("ResolveSyntheticCheck", ctx, mock.Anything, "test-dataset").Return("test-id", testURL, nil)

	// Execute
	r.Create(ctx, req, resp)

	// Verify
	assert.False(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)

	// Verify the resolved URL was written to state
	var resultState syntheticCheckModel
	diags := resp.State.Get(ctx, &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")
	assert.Equal(t, testURL, resultState.URL.ValueString())
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
					"id":                   tftypes.String,
					"dataset":              tftypes.String,
					"synthetic_check_yaml": tftypes.String,
					"url":                  tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":  tftypes.NewValue(tftypes.String, nil),
				"id":      tftypes.NewValue(tftypes.String, nil),
				"dataset": tftypes.NewValue(tftypes.String, "test-dataset"),
				"synthetic_check_yaml": tftypes.NewValue(tftypes.String, `
kind: Dash0SyntheticCheck
metadata:
  name: examplecom`),
				"url": tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSyntheticCheckSchema(),
		},
	}

	resp := &resource.CreateResponse{
		State: tfsdk.State{
			Schema: testSyntheticCheckSchema(),
		},
	}

	// Setup mock to return error - CreateSyntheticCheck(ctx, origin, jsonBody, dataset)
	mockClient.On("CreateSyntheticCheck", ctx, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("API error"))

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
					"id":                   tftypes.String,
					"dataset":              tftypes.String,
					"synthetic_check_yaml": tftypes.String,
					"url":                  tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":               tftypes.NewValue(tftypes.String, "test-origin"),
				"id":                   tftypes.NewValue(tftypes.String, nil),
				"dataset":              tftypes.NewValue(tftypes.String, "test-dataset"),
				"synthetic_check_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
				"url":                  tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSyntheticCheckSchema(),
		},
	}

	resp := &resource.DeleteResponse{}

	// Setup mock expectations - DeleteSyntheticCheck(ctx, origin, dataset)
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
			"id": schema.StringAttribute{
				Computed: true,
			},
			"dataset": schema.StringAttribute{
				Required: true,
			},
			"synthetic_check_yaml": schema.StringAttribute{
				Required: true,
			},
			"url": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func TestSyntheticCheckResource_SharingAnnotationTriggersReplan(t *testing.T) {
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
  name: my-synthetic-check
  annotations:
    dash0.com/sharing: all-users
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`),
			stateValue: types.StringValue(`
metadata:
  name: my-synthetic-check
  annotations:
    dash0.com/sharing: private
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`),
			expectedPlan: types.StringValue(`
metadata:
  name: my-synthetic-check
  annotations:
    dash0.com/sharing: all-users
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`),
			description: "Should use config value when dash0.com/sharing annotation changed on synthetic check",
		},
		{
			name: "dash0.com/sharing same - should suppress replan",
			configValue: types.StringValue(`
metadata:
  name: my-synthetic-check
  annotations:
    dash0.com/sharing: all-users
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`),
			stateValue: types.StringValue(`
metadata:
  name: my-synthetic-check
  annotations:
    dash0.com/sharing: all-users
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`),
			expectedPlan: types.StringValue(`
metadata:
  name: my-synthetic-check
  annotations:
    dash0.com/sharing: all-users
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`),
			description: "Should use state value when dash0.com/sharing annotation is the same on synthetic check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := customplanmodifier.YAMLSemanticEqual(converter.AnnotationSharing)

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

func TestSyntheticCheckResource_Update(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SyntheticCheckResource{
		client: mockClient,
	}

	testURL := "https://app.dash0.com/goto/alerting/synthetics?check_id=internal-uuid"

	// Test regular update (same dataset)
	t.Run("Update same dataset", func(t *testing.T) {
		req := resource.UpdateRequest{
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
					"synthetic_check_yaml": tftypes.NewValue(tftypes.String, "old-yaml"),
					"url":                  tftypes.NewValue(tftypes.String, testURL),
				}),
				Schema: testSyntheticCheckSchema(),
			},
			Plan: tfsdk.Plan{
				Raw: tftypes.NewValue(tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":               tftypes.String,
						"id":                   tftypes.String,
						"dataset":              tftypes.String,
						"synthetic_check_yaml": tftypes.String,
						"url":                  tftypes.String,
					},
				}, map[string]tftypes.Value{
					"origin":  tftypes.NewValue(tftypes.String, "test-origin"),
					"id":      tftypes.NewValue(tftypes.String, nil),
					"dataset": tftypes.NewValue(tftypes.String, "test-dataset"),
					"synthetic_check_yaml": tftypes.NewValue(tftypes.String, `
kind: Dash0SyntheticCheck
metadata:
  name: updated`),
					"url": tftypes.NewValue(tftypes.String, testURL),
				}),
				Schema: testSyntheticCheckSchema(),
			},
		}

		resp := &resource.UpdateResponse{
			State: tfsdk.State{
				Schema: testSyntheticCheckSchema(),
			},
		}

		// Setup mock expectations - UpdateSyntheticCheck(ctx, origin, jsonBody, dataset)
		mockClient.On("UpdateSyntheticCheck", ctx, "test-origin", mock.Anything, "test-dataset").Return(nil).Once()

		r.Update(ctx, req, resp)

		assert.False(t, resp.Diagnostics.HasError())
		mockClient.AssertExpectations(t)

		// URL is carried over from prior state (Update does not re-resolve it).
		var resultState syntheticCheckModel
		diags := resp.State.Get(ctx, &resultState)
		require.False(t, diags.HasError(), "state cannot be unmarshalled")
		assert.Equal(t, testURL, resultState.URL.ValueString())
	})
}
