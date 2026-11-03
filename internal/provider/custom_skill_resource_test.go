package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const customSkillTestID = "8b3f6a52-6d8d-4a0f-9b40-0e6a3c6c2f10"

const customSkillTestYaml = `kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: incident-response
        description: Triage incidents
        ---
        Follow the runbook.`

// customSkillTestState builds a state from the resource's real schema. A nil
// value is a null attribute.
func customSkillTestState(t *testing.T, origin, id, skillYaml interface{}) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var schemaResp resource.SchemaResponse
	NewCustomSkillResource().Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	require.True(t, ok)
	return tfsdk.State{
		Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
			"origin":            tftypes.NewValue(tftypes.String, origin),
			"id":                tftypes.NewValue(tftypes.String, id),
			"custom_skill_yaml": tftypes.NewValue(tftypes.String, skillYaml),
		}),
		Schema: schemaResp.Schema,
	}
}

// customSkillNullState is the state Terraform passes for a resource that does not exist yet.
func customSkillNullState(t *testing.T) tfsdk.State {
	t.Helper()
	state := customSkillTestState(t, nil, nil, nil)
	state.Raw = tftypes.NewValue(state.Raw.Type(), nil)
	return state
}

func customSkillTestPlan(t *testing.T, origin, id, skillYaml interface{}) tfsdk.Plan {
	t.Helper()
	state := customSkillTestState(t, origin, id, skillYaml)
	return tfsdk.Plan{Raw: state.Raw, Schema: state.Schema}
}

func TestCustomSkillResource_Metadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewCustomSkillResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_custom_skill", resp.TypeName)
}

func TestCustomSkillResource_Schema(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewCustomSkillResource().Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotContains(t, resp.Schema.Attributes, "dataset")
	assert.NotContains(t, resp.Schema.Attributes, "url")
	assert.NotContains(t, resp.Schema.Attributes, "enabled")

	for _, name := range []string{"origin", "id"} {
		attr := resp.Schema.Attributes[name]
		require.NotNil(t, attr, name)
		assert.True(t, attr.IsComputed(), name)
		assert.False(t, attr.IsRequired(), name)
	}
	yamlAttr := resp.Schema.Attributes["custom_skill_yaml"]
	require.NotNil(t, yamlAttr)
	assert.True(t, yamlAttr.IsRequired())
	assert.False(t, yamlAttr.IsComputed())
}

func TestCustomSkillResource_Configure(t *testing.T) {
	tests := []struct {
		name         string
		providerData interface{}
		expectError  bool
	}{
		{"valid client interface", resourceProviderData{client: &MockClient{}}, false},
		{"nil provider data", nil, false},
		{"invalid provider data type", "invalid", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &resource.ConfigureResponse{}
			(&CustomSkillResource{}).Configure(context.Background(), resource.ConfigureRequest{ProviderData: tc.providerData}, resp)
			assert.Equal(t, tc.expectError, resp.Diagnostics.HasError())
		})
	}
}

func TestValidateCustomSkillYAML(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantErrors int
		wantDetail string
	}{
		{name: "minimal skill", yaml: customSkillTestYaml},
		{
			name: "read and delete grants",
			yaml: customSkillTestYaml + `
  permissions:
    - role: basic_member
      actions: [custom_skill:read, custom_skill:delete]
      defaultEnabled: false`,
		},
		{
			name: "write grant is checked at plan time, not here",
			yaml: customSkillTestYaml + `
  permissions:
    - teamId: team-1
      actions: [custom_skill:read, custom_skill:write]`,
		},
		{
			name: "source",
			yaml: customSkillTestYaml + `
  source:
    provider: github
    repositoryUrl: https://example.com/skills
    path: skills/incident-response`,
			wantErrors: 1,
			wantDetail: "spec.source",
		},
		{
			name: "enabled annotation",
			yaml: `kind: Dash0Skill
metadata:
  annotations:
    dash0.com/enabled: "false"
spec:
  files: []`,
			wantErrors: 1,
			wantDetail: "defaultEnabled: false",
		},
		{
			name: "enabled label",
			yaml: `kind: Dash0Skill
metadata:
  labels:
    dash0.com/enabled: "true"
spec:
  files: []`,
			wantErrors: 1,
			wantDetail: "defaultEnabled: false",
		},
		{name: "invalid yaml is left to create and update", yaml: "spec: ["},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			validateCustomSkillYAML(tc.yaml, &diags)
			require.Equal(t, tc.wantErrors, diags.ErrorsCount(), diags)
			if tc.wantDetail != "" {
				assert.Contains(t, diags.Errors()[0].Summary()+diags.Errors()[0].Detail(), tc.wantDetail)
			}
		})
	}
}

func TestRejectCustomSkillWriteGrants(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantErrors int
	}{
		{name: "no permissions", yaml: customSkillTestYaml},
		{
			name: "read and delete grants",
			yaml: customSkillTestYaml + `
  permissions:
    - role: basic_member
      actions: [custom_skill:read, custom_skill:delete]`,
		},
		{
			name: "admin entry is exempt",
			yaml: customSkillTestYaml + `
  permissions:
    - role: admin
      actions: [custom_skill:read, custom_skill:write, custom_skill:delete]`,
		},
		{
			name: "write grants",
			yaml: customSkillTestYaml + `
  permissions:
    - role: basic_member
      actions: [custom_skill:read]
    - teamId: team-1
      actions: [custom_skill:read, custom_skill:write]
    - userId: user-1
      actions: [custom_skill:write]`,
			wantErrors: 2,
		},
		{name: "invalid yaml is left to create and update", yaml: "spec: ["},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			rejectCustomSkillWriteGrants(tc.yaml, &diags)
			require.Equal(t, tc.wantErrors, diags.ErrorsCount(), diags)
			if tc.wantErrors > 0 {
				assert.Contains(t, diags.Errors()[0].Detail(), "spec.permissions[1]")
			}
		})
	}
}

func TestCustomSkillResource_ModifyPlan_WriteGrants(t *testing.T) {
	writeGrant := customSkillTestYaml + `
  permissions:
    - teamId: team-1
      actions: [custom_skill:read, custom_skill:write]`

	tests := []struct {
		name    string
		state   tfsdk.State
		wantErr bool
	}{
		{"create", customSkillNullState(t), true},
		{"update of a Terraform-created skill", customSkillTestState(t, "tf_abc", customSkillTestID, customSkillTestYaml), true},
		{"update of an imported UI skill", customSkillTestState(t, nil, customSkillTestID, customSkillTestYaml), false},
		{"update of an imported CLI skill", customSkillTestState(t, "dash0-cli_abc", customSkillTestID, customSkillTestYaml), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := customSkillTestPlan(t, nil, nil, writeGrant)
			resp := &resource.ModifyPlanResponse{Plan: plan}
			(&CustomSkillResource{}).ModifyPlan(context.Background(), resource.ModifyPlanRequest{State: tc.state, Plan: plan}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError(), resp.Diagnostics)
		})
	}
}

func TestCustomSkillResource_Create(t *testing.T) {
	mockClient := &MockClient{}
	mockClient.On("CreateCustomSkill", mock.Anything, mock.MatchedBy(func(origin string) bool {
		return len(origin) > 3 && origin[:3] == "tf_"
	}), mock.Anything).Return(customSkillTestID, nil)

	resp := &resource.CreateResponse{State: customSkillTestState(t, nil, nil, nil)}
	(&CustomSkillResource{client: mockClient}).Create(context.Background(),
		resource.CreateRequest{Plan: customSkillTestPlan(t, nil, nil, customSkillTestYaml)}, resp)
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

	var got customSkillModel
	resp.State.Get(context.Background(), &got)
	assert.Equal(t, customSkillTestID, got.ID.ValueString())
	assert.Equal(t, customSkillTestYaml, got.CustomSkillYaml.ValueString())
	assert.Contains(t, got.Origin.ValueString(), "tf_")
	mockClient.AssertExpectations(t)
}

func TestCustomSkillResource_Create_InvalidYAML(t *testing.T) {
	resp := &resource.CreateResponse{State: customSkillTestState(t, nil, nil, nil)}
	(&CustomSkillResource{client: &MockClient{}}).Create(context.Background(),
		resource.CreateRequest{Plan: customSkillTestPlan(t, nil, nil, "invalid: yaml: content: [")}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

func TestCustomSkillResource_Create_ClientError(t *testing.T) {
	mockClient := &MockClient{}
	mockClient.On("CreateCustomSkill", mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("400 Bad Request"))
	resp := &resource.CreateResponse{State: customSkillTestState(t, nil, nil, nil)}
	(&CustomSkillResource{client: mockClient}).Create(context.Background(),
		resource.CreateRequest{Plan: customSkillTestPlan(t, nil, nil, customSkillTestYaml)}, resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "400 Bad Request")
}

func TestCustomSkillResource_Update_AddressesByOriginOrID(t *testing.T) {
	tests := []struct {
		name    string
		origin  interface{}
		address string
	}{
		{"terraform-created skill by origin", "tf_abc", "tf_abc"},
		{"imported UI skill by id", nil, customSkillTestID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockClient{}
			mockClient.On("UpdateCustomSkill", mock.Anything, tc.address, mock.Anything).Return(nil)
			resp := &resource.UpdateResponse{State: customSkillTestState(t, tc.origin, customSkillTestID, customSkillTestYaml)}
			(&CustomSkillResource{client: mockClient}).Update(context.Background(), resource.UpdateRequest{
				State: customSkillTestState(t, tc.origin, customSkillTestID, customSkillTestYaml),
				Plan:  customSkillTestPlan(t, nil, nil, customSkillTestYaml+"\n"),
			}, resp)
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

			var got customSkillModel
			resp.State.Get(context.Background(), &got)
			assert.Equal(t, customSkillTestID, got.ID.ValueString(), "the id is carried from state")
			mockClient.AssertExpectations(t)
		})
	}
}

func TestCustomSkillResource_Delete(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"success", nil, false},
		{"404 is success", errNotFound, false},
		{"500 surfaces", errServer, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockClient{}
			mockClient.On("DeleteCustomSkill", mock.Anything, "tf_abc").Return(tc.err)
			resp := &resource.DeleteResponse{}
			(&CustomSkillResource{client: mockClient}).Delete(context.Background(),
				resource.DeleteRequest{State: customSkillTestState(t, "tf_abc", customSkillTestID, customSkillTestYaml)}, resp)
			assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError())
			mockClient.AssertExpectations(t)
		})
	}
}

func TestCustomSkillResource_ImportState(t *testing.T) {
	importState := func(t *testing.T, id string, apiResponse string, err error) (*resource.ImportStateResponse, customSkillModel) {
		mockClient := &MockClient{}
		mockClient.On("GetCustomSkill", mock.Anything, id).Return(apiResponse, err)
		resp := &resource.ImportStateResponse{State: customSkillTestState(t, nil, nil, nil)}
		(&CustomSkillResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, resp)
		var got customSkillModel
		if !resp.Diagnostics.HasError() {
			resp.State.Get(context.Background(), &got)
		}
		return resp, got
	}

	t.Run("by origin", func(t *testing.T) {
		body := `{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/id":"` + customSkillTestID + `","dash0.com/origin":"tf_abc"}},"spec":{"files":[]}}`
		resp, got := importState(t, "tf_abc", body, nil)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.Equal(t, "tf_abc", got.Origin.ValueString())
		assert.Equal(t, customSkillTestID, got.ID.ValueString())
		assert.Equal(t, body, got.CustomSkillYaml.ValueString())
	})
	t.Run("UI-created skill by id leaves origin null", func(t *testing.T) {
		body := `{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/id":"` + customSkillTestID + `"}},"spec":{"files":[]}}`
		resp, got := importState(t, customSkillTestID, body, nil)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, got.Origin.IsNull())
		assert.Equal(t, customSkillTestID, got.ID.ValueString())
		assert.Equal(t, customSkillTestID, got.address())
	})
	t.Run("response without id label errors", func(t *testing.T) {
		resp, _ := importState(t, "tf_abc", `{"kind":"Dash0Skill","metadata":{},"spec":{"files":[]}}`, nil)
		assert.True(t, resp.Diagnostics.HasError())
	})
	t.Run("unknown skill errors", func(t *testing.T) {
		resp, _ := importState(t, "nope", "", errNotFound)
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "nope")
	})
}
