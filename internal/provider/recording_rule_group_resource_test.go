package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRecordingRuleGroupResourceModel(t *testing.T) {
	origin := "test-origin"
	dataset := "test-dataset"
	yaml := `kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: true
  display:
    name: HTTP Metrics
  interval: 1m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])`

	m := recordingRuleGroupModel{
		Origin:                 types.StringValue(origin),
		Dataset:                types.StringValue(dataset),
		RecordingRuleGroupYaml: types.StringValue(yaml),
	}

	assert.Equal(t, origin, m.Origin.ValueString())
	assert.Equal(t, dataset, m.Dataset.ValueString())
	assert.Equal(t, yaml, m.RecordingRuleGroupYaml.ValueString())
}

func TestNewRecordingRuleGroupResource(t *testing.T) {
	r := NewRecordingRuleGroupResource()
	assert.NotNil(t, r)

	_, ok := r.(*RecordingRuleGroupResource)
	assert.True(t, ok)
}

func TestRecordingRuleGroupResource_Metadata(t *testing.T) {
	r := &RecordingRuleGroupResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{
		ProviderTypeName: "dash0",
	}

	r.Metadata(context.Background(), req, resp)

	assert.Equal(t, "dash0_recording_rule_group", resp.TypeName)
}

func TestRecordingRuleGroupResource_Schema(t *testing.T) {
	r := &RecordingRuleGroupResource{}
	resp := &resource.SchemaResponse{}
	req := resource.SchemaRequest{}

	r.Schema(context.Background(), req, resp)

	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "dataset")
	assert.Contains(t, resp.Schema.Attributes, "recording_rule_group_yaml")

	originAttr := resp.Schema.Attributes["origin"]
	assert.True(t, originAttr.IsComputed())
	assert.False(t, originAttr.IsRequired())

	datasetAttr := resp.Schema.Attributes["dataset"]
	assert.True(t, datasetAttr.IsRequired())
	assert.False(t, datasetAttr.IsComputed())

	yamlAttr := resp.Schema.Attributes["recording_rule_group_yaml"]
	assert.True(t, yamlAttr.IsRequired())
	assert.False(t, yamlAttr.IsComputed())
}

func TestRecordingRuleGroupResource_Configure(t *testing.T) {
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
			r := &RecordingRuleGroupResource{}
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

func TestRecordingRuleGroupResource_Create_InvalidYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &RecordingRuleGroupResource{client: mockClient}

	req := resource.CreateRequest{}
	resp := &resource.CreateResponse{}

	req.Plan = tfsdk.Plan{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":                    tftypes.String,
					"dataset":                   tftypes.String,
					"recording_rule_group_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":                    tftypes.NewValue(tftypes.String, "test-origin"),
				"dataset":                   tftypes.NewValue(tftypes.String, "test-dataset"),
				"recording_rule_group_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: content: ["),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin": schema.StringAttribute{
					Computed: true,
				},
				"dataset": schema.StringAttribute{
					Required: true,
				},
				"recording_rule_group_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	r.Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

func TestRecordingRuleGroupResource_ReadError(t *testing.T) {
	mockClient := &MockClient{}
	r := &RecordingRuleGroupResource{client: mockClient}

	// Mock client to return error - GetRecordingRuleGroup(ctx, origin, dataset)
	mockClient.On("GetRecordingRuleGroup", mock.Anything, "test-origin", "test-dataset").Return(
		"", errors.New("not found"))

	req := resource.ReadRequest{}
	resp := &resource.ReadResponse{}

	req.State = tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":                    tftypes.String,
					"dataset":                   tftypes.String,
					"recording_rule_group_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":                    tftypes.NewValue(tftypes.String, "test-origin"),
				"dataset":                   tftypes.NewValue(tftypes.String, "test-dataset"),
				"recording_rule_group_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin": schema.StringAttribute{
					Computed: true,
				},
				"dataset": schema.StringAttribute{
					Required: true,
				},
				"recording_rule_group_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	r.Read(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")

	mockClient.AssertExpectations(t)
}
