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

func TestCheckRuleResourceModel(t *testing.T) {
	origin := "test-origin"
	dataset := "test-dataset"
	checkRuleYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-rule
spec:
  groups:
    - name: TestGroup
      rules:
        - alert: TestAlert
          expr: up == 0
          for: 5m`

	url := "https://app.dash0.com/goto/alerting/check-rules?check_rule_id=internal-uuid"

	m := checkRuleModel{
		Origin:        types.StringValue(origin),
		Dataset:       types.StringValue(dataset),
		CheckRuleYaml: types.StringValue(checkRuleYaml),
		URL:           types.StringValue(url),
	}

	assert.Equal(t, origin, m.Origin.ValueString())
	assert.Equal(t, dataset, m.Dataset.ValueString())
	assert.Equal(t, checkRuleYaml, m.CheckRuleYaml.ValueString())
	assert.Equal(t, url, m.URL.ValueString())
}

func TestNewCheckRuleResource(t *testing.T) {
	resource := NewCheckRuleResource()
	assert.NotNil(t, resource)

	// Check that it's the correct type
	_, ok := resource.(*CheckRuleResource)
	assert.True(t, ok)
}

func TestCheckRuleResource_Metadata(t *testing.T) {
	r := &CheckRuleResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{
		ProviderTypeName: "dash0",
	}

	r.Metadata(context.Background(), req, resp)

	assert.Equal(t, "dash0_check_rule", resp.TypeName)
}

func TestCheckRuleResource_Schema(t *testing.T) {
	r := &CheckRuleResource{}
	resp := &resource.SchemaResponse{}
	req := resource.SchemaRequest{}

	r.Schema(context.Background(), req, resp)

	// Verify schema has the expected attributes
	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "dataset")
	assert.Contains(t, resp.Schema.Attributes, "check_rule_yaml")
	assert.Contains(t, resp.Schema.Attributes, "url")

	// Verify origin is computed
	originAttr := resp.Schema.Attributes["origin"]
	assert.True(t, originAttr.IsComputed())
	assert.False(t, originAttr.IsRequired())

	// Verify dataset is required
	datasetAttr := resp.Schema.Attributes["dataset"]
	assert.True(t, datasetAttr.IsRequired())
	assert.False(t, datasetAttr.IsComputed())

	// Verify check_rule_yaml is required
	yamlAttr := resp.Schema.Attributes["check_rule_yaml"]
	assert.True(t, yamlAttr.IsRequired())
	assert.False(t, yamlAttr.IsComputed())

	// Verify url is computed
	urlAttr := resp.Schema.Attributes["url"]
	assert.True(t, urlAttr.IsComputed())
	assert.False(t, urlAttr.IsRequired())
}

func TestCheckRuleResource_Configure(t *testing.T) {
	tests := []struct {
		name         string
		providerData interface{}
		expectError  bool
		errorMessage string
	}{
		{
			name:         "valid client interface",
			providerData: &MockClient{},
			expectError:  false,
		},
		{
			name:         "nil provider data",
			providerData: nil,
			expectError:  false,
		},
		{
			name:         "invalid provider data type",
			providerData: "invalid",
			expectError:  true,
			errorMessage: "Unexpected Data Source Configure Type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &CheckRuleResource{}
			resp := &resource.ConfigureResponse{}
			req := resource.ConfigureRequest{
				ProviderData: tc.providerData,
			}

			r.Configure(context.Background(), req, resp)

			if tc.expectError {
				assert.True(t, resp.Diagnostics.HasError())
				if tc.errorMessage != "" {
					assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tc.errorMessage)
				}
			} else {
				assert.False(t, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestCheckRuleResource_Create_InvalidYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &CheckRuleResource{client: mockClient}

	// Create request with invalid YAML
	req := resource.CreateRequest{}
	resp := &resource.CreateResponse{}

	// Set up the request state with invalid YAML
	req.Plan = tfsdk.Plan{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":          tftypes.String,
					"id":              tftypes.String,
					"dataset":         tftypes.String,
					"check_rule_yaml": tftypes.String,
					"url":             tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":          tftypes.NewValue(tftypes.String, "test-origin"),
				"id":              tftypes.NewValue(tftypes.String, nil),
				"dataset":         tftypes.NewValue(tftypes.String, "test-dataset"),
				"check_rule_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: content: ["),
				"url":             tftypes.NewValue(tftypes.String, nil),
			},
		),
		Schema: schema.Schema{
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
				"check_rule_yaml": schema.StringAttribute{
					Required: true,
				},
				"url": schema.StringAttribute{
					Computed: true,
				},
			},
		},
	}

	r.Create(context.Background(), req, resp)

	// Should have error due to invalid YAML
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

// testCheckRuleSchema returns the schema (incl. the computed url attribute) used
// by the lifecycle tests.
func testCheckRuleSchema() schema.Schema {
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
			"check_rule_yaml": schema.StringAttribute{
				Required: true,
			},
			"url": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func TestCheckRuleResource_Create(t *testing.T) {
	mockClient := new(MockClient)
	r := &CheckRuleResource{client: mockClient}

	testYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-rule
spec:
  groups:
    - name: TestGroup
      rules:
        - alert: TestAlert
          expr: up == 0`
	testDataset := "test-dataset"
	testURL := "https://app.dash0.com/goto/alerting/check-rules?check_rule_id=internal-uuid"

	plan := tfsdk.Plan{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":          tftypes.NewValue(tftypes.String, ""),
			"id":              tftypes.NewValue(tftypes.String, nil),
			"dataset":         tftypes.NewValue(tftypes.String, testDataset),
			"check_rule_yaml": tftypes.NewValue(tftypes.String, testYaml),
			"url":             tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: testCheckRuleSchema(),
	}
	resp := resource.CreateResponse{
		State: tfsdk.State{Schema: plan.Schema},
	}
	req := resource.CreateRequest{Plan: plan}

	mockClient.On("CreateCheckRule", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// After create, the URL is resolved by origin (generated tf_-prefixed value).
	mockClient.On("ResolveCheckRule", mock.Anything, mock.Anything, testDataset).Return("test-id", testURL, nil)

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	var resultState checkRuleModel
	diags := resp.State.Get(context.Background(), &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")
	assert.Equal(t, testURL, resultState.URL.ValueString())
}

func TestCheckRuleResource_Update(t *testing.T) {
	mockClient := new(MockClient)
	r := &CheckRuleResource{client: mockClient}

	testOrigin := "test-origin"
	testDataset := "test-dataset"
	testYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-rule
spec:
  groups:
    - name: TestGroup
      rules:
        - alert: TestAlert
          expr: up == 0`
	testURL := "https://app.dash0.com/goto/alerting/check-rules?check_rule_id=internal-uuid"

	state := tfsdk.State{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":          tftypes.NewValue(tftypes.String, testOrigin),
			"id":              tftypes.NewValue(tftypes.String, nil),
			"dataset":         tftypes.NewValue(tftypes.String, testDataset),
			"check_rule_yaml": tftypes.NewValue(tftypes.String, testYaml),
			"url":             tftypes.NewValue(tftypes.String, testURL),
		}),
		Schema: testCheckRuleSchema(),
	}
	plan := tfsdk.Plan{
		Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{
			"origin":          tftypes.NewValue(tftypes.String, testOrigin),
			"id":              tftypes.NewValue(tftypes.String, nil),
			"dataset":         tftypes.NewValue(tftypes.String, testDataset),
			"check_rule_yaml": tftypes.NewValue(tftypes.String, testYaml+"\n          for: 5m"),
			"url":             tftypes.NewValue(tftypes.String, testURL),
		}),
		Schema: state.Schema,
	}
	req := resource.UpdateRequest{State: state, Plan: plan}
	resp := resource.UpdateResponse{State: state}

	mockClient.On("UpdateCheckRule", mock.Anything, testOrigin, mock.Anything, testDataset).Return(nil)

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())

	// URL is carried over from prior state (Update does not re-resolve it).
	var resultState checkRuleModel
	diags := resp.State.Get(context.Background(), &resultState)
	require.False(t, diags.HasError(), "state cannot be unmarshalled")
	assert.Equal(t, testURL, resultState.URL.ValueString())
}

func TestCheckRuleResource_SharingAnnotationTriggersReplan(t *testing.T) {
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
  name: my-check-rule
  annotations:
    dash0.com/sharing: all-users
spec:
  groups:
    - name: test-group
      rules:
        - alert: TestAlert
          expr: test > 0
`),
			stateValue: types.StringValue(`
metadata:
  name: my-check-rule
  annotations:
    dash0.com/sharing: private
spec:
  groups:
    - name: test-group
      rules:
        - alert: TestAlert
          expr: test > 0
`),
			expectedPlan: types.StringValue(`
metadata:
  name: my-check-rule
  annotations:
    dash0.com/sharing: all-users
spec:
  groups:
    - name: test-group
      rules:
        - alert: TestAlert
          expr: test > 0
`),
			description: "Should use config value when dash0.com/sharing annotation changed on check rule",
		},
		{
			name: "dash0.com/sharing same - should suppress replan",
			configValue: types.StringValue(`
metadata:
  name: my-check-rule
  annotations:
    dash0.com/sharing: all-users
spec:
  groups:
    - name: test-group
      rules:
        - alert: TestAlert
          expr: test > 0
`),
			stateValue: types.StringValue(`
metadata:
  name: my-check-rule
  annotations:
    dash0.com/sharing: all-users
spec:
  groups:
    - name: test-group
      rules:
        - alert: TestAlert
          expr: test > 0
`),
			expectedPlan: types.StringValue(`
metadata:
  name: my-check-rule
  annotations:
    dash0.com/sharing: all-users
spec:
  groups:
    - name: test-group
      rules:
        - alert: TestAlert
          expr: test > 0
`),
			description: "Should use state value when dash0.com/sharing annotation is the same on check rule",
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

func TestCheckRuleResource_ReadError(t *testing.T) {
	mockClient := &MockClient{}
	r := &CheckRuleResource{client: mockClient}

	// Mock client to return error - GetCheckRule(ctx, origin, dataset)
	mockClient.On("GetCheckRule", mock.Anything, "test-origin", "test-dataset").Return(
		"", errors.New("not found"))

	req := resource.ReadRequest{}
	resp := &resource.ReadResponse{}

	// Create mock state

	req.State = tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":          tftypes.String,
					"id":              tftypes.String,
					"dataset":         tftypes.String,
					"check_rule_yaml": tftypes.String,
					"url":             tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":          tftypes.NewValue(tftypes.String, "test-origin"),
				"id":              tftypes.NewValue(tftypes.String, nil),
				"dataset":         tftypes.NewValue(tftypes.String, "test-dataset"),
				"check_rule_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
				"url":             tftypes.NewValue(tftypes.String, nil),
			},
		),
		Schema: schema.Schema{
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
				"check_rule_yaml": schema.StringAttribute{
					Required: true,
				},
				"url": schema.StringAttribute{
					Computed: true,
				},
			},
		},
	}

	r.Read(context.Background(), req, resp)

	// Should have error from client
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")

	mockClient.AssertExpectations(t)
}
