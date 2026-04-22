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

func TestRecordingRuleResourceModel(t *testing.T) {
	origin := "test-origin"
	dataset := "test-dataset"
	recordingRuleYaml := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-recording-rule
spec:
  groups:
    - name: TestGroup
      rules:
        - record: test_metric
          expr: sum(rate(http_requests_total[5m]))`

	m := recordingRuleModel{
		Origin:            types.StringValue(origin),
		Dataset:           types.StringValue(dataset),
		RecordingRuleYaml: types.StringValue(recordingRuleYaml),
	}

	assert.Equal(t, origin, m.Origin.ValueString())
	assert.Equal(t, dataset, m.Dataset.ValueString())
	assert.Equal(t, recordingRuleYaml, m.RecordingRuleYaml.ValueString())
}

func TestNewRecordingRuleResource(t *testing.T) {
	resource := NewRecordingRuleResource()
	assert.NotNil(t, resource)

	// Check that it's the correct type
	_, ok := resource.(*RecordingRuleResource)
	assert.True(t, ok)
}

func TestRecordingRuleResource_Metadata(t *testing.T) {
	r := &RecordingRuleResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{
		ProviderTypeName: "dash0",
	}

	r.Metadata(context.Background(), req, resp)

	assert.Equal(t, "dash0_recording_rule", resp.TypeName)
}

func TestRecordingRuleResource_Schema(t *testing.T) {
	r := &RecordingRuleResource{}
	resp := &resource.SchemaResponse{}
	req := resource.SchemaRequest{}

	r.Schema(context.Background(), req, resp)

	// Verify schema has the expected attributes
	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "dataset")
	assert.Contains(t, resp.Schema.Attributes, "recording_rule_yaml")

	// Verify origin is computed
	originAttr := resp.Schema.Attributes["origin"]
	assert.True(t, originAttr.IsComputed())
	assert.False(t, originAttr.IsRequired())

	// Verify dataset is required
	datasetAttr := resp.Schema.Attributes["dataset"]
	assert.True(t, datasetAttr.IsRequired())
	assert.False(t, datasetAttr.IsComputed())

	// Verify recording_rule_yaml is required
	yamlAttr := resp.Schema.Attributes["recording_rule_yaml"]
	assert.True(t, yamlAttr.IsRequired())
	assert.False(t, yamlAttr.IsComputed())
}

func TestRecordingRuleResource_Configure(t *testing.T) {
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
			r := &RecordingRuleResource{}
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

func TestRecordingRuleResource_Create_InvalidYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &RecordingRuleResource{client: mockClient}

	// Create request with invalid YAML
	req := resource.CreateRequest{}
	resp := &resource.CreateResponse{}

	// Set up the request state with invalid YAML
	req.Plan = tfsdk.Plan{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":              tftypes.String,
					"dataset":             tftypes.String,
					"recording_rule_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":              tftypes.NewValue(tftypes.String, "test-origin"),
				"dataset":             tftypes.NewValue(tftypes.String, "test-dataset"),
				"recording_rule_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: content: ["),
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
				"recording_rule_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	r.Create(context.Background(), req, resp)

	// Should have error due to invalid YAML
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

func TestRecordingRuleResource_ReadError(t *testing.T) {
	mockClient := &MockClient{}
	r := &RecordingRuleResource{client: mockClient}

	// Mock client to return error - GetRecordingRule(ctx, origin, dataset)
	mockClient.On("GetRecordingRule", mock.Anything, "test-origin", "test-dataset").Return(
		"", errors.New("not found"))

	req := resource.ReadRequest{}
	resp := &resource.ReadResponse{}

	// Create mock state
	req.State = tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":              tftypes.String,
					"dataset":             tftypes.String,
					"recording_rule_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":              tftypes.NewValue(tftypes.String, "test-origin"),
				"dataset":             tftypes.NewValue(tftypes.String, "test-dataset"),
				"recording_rule_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
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
				"recording_rule_yaml": schema.StringAttribute{
					Required: true,
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
