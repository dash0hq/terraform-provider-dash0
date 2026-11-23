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

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const (
	awsLogPolicyTestOrigin = "tf_11111111-2222-3333-4444-555555555555"
	// onNoLongerMatching is omitted on purpose: the API returns it as keep.
	awsLogPolicyTestYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0AwsLogGroupInstrumentationPolicy
spec:
  rules:
    - conditions:
        - key: dash0.cloud.tags.team
          operator: is_one_of
          values: [checkout, payments]
        - key: dash0.cloud.tags.env
          operator: is
          value: production`
	awsLogPolicyAPIJSON = `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsLogGroupInstrumentationPolicy","metadata":{},` +
		`"spec":{"onNoLongerMatching":"keep","rules":[{"conditions":[` +
		`{"key":"dash0.cloud.tags.team","operator":"is_one_of","values":["checkout","payments"]},` +
		`{"key":"dash0.cloud.tags.env","operator":"is","value":"production"}]}]}}`
)

var errConflict = &dash0.APIError{StatusCode: 409, Status: "409 Conflict"}

func awsLogPolicyTestSchema() resource.SchemaResponse {
	var resp resource.SchemaResponse
	(&AwsLogGroupPolicyResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp
}

func awsLogPolicyRaw(integrationID, origin, disposition, yamlStr interface{}) tftypes.Value {
	objType := awsLogPolicyTestSchema().Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	return tftypes.NewValue(objType, map[string]tftypes.Value{
		"integration_id":            tftypes.NewValue(tftypes.String, integrationID),
		"origin":                    tftypes.NewValue(tftypes.String, origin),
		"deletion_disposition":      tftypes.NewValue(tftypes.String, disposition),
		"aws_log_group_policy_yaml": tftypes.NewValue(tftypes.String, yamlStr),
	})
}

func awsLogPolicyState(integrationID, origin, disposition, yamlStr interface{}) tfsdk.State {
	return tfsdk.State{Raw: awsLogPolicyRaw(integrationID, origin, disposition, yamlStr), Schema: awsLogPolicyTestSchema().Schema}
}

func awsLogPolicyModelOf(t *testing.T, s tfsdk.State) awsLogGroupPolicyModel {
	t.Helper()
	var m awsLogGroupPolicyModel
	assert.False(t, s.Get(context.Background(), &m).HasError())
	return m
}

func TestAwsLogGroupPolicyResource_Schema(t *testing.T) {
	var meta resource.MetadataResponse
	(&AwsLogGroupPolicyResource{}).Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, &meta)
	assert.Equal(t, "dash0_aws_log_group_policy", meta.TypeName)

	s := awsLogPolicyTestSchema().Schema
	assert.True(t, s.Attributes["integration_id"].IsRequired())
	assert.True(t, s.Attributes["aws_log_group_policy_yaml"].IsRequired())
	assert.True(t, s.Attributes["origin"].IsComputed())
	assert.True(t, s.Attributes["deletion_disposition"].IsOptional())
	assert.NotContains(t, s.Attributes, "dataset")
}

func TestAwsLogGroupPolicyResource_ValidateConfig(t *testing.T) {
	validate := func(id, disposition, y string) *resource.ValidateConfigResponse {
		resp := &resource.ValidateConfigResponse{}
		(&AwsLogGroupPolicyResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
			Config: tfsdk.Config{Raw: awsLogPolicyRaw(id, nil, disposition, y), Schema: awsLogPolicyTestSchema().Schema},
		}, resp)
		return resp
	}

	assert.False(t, validate(awsScrapeTestIntegrationID, "remove", awsLogPolicyTestYaml).Diagnostics.HasError())

	for name, tc := range map[string]struct{ id, disposition, yaml, want string }{
		"bad id":          {"not-a-uuid", "keep", awsLogPolicyTestYaml, "Invalid integration_id"},
		"bad disposition": {awsScrapeTestIntegrationID, "delete", awsLogPolicyTestYaml, "Invalid deletion_disposition"},
		"missing rules":   {awsScrapeTestIntegrationID, "keep", "apiVersion: dash0.com/v1alpha1\nkind: Dash0AwsLogGroupInstrumentationPolicy\nspec:\n  rule: []\n", "Invalid AWS log-group policy"},
		"bad operator":    {awsScrapeTestIntegrationID, "keep", strings.Replace(awsLogPolicyTestYaml, "operator: is\n", "operator: matches\n", 1) + "\n", "Invalid AWS log-group policy"},
	} {
		t.Run(name, func(t *testing.T) {
			d := validate(tc.id, tc.disposition, tc.yaml).Diagnostics
			assert.True(t, d.HasError())
			assert.Contains(t, d.Errors()[0].Summary(), tc.want)
		})
	}
}

func TestAwsLogGroupPolicyResource_Create(t *testing.T) {
	m := &MockClient{}
	m.On("UpdateAwsLogGroupPolicy", mock.Anything, awsScrapeTestIntegrationID,
		mock.MatchedBy(func(o string) bool { return strings.HasPrefix(o, "tf_") }),
		mock.MatchedBy(func(body string) bool { return strings.Contains(body, `"dash0.cloud.tags.team"`) }),
	).Return(nil)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: awsLogPolicyRaw(awsScrapeTestIntegrationID, tftypes.UnknownValue, "keep", awsLogPolicyTestYaml), Schema: awsLogPolicyTestSchema().Schema}}
	resp := &resource.CreateResponse{State: awsLogPolicyState(nil, nil, nil, nil)}
	(&AwsLogGroupPolicyResource{client: m}).Create(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	assert.True(t, strings.HasPrefix(awsLogPolicyModelOf(t, resp.State).Origin.ValueString(), "tf_"))
	m.AssertExpectations(t)
}

func TestAwsLogGroupPolicyResource_Create_OwnedElsewhere(t *testing.T) {
	m := &MockClient{}
	m.On("UpdateAwsLogGroupPolicy", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errConflict)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: awsLogPolicyRaw(awsScrapeTestIntegrationID, tftypes.UnknownValue, "keep", awsLogPolicyTestYaml), Schema: awsLogPolicyTestSchema().Schema}}
	resp := &resource.CreateResponse{State: awsLogPolicyState(nil, nil, nil, nil)}
	(&AwsLogGroupPolicyResource{client: m}).Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "terraform import")
	assert.True(t, resp.State.Raw.Equal(awsLogPolicyRaw(nil, nil, nil, nil)))
}

// Changing only deletion_disposition is local to Terraform: nothing is written.
func TestAwsLogGroupPolicyResource_Update_DispositionOnlyMakesNoAPICall(t *testing.T) {
	m := &MockClient{}
	req := resource.UpdateRequest{
		State: awsLogPolicyState(awsScrapeTestIntegrationID, awsLogPolicyTestOrigin, "keep", awsLogPolicyTestYaml),
		Plan:  tfsdk.Plan{Raw: awsLogPolicyRaw(awsScrapeTestIntegrationID, awsLogPolicyTestOrigin, "remove", awsLogPolicyTestYaml), Schema: awsLogPolicyTestSchema().Schema},
	}
	resp := &resource.UpdateResponse{State: req.State}
	(&AwsLogGroupPolicyResource{client: m}).Update(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	assert.Equal(t, "remove", awsLogPolicyModelOf(t, resp.State).DeletionDisposition.ValueString())
	m.AssertExpectations(t)
}

func TestAwsLogGroupPolicyResource_Update_WritesWithStateOrigin(t *testing.T) {
	newYaml := strings.Replace(awsLogPolicyTestYaml, "production", "staging", 1)
	m := &MockClient{}
	m.On("UpdateAwsLogGroupPolicy", mock.Anything, awsScrapeTestIntegrationID, awsLogPolicyTestOrigin, mock.MatchedBy(func(b string) bool { return strings.Contains(b, "staging") })).Return(nil)

	req := resource.UpdateRequest{
		State: awsLogPolicyState(awsScrapeTestIntegrationID, awsLogPolicyTestOrigin, "keep", awsLogPolicyTestYaml),
		Plan:  tfsdk.Plan{Raw: awsLogPolicyRaw(awsScrapeTestIntegrationID, tftypes.UnknownValue, "keep", newYaml), Schema: awsLogPolicyTestSchema().Schema},
	}
	resp := &resource.UpdateResponse{State: req.State}
	(&AwsLogGroupPolicyResource{client: m}).Update(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	m.AssertExpectations(t)
}

func TestAwsLogGroupPolicyResource_Read(t *testing.T) {
	changedJSON := strings.Replace(awsLogPolicyAPIJSON, "production", "staging", 1)
	removeJSON := strings.Replace(awsLogPolicyAPIJSON, `"onNoLongerMatching":"keep"`, `"onNoLongerMatching":"remove"`, 1)
	tests := []struct {
		name        string
		getJSON     string
		deleted     bool
		getErr      error
		wantRemoved bool
		wantYAML    string
		wantErr     bool
	}{
		{name: "server default for onNoLongerMatching is not drift", getJSON: awsLogPolicyAPIJSON, wantYAML: awsLogPolicyTestYaml},
		{name: "changed rule is drift", getJSON: changedJSON, wantYAML: changedJSON},
		{name: "changed onNoLongerMatching is drift", getJSON: removeJSON, wantYAML: removeJSON},
		{name: "tombstone is removed from state", getJSON: awsLogPolicyAPIJSON, deleted: true, wantRemoved: true},
		{name: "not found is removed from state", getErr: errNotFound, wantRemoved: true},
		{name: "server error surfaces", getErr: errServer, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MockClient{}
			m.On("GetAwsLogGroupPolicy", mock.Anything, awsScrapeTestIntegrationID).Return(tc.getJSON, awsLogPolicyTestOrigin, tc.deleted, tc.getErr)

			state := awsLogPolicyState(awsScrapeTestIntegrationID, awsLogPolicyTestOrigin, "keep", awsLogPolicyTestYaml)
			resp := &resource.ReadResponse{State: state}
			(&AwsLogGroupPolicyResource{client: m}).Read(context.Background(), resource.ReadRequest{State: state}, resp)

			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
			if tc.wantErr {
				return
			}
			if tc.wantRemoved {
				assert.True(t, resp.State.Raw.IsNull())
				return
			}
			assert.Equal(t, tc.wantYAML, awsLogPolicyModelOf(t, resp.State).AwsLogGroupPolicyYaml.ValueString())
		})
	}
}

func TestAwsLogGroupPolicyResource_Delete_SendsDisposition(t *testing.T) {
	for _, tc := range []struct {
		name, disposition string
		err               error
		wantErr           bool
	}{
		{"keep", "keep", nil, false},
		{"remove", "remove", nil, false},
		{"already gone", "keep", errNotFound, false},
		{"server error", "keep", errServer, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &MockClient{}
			m.On("DeleteAwsLogGroupPolicy", mock.Anything, awsScrapeTestIntegrationID, tc.disposition).Return(tc.err)

			state := awsLogPolicyState(awsScrapeTestIntegrationID, awsLogPolicyTestOrigin, tc.disposition, awsLogPolicyTestYaml)
			resp := &resource.DeleteResponse{State: state}
			(&AwsLogGroupPolicyResource{client: m}).Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
			m.AssertExpectations(t)
		})
	}
}

func TestAwsLogGroupPolicyResource_ImportState(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		deleted bool
		getErr  error
		wantErr string
	}{
		{name: "active policy", id: awsScrapeTestIntegrationID},
		{name: "invalid id", id: "dataset,origin", wantErr: "Invalid Import ID"},
		{name: "tombstone", id: awsScrapeTestIntegrationID, deleted: true, wantErr: "Error Importing"},
		{name: "no policy", id: awsScrapeTestIntegrationID, getErr: errNotFound, wantErr: "Error Importing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MockClient{}
			if isUUID(tc.id) {
				m.On("GetAwsLogGroupPolicy", mock.Anything, tc.id).Return(awsLogPolicyAPIJSON, awsLogPolicyTestOrigin, tc.deleted, tc.getErr)
			}

			resp := &resource.ImportStateResponse{State: awsLogPolicyState(nil, nil, nil, nil)}
			(&AwsLogGroupPolicyResource{client: m}).ImportState(context.Background(), resource.ImportStateRequest{ID: tc.id}, resp)

			m.AssertExpectations(t)
			if tc.wantErr != "" {
				assert.True(t, resp.Diagnostics.HasError())
				assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tc.wantErr)
				return
			}
			assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
			assert.True(t, resp.State.Raw.Equal(awsLogPolicyRaw(tc.id, awsLogPolicyTestOrigin, "keep", awsLogPolicyAPIJSON)))
		})
	}
}
