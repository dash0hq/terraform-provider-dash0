package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	awsScrapeTestIntegrationID = "27755ed0-0009-422d-8c11-19c6dd72df98"
	awsScrapeTestOrigin        = "tf_11111111-2222-3333-4444-555555555555"
	awsScrapeTestYaml          = `apiVersion: dash0.com/v1alpha1
kind: Dash0AwsMetricScrapeConfig
spec:
  namespaces:
    - namespace: AWS/SQS
      metrics: [NumberOfMessagesSent, ApproximateAgeOfOldestMessage]
    - namespace: AWS/Lambda
      metrics: [Invocations, Errors]`
)

func awsScrapeTestSchema() resource.SchemaResponse {
	var resp resource.SchemaResponse
	(&AwsMetricScrapeConfigResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp
}

// awsScrapeRaw builds a resource object value; nil means null, tftypes.UnknownValue means unknown.
func awsScrapeRaw(integrationID, origin, yamlStr interface{}) tftypes.Value {
	objType := awsScrapeTestSchema().Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	return tftypes.NewValue(objType, map[string]tftypes.Value{
		"integration_id":                tftypes.NewValue(tftypes.String, integrationID),
		"origin":                        tftypes.NewValue(tftypes.String, origin),
		"aws_metric_scrape_config_yaml": tftypes.NewValue(tftypes.String, yamlStr),
	})
}

func awsScrapeState(integrationID, origin, yamlStr interface{}) tfsdk.State {
	return tfsdk.State{Raw: awsScrapeRaw(integrationID, origin, yamlStr), Schema: awsScrapeTestSchema().Schema}
}

func awsScrapeGetModel(t *testing.T, s tfsdk.State) awsMetricScrapeConfigModel {
	t.Helper()
	var m awsMetricScrapeConfigModel
	assert.False(t, s.Get(context.Background(), &m).HasError())
	return m
}

func TestAwsMetricScrapeConfigResource_Metadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	(&AwsMetricScrapeConfigResource{}).Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_aws_metric_scrape_config", resp.TypeName)
}

func TestAwsMetricScrapeConfigResource_Schema(t *testing.T) {
	s := awsScrapeTestSchema().Schema
	assert.True(t, s.Attributes["integration_id"].IsRequired())
	assert.True(t, s.Attributes["aws_metric_scrape_config_yaml"].IsRequired())
	assert.True(t, s.Attributes["origin"].IsComputed())
	assert.NotContains(t, s.Attributes, "dataset", "the integration decides the dataset")
}

func TestAwsMetricScrapeConfigResource_ValidateConfig(t *testing.T) {
	validate := func(id, y string) *resource.ValidateConfigResponse {
		resp := &resource.ValidateConfigResponse{}
		(&AwsMetricScrapeConfigResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
			Config: tfsdk.Config{Raw: awsScrapeRaw(id, nil, y), Schema: awsScrapeTestSchema().Schema},
		}, resp)
		return resp
	}

	assert.False(t, validate(awsScrapeTestIntegrationID, awsScrapeTestYaml).Diagnostics.HasError())

	for _, id := range []string{"", "not-a-uuid", "27755ed00009422d8c1119c6dd72df98", "{27755ed0-0009-422d-8c11-19c6dd72df98}"} {
		d := validate(id, awsScrapeTestYaml).Diagnostics
		assert.True(t, d.HasError(), id)
		assert.Contains(t, d.Errors()[0].Summary(), "Invalid integration_id")
	}

	d := validate(awsScrapeTestIntegrationID, "spec: [").Diagnostics
	assert.True(t, d.HasError())
	assert.Contains(t, d.Errors()[0].Summary(), "Invalid YAML")
}

func TestAwsMetricScrapeConfigResource_Create(t *testing.T) {
	m := &MockClient{}
	m.On("UpdateAwsMetricScrapeConfig", mock.Anything, awsScrapeTestIntegrationID,
		mock.MatchedBy(func(o string) bool { return strings.HasPrefix(o, "tf_") }),
		mock.MatchedBy(func(body string) bool { return strings.Contains(body, `"namespace":"AWS/SQS"`) }),
	).Return(nil)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: awsScrapeRaw(awsScrapeTestIntegrationID, tftypes.UnknownValue, awsScrapeTestYaml), Schema: awsScrapeTestSchema().Schema}}
	resp := &resource.CreateResponse{State: awsScrapeState(nil, nil, nil)}
	(&AwsMetricScrapeConfigResource{client: m}).Create(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	got := awsScrapeGetModel(t, resp.State)
	assert.True(t, strings.HasPrefix(got.Origin.ValueString(), "tf_"))
	assert.Equal(t, awsScrapeTestYaml, got.AwsMetricScrapeConfigYaml.ValueString())
	m.AssertExpectations(t)
}

func TestAwsMetricScrapeConfigResource_Create_InvalidYAML(t *testing.T) {
	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: awsScrapeRaw(awsScrapeTestIntegrationID, tftypes.UnknownValue, "spec: ["), Schema: awsScrapeTestSchema().Schema}}
	resp := &resource.CreateResponse{State: awsScrapeState(nil, nil, nil)}
	(&AwsMetricScrapeConfigResource{client: &MockClient{}}).Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

func TestAwsMetricScrapeConfigResource_Create_ClientError(t *testing.T) {
	m := &MockClient{}
	m.On("UpdateAwsMetricScrapeConfig", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errServer)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: awsScrapeRaw(awsScrapeTestIntegrationID, tftypes.UnknownValue, awsScrapeTestYaml), Schema: awsScrapeTestSchema().Schema}}
	resp := &resource.CreateResponse{State: awsScrapeState(nil, nil, nil)}
	(&AwsMetricScrapeConfigResource{client: m}).Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.Equal(awsScrapeRaw(nil, nil, nil)), "nothing must be stored when the write fails")
}

func TestAwsMetricScrapeConfigResource_Update(t *testing.T) {
	const newYaml = awsScrapeTestYaml + "\n    - namespace: AWS/EC2\n      metrics: [CPUUtilization]"
	tests := []struct {
		name        string
		stateOrigin interface{}
		wantOrigin  func(string) bool
	}{
		{"keeps the state origin", awsScrapeTestOrigin, func(o string) bool { return o == awsScrapeTestOrigin }},
		{"generates an origin for an imported config without one", nil, func(o string) bool {
			return strings.HasPrefix(o, "tf_") && o != awsScrapeTestOrigin
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MockClient{}
			m.On("UpdateAwsMetricScrapeConfig", mock.Anything, awsScrapeTestIntegrationID, mock.MatchedBy(tc.wantOrigin), mock.Anything).Return(nil)

			planOrigin := tc.stateOrigin
			if planOrigin == nil {
				planOrigin = tftypes.UnknownValue
			}
			req := resource.UpdateRequest{
				State: awsScrapeState(awsScrapeTestIntegrationID, tc.stateOrigin, awsScrapeTestYaml),
				Plan:  tfsdk.Plan{Raw: awsScrapeRaw(awsScrapeTestIntegrationID, planOrigin, newYaml), Schema: awsScrapeTestSchema().Schema},
			}
			resp := &resource.UpdateResponse{State: req.State}
			(&AwsMetricScrapeConfigResource{client: m}).Update(context.Background(), req, resp)

			assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
			got := awsScrapeGetModel(t, resp.State)
			assert.True(t, tc.wantOrigin(got.Origin.ValueString()), got.Origin.ValueString())
			assert.Equal(t, newYaml, got.AwsMetricScrapeConfigYaml.ValueString())
			m.AssertExpectations(t)
		})
	}
}

func TestAwsMetricScrapeConfigResource_Delete(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"cleared", nil, false},
		{"integration gone is success", errNotFound, false},
		{"500 surfaces", errServer, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &MockClient{}
			m.On("DeleteAwsMetricScrapeConfig", mock.Anything, awsScrapeTestIntegrationID).Return(tc.err)

			state := awsScrapeState(awsScrapeTestIntegrationID, awsScrapeTestOrigin, awsScrapeTestYaml)
			resp := &resource.DeleteResponse{State: state}
			(&AwsMetricScrapeConfigResource{client: m}).Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
			m.AssertExpectations(t)
		})
	}
}

func TestAwsMetricScrapeConfigResource_ImportState(t *testing.T) {
	const apiJSON = `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","metadata":{"name":"dash0-integration-test"},"spec":{"namespaces":[{"namespace":"AWS/Lambda","metrics":["Errors"]}]}}`
	const clearedJSON = `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","metadata":{"name":"dash0-integration-test"},"spec":{"namespaces":[]}}`

	tests := []struct {
		name       string
		id         string
		getJSON    string
		getOrigin  string
		getErr     error
		wantErr    string
		wantOrigin interface{}
	}{
		{name: "config made in the UI has no origin", id: awsScrapeTestIntegrationID, getJSON: apiJSON, wantOrigin: nil},
		{name: "config with origin", id: awsScrapeTestIntegrationID, getJSON: apiJSON, getOrigin: awsScrapeTestOrigin, wantOrigin: awsScrapeTestOrigin},
		{name: "invalid id", id: "dataset,origin", wantErr: "Invalid Import ID"},
		{name: "nothing configured", id: awsScrapeTestIntegrationID, getJSON: clearedJSON, wantErr: "Error Importing"},
		{name: "integration not found", id: awsScrapeTestIntegrationID, getErr: errNotFound, wantErr: "Error Importing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MockClient{}
			if tc.getJSON != "" || tc.getErr != nil {
				m.On("GetAwsMetricScrapeConfig", mock.Anything, tc.id).Return(tc.getJSON, tc.getOrigin, tc.getErr)
			}

			resp := &resource.ImportStateResponse{State: awsScrapeState(nil, nil, nil)}
			(&AwsMetricScrapeConfigResource{client: m}).ImportState(context.Background(), resource.ImportStateRequest{ID: tc.id}, resp)

			m.AssertExpectations(t)
			if tc.wantErr != "" {
				assert.True(t, resp.Diagnostics.HasError())
				assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tc.wantErr)
				return
			}
			assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
			assert.True(t, resp.State.Raw.Equal(awsScrapeRaw(tc.id, tc.wantOrigin, tc.getJSON)))
		})
	}
}
