package provider

import (
	"context"
	"errors"
	"strings"
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
)

// basicSLOYaml is a minimal OpenSLO v1 document within the supported subset
// (single objective, inline ratioMetric, Occurrences budgeting, rolling 28d).
const basicSLOYaml = `apiVersion: openslo.com/v1
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

const metadataSLOYaml = `apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-availability
  labels:
    team: payments
  annotations:
    dash0.com/display-name: Checkout availability
    dash0.com/enabled: "true"
    dash0.com/folder-path: /payments
    dash0.com/sharing: "role:basic_member"
    owner: payments-oncall
spec:
  service: checkout
  objectives:
    - displayName: 99% availability
      target: 0.99
`

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

	// Check dataset is optional with a provider-level default
	datasetAttr := attrs["dataset"].(schema.StringAttribute)
	assert.False(t, datasetAttr.Required)
	assert.True(t, datasetAttr.Optional)
	assert.True(t, datasetAttr.Computed)

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

func TestSLOResource_ReadError(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

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
				"slo_yaml": tftypes.NewValue(tftypes.String, basicSLOYaml),
				"url":      tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSLOSchema(),
		},
	}

	resp := &resource.ReadResponse{
		State: tfsdk.State{
			Schema: testSLOSchema(),
		},
	}

	mockClient.On("GetSLO", ctx, "test-origin", "test-dataset").Return("", errors.New("not found"))

	r.Read(ctx, req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

func TestSLOResource_UpdateError(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

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
				"url":      tftypes.NewValue(tftypes.String, nil),
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
				"url":      tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: testSLOSchema(),
		},
	}

	resp := &resource.UpdateResponse{
		State: tfsdk.State{
			Schema: testSLOSchema(),
		},
	}

	mockClient.On("UpdateSLO", ctx, "test-origin", mock.Anything, "test-dataset").Return(errors.New("API error"))

	r.Update(ctx, req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	mockClient.AssertExpectations(t)
}

func TestSLOResource_DeleteError(t *testing.T) {
	ctx := context.Background()
	mockClient := new(MockClient)

	r := &SLOResource{
		client: mockClient,
	}

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

	mockClient.On("DeleteSLO", ctx, "test-origin", "test-dataset").Return(errors.New("API error"))

	r.Delete(ctx, req, resp)

	assert.True(t, resp.Diagnostics.HasError())
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

func TestSLOResource_MetadataPlan(t *testing.T) {
	schemaResp := &resource.SchemaResponse{}
	(&SLOResource{}).Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	sloYamlModifiers := schemaResp.Schema.Attributes["slo_yaml"].(schema.StringAttribute).PlanModifiers
	require.Len(t, sloYamlModifiers, 1)

	tests := []struct {
		name       string
		config     string
		state      string
		wantUpdate bool
	}{
		{
			name:       "display name edit",
			config:     editedMetadataSLOYaml(t, "dash0.com/display-name: Checkout availability", "dash0.com/display-name: Checkout success"),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "disabled SLO",
			config:     editedMetadataSLOYaml(t, `dash0.com/enabled: "true"`, `dash0.com/enabled: "false"`),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "folder path edit",
			config:     editedMetadataSLOYaml(t, "dash0.com/folder-path: /payments", "dash0.com/folder-path: /checkout"),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "sharing edit",
			config:     editedMetadataSLOYaml(t, `dash0.com/sharing: "role:basic_member"`, `dash0.com/sharing: "role:admin"`),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "user label edit",
			config:     editedMetadataSLOYaml(t, "team: payments", "team: checkout"),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "user annotation edit",
			config:     editedMetadataSLOYaml(t, "owner: payments-oncall", "owner: checkout-oncall"),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "objective edit",
			config:     editedMetadataSLOYaml(t, "target: 0.99", "target: 0.995"),
			state:      metadataSLOYaml,
			wantUpdate: true,
		},
		{
			name:       "omitted enabled annotation",
			config:     editedMetadataSLOYaml(t, "    dash0.com/enabled: \"true\"\n", ""),
			state:      metadataSLOYaml,
			wantUpdate: false,
		},
		{
			name:       "server labels in state",
			config:     metadataSLOYaml,
			state:      editedMetadataSLOYaml(t, "    team: payments\n", "    team: payments\n    dash0.com/id: 6f1d1c3e-7c43-4f39-9a52-0b7a1d8e1f20\n    dash0.com/version: \"3\"\n"),
			wantUpdate: false,
		},
		{
			name:       "server annotations in state",
			config:     metadataSLOYaml,
			state:      editedMetadataSLOYaml(t, "    owner: payments-oncall\n", "    owner: payments-oncall\n    dash0.com/created-at: \"2026-10-01T09:00:00Z\"\n    dash0.com/window-start: \"2026-10-01T09:00:00Z\"\n"),
			wantUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := planmodifier.StringRequest{
				ConfigValue: types.StringValue(tt.config),
				StateValue:  types.StringValue(tt.state),
				PlanValue:   types.StringValue(tt.config),
			}
			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}

			sloYamlModifiers[0].PlanModifyString(context.Background(), req, resp)

			assert.Equal(t, tt.wantUpdate, !resp.PlanValue.Equal(req.StateValue), "planned slo_yaml:\n%s", resp.PlanValue.ValueString())
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

func editedMetadataSLOYaml(t *testing.T, from, to string) string {
	t.Helper()
	edited := strings.Replace(metadataSLOYaml, from, to, 1)
	require.NotEqual(t, metadataSLOYaml, edited, "%q not found in metadataSLOYaml", from)
	return edited
}
