package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

type testCustomSkillClient struct {
	client.Client
	getResponse string
	getError    error
	gotAddress  string
}

func (c *testCustomSkillClient) GetCustomSkill(_ context.Context, originOrID string) (string, error) {
	c.gotAddress = originOrID
	return c.getResponse, c.getError
}

func TestCustomSkillResource_ReadWithDiffs(t *testing.T) {
	stateYaml := `
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: "---\nname: incident-response\n---\nFollow the runbook.\n"
`
	stateYamlWithPermissions := stateYaml + `  permissions:
    - role: basic_member
      actions: [custom_skill:read]
`

	tests := []struct {
		name              string
		stateYaml         string
		apiResponse       string
		expectYamlUpdated bool
		expectWarning     bool
	}{
		{
			name:      "server-managed fields only - no drift",
			stateYaml: stateYaml,
			apiResponse: `{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/id":"` + customSkillTestID + `","dash0.com/origin":"tf_abc"}},` +
				`"spec":{"files":[{"relativePath":"SKILL.md","content":"---\nname: incident-response\n---\nFollow the runbook.\n"}],` +
				`"permissions":[{"role":"basic_member","actions":["custom_skill:read"],"defaultEnabled":true}]}}`,
		},
		{
			name:      "declared permissions match after the admin grant is ignored",
			stateYaml: stateYamlWithPermissions,
			apiResponse: `{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/id":"` + customSkillTestID + `"}},` +
				`"spec":{"files":[{"relativePath":"SKILL.md","content":"---\nname: incident-response\n---\nFollow the runbook.\n"}],` +
				`"permissions":[{"role":"admin","actions":["custom_skill:delete","custom_skill:read"],"defaultEnabled":false},` +
				`{"role":"basic_member","actions":["custom_skill:read"],"defaultEnabled":true}]}}`,
		},
		{
			name:      "declared permissions changed out of band - drift",
			stateYaml: stateYamlWithPermissions,
			apiResponse: `{"kind":"Dash0Skill","metadata":{},` +
				`"spec":{"files":[{"relativePath":"SKILL.md","content":"---\nname: incident-response\n---\nFollow the runbook.\n"}],` +
				`"permissions":[{"role":"basic_member","actions":["custom_skill:read","custom_skill:delete"],"defaultEnabled":true}]}}`,
			expectYamlUpdated: true,
		},
		{
			name:      "file content changed - drift",
			stateYaml: stateYaml,
			apiResponse: `{"kind":"Dash0Skill","metadata":{},` +
				`"spec":{"files":[{"relativePath":"SKILL.md","content":"---\nname: incident-response\n---\nEdited in the UI.\n"}]}}`,
			expectYamlUpdated: true,
		},
		{
			name:              "invalid response - update and warn",
			stateYaml:         stateYaml,
			apiResponse:       `not valid yaml {`,
			expectYamlUpdated: true,
			expectWarning:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testClient := &testCustomSkillClient{getResponse: tc.apiResponse}
			r := &CustomSkillResource{client: testClient}
			state := customSkillTestState(t, "tf_abc", customSkillTestID, tc.stateYaml)
			resp := resource.ReadResponse{State: state}

			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

			var got customSkillModel
			resp.State.Get(context.Background(), &got)
			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponse, got.CustomSkillYaml.ValueString())
			} else {
				assert.Equal(t, tc.stateYaml, got.CustomSkillYaml.ValueString())
			}
			assert.Equal(t, tc.expectWarning, resp.Diagnostics.WarningsCount() > 0)
			assert.Equal(t, "tf_abc", testClient.gotAddress)
		})
	}
}

func TestCustomSkillResource_ReadImportedUISkillAddressesByID(t *testing.T) {
	testClient := &testCustomSkillClient{
		getResponse: `{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/id":"` + customSkillTestID + `"}},"spec":{"files":[]}}`,
	}
	r := &CustomSkillResource{client: testClient}
	resp := readWithState(t, r, map[string]string{"id": customSkillTestID})
	require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	assert.Equal(t, customSkillTestID, testClient.gotAddress)
}

func TestCustomSkillResource_ReadNotFoundClearsState(t *testing.T) {
	r := &CustomSkillResource{client: &testCustomSkillClient{getError: errNotFound}}
	assertReadClearsState(t, readWithState(t, r, map[string]string{"origin": "tf_test"}))
}

func TestCustomSkillResource_ReadSoftDeletedClearsState(t *testing.T) {
	r := &CustomSkillResource{client: &testCustomSkillClient{getResponse: softDeletedJSON}}
	assertReadClearsState(t, readWithState(t, r, map[string]string{"origin": "tf_test"}))
}

func TestCustomSkillResource_ReadServerErrorKeepsStateAndErrors(t *testing.T) {
	r := &CustomSkillResource{client: &testCustomSkillClient{getError: errServer}}
	assertReadKeepsStateAndErrors(t, readWithState(t, r, map[string]string{"origin": "tf_test"}))
}
