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

// basicSLOYaml is a minimal OpenSLO v1 document within the supported subset
// (single objective, inline ratioMetric, Occurrences budgeting, rolling 28d).
const basicSLOYaml = `apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/display-name: Checkout availability
spec:
  description: 99 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99% availability
      target: 0.99`

// Tests for sloResource
func TestSLOResource_Metadata(t *testing.T) {
	r := &SLOResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{
		ProviderTypeName: "dash0",
	}

	r.Metadata(context.Background(), req, resp)

	assert.Equal(t, "dash0_slo", resp.TypeName)
}

func TestSLOResource_Schema(t *testing.T) {
	r := &SLOResource{}
	resp := &resource.SchemaResponse{}
	req := resource.SchemaRequest{}

	r.Schema(context.Background(), req, resp)

	assert.NotNil(t, resp.Schema)
	assert.Contains(t, resp.Schema.Description, "Manages a Dash0 Service Level Objective")

	// Check attributes
	attrs := resp.Schema.Attributes
	assert.Contains(t, attrs, "origin")
	assert.Contains(t, attrs, "id")
	assert.Contains(t, attrs, "dataset")
	assert.Contains(t, attrs, "slo_yaml")
	assert.Contains(t, attrs, "url")

	// Check origin is computed
	originAttr := attrs["origin"].(schema.StringAttribute)
	assert.True(t, originAttr.Computed)

	// Check id is computed
	idAttr := attrs["id"].(schema.StringAttribute)
	assert.True(t, idAttr.Computed)

	// Check dataset is required
	datasetAttr := attrs["dataset"].(schema.StringAttribute)
	assert.True(t, datasetAttr.Required)

	// Check slo_yaml is required
	sloYamlAttr := attrs["slo_yaml"].(schema.StringAttribute)
	assert.True(t, sloYamlAttr.Required)

	// Check url is computed
	urlAttr := attrs["url"].(schema.StringAttribute)
	assert.True(t, urlAttr.Computed)
}

func TestSLOResource_Create(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

	testURL := "https://app.dash0.com/goto/alerting/slos/details?slo_id=internal-uuid"

	// Setup request
	req := resource.CreateRequest{
		Plan: tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":   tftypes.String,
					"id":       tftypes.String,
					"dataset":  tftypes.String,
					"slo_yaml": tftypes.String,
					"url":      tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":   tftypes.NewValue(tftypes.String, nil),
				"id":       tftypes.NewValue(tftypes.String, nil),
				"dataset":  tftypes.NewValue(tftypes.String, "test-dataset"),
				"slo_yaml": tftypes.NewValue(tftypes.String, basicSLOYaml),
				"url":      tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSLOSchema(),
		},
	}

	resp := &resource.CreateResponse{
		State: tfsdk.State{
			Schema: testSLOSchema(),
		},
	}

	// Setup mock expectations - CreateSLO(ctx, origin, jsonBody, dataset)
	mockClient.On("CreateSLO", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// After create, the URL is resolved by origin (generated tf_-prefixed value).
	mockClient.On("ResolveSLO", ctx, mock.Anything, "test-dataset").Return("test-id", testURL, nil)

	// Execute
	r.Create(ctx, req, resp)

	// Verify
	assert.False(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)

	// Verify the resolved id and URL were written to state
	var resultState sloModel
	diags := resp.State.Get(ctx, &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")
	assert.Equal(t, "test-id", resultState.ID.ValueString())
	assert.Equal(t, testURL, resultState.URL.ValueString())
}

func TestSLOResource_CreateWithError(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

	// Setup request
	req := resource.CreateRequest{
		Plan: tfsdk.Plan{
			Raw: tftypes.NewValue(tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":   tftypes.String,
					"id":       tftypes.String,
					"dataset":  tftypes.String,
					"slo_yaml": tftypes.String,
					"url":      tftypes.String,
				},
			}, map[string]tftypes.Value{
				"origin":   tftypes.NewValue(tftypes.String, nil),
				"id":       tftypes.NewValue(tftypes.String, nil),
				"dataset":  tftypes.NewValue(tftypes.String, "test-dataset"),
				"slo_yaml": tftypes.NewValue(tftypes.String, basicSLOYaml),
				"url":      tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSLOSchema(),
		},
	}

	resp := &resource.CreateResponse{
		State: tfsdk.State{
			Schema: testSLOSchema(),
		},
	}

	// Setup mock to return error - CreateSLO(ctx, origin, jsonBody, dataset)
	mockClient.On("CreateSLO", ctx, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("API error"))

	// Execute
	r.Create(ctx, req, resp)

	// Verify error was added to diagnostics
	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

func TestSLOResource_Delete(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

	// Setup request
	req := resource.DeleteRequest{
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
				"id":       tftypes.NewValue(tftypes.String, nil),
				"dataset":  tftypes.NewValue(tftypes.String, "test-dataset"),
				"slo_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
				"url":      tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSLOSchema(),
		},
	}

	resp := &resource.DeleteResponse{}

	// Setup mock expectations - DeleteSLO(ctx, origin, dataset)
	mockClient.On("DeleteSLO", ctx, "test-origin", "test-dataset").Return(nil)

	// Execute
	r.Delete(ctx, req, resp)

	// Verify
	assert.False(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

// Helper function to create test schema
func testSLOSchema() schema.Schema {
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
			"slo_yaml": schema.StringAttribute{
				Required: true,
			},
			"url": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func TestSLOResource_SharingAnnotationTriggersReplan(t *testing.T) {
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
apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/sharing: all-users
spec:
  service: checkout
`),
			stateValue: types.StringValue(`
apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/sharing: private
spec:
  service: checkout
`),
			expectedPlan: types.StringValue(`
apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/sharing: all-users
spec:
  service: checkout
`),
			description: "Should use config value when dash0.com/sharing annotation changed on SLO",
		},
		{
			name: "dash0.com/sharing same - should suppress replan",
			configValue: types.StringValue(`
apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/sharing: all-users
spec:
  service: checkout
`),
			stateValue: types.StringValue(`
apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/sharing: all-users
spec:
  service: checkout
`),
			expectedPlan: types.StringValue(`
apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/sharing: all-users
spec:
  service: checkout
`),
			description: "Should use state value when dash0.com/sharing annotation is the same on SLO",
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

func TestSLOResource_Update(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

	testURL := "https://app.dash0.com/goto/alerting/slos/details?slo_id=internal-uuid"

	// Test regular update (same dataset)
	t.Run("Update same dataset", func(t *testing.T) {
		req := resource.UpdateRequest{
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
					"slo_yaml": tftypes.NewValue(tftypes.String, "old-yaml"),
					"url":      tftypes.NewValue(tftypes.String, testURL),
				}),
				Schema: testSLOSchema(),
			},
			Plan: tfsdk.Plan{
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
					"slo_yaml": tftypes.NewValue(tftypes.String, basicSLOYaml),
					"url":      tftypes.NewValue(tftypes.String, testURL),
				}),
				Schema: testSLOSchema(),
			},
		}

		resp := &resource.UpdateResponse{
			State: tfsdk.State{
				Schema: testSLOSchema(),
			},
		}

		// Setup mock expectations - UpdateSLO(ctx, origin, jsonBody, dataset)
		mockClient.On("UpdateSLO", ctx, "test-origin", mock.Anything, "test-dataset").Return(nil).Once()

		r.Update(ctx, req, resp)

		assert.False(t, resp.Diagnostics.HasError())
		mockClient.AssertExpectations(t)

		// id and URL are carried over from prior state (Update does not re-resolve them).
		var resultState sloModel
		diags := resp.State.Get(ctx, &resultState)
		require.False(t, diags.HasError(), "state cannot be unmarshalled")
		assert.Equal(t, "test-id", resultState.ID.ValueString())
		assert.Equal(t, testURL, resultState.URL.ValueString())
	})
}
