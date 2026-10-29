package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const customSkillConfigWithoutPermissions = `
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: incident-response
        description: Triage incidents
        ---
        Follow the runbook.
    - relativePath: references/runbook.md
      content: Step 1
`

const customSkillConfigWithPermissions = `
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: incident-response
        description: Triage incidents
        ---
        Follow the runbook.
  permissions:
    - role: basic_member
      actions: [custom_skill:read]
    - teamId: team-1
      actions: [custom_skill:read, custom_skill:delete]
`

// customSkillAPIResponse is a GET response with every server-managed field set.
const customSkillAPIResponse = `{
  "kind": "Dash0Skill",
  "metadata": {
    "name": "incident-response",
    "labels": {
      "dash0.com/id": "8b3f6a52-6d8d-4a0f-9b40-0e6a3c6c2f10",
      "dash0.com/origin": "tf_abc",
      "dash0.com/version": "3",
      "dash0.com/source": "terraform"
    },
    "annotations": {
      "dash0.com/created-at": "2026-10-01T10:00:00Z",
      "dash0.com/updated-at": "2026-10-02T10:00:00Z",
      "dash0.com/enabled": "true"
    }
  },
  "spec": {
    "files": [
      {"relativePath": "references/runbook.md", "content": "Step 1"},
      {"relativePath": "SKILL.md", "content": "---\nname: incident-response\ndescription: Triage incidents\n---\nFollow the runbook.\n"}
    ],
    "permissions": [
      {"role": "admin", "actions": ["custom_skill:delete", "custom_skill:read"], "defaultEnabled": true},
      {"role": "basic_member", "actions": ["custom_skill:read"], "defaultEnabled": true}
    ],
    "permittedActions": ["custom_skill:read"]
  }
}`

func customSkillEquivalent(t *testing.T, config, actual string) bool {
	t.Helper()
	ignored := FieldsAbsentFromYAML(config, ConditionallyIgnoredFields)
	equivalent, err := CustomSkillYAMLEquivalent(config, actual, ignored)
	require.NoError(t, err)
	return equivalent
}

func TestCustomSkillYAMLEquivalent_IgnoresServerFieldsAndFileOrder(t *testing.T) {
	assert.True(t, customSkillEquivalent(t, customSkillConfigWithoutPermissions, customSkillAPIResponse))
}

func TestCustomSkillYAMLEquivalent_DetectsFileChange(t *testing.T) {
	changed := `
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: incident-response
        description: Triage incidents
        ---
        Follow the runbook.
    - relativePath: references/runbook.md
      content: Step 2
`
	assert.False(t, customSkillEquivalent(t, changed, customSkillAPIResponse))
}

func TestCustomSkillYAMLEquivalent_Permissions(t *testing.T) {
	skillFile := `
    - relativePath: SKILL.md
      content: x
`
	withGrants := func(grants string) string {
		return "kind: Dash0Skill\nspec:\n  files:" + skillFile + "  permissions:\n" + grants
	}

	tests := []struct {
		name       string
		config     string
		actual     string
		equivalent bool
	}{
		{
			name:       "config without permissions ignores the stored grants",
			config:     "kind: Dash0Skill\nspec:\n  files:" + skillFile,
			actual:     withGrants("    - {teamId: t1, actions: [custom_skill:read]}\n"),
			equivalent: true,
		},
		{
			name:   "server-added admin grant and defaultEnabled are ignored",
			config: withGrants("    - {role: basic_member, actions: [custom_skill:read]}\n"),
			actual: withGrants(
				"    - {role: admin, actions: [custom_skill:read, custom_skill:delete], defaultEnabled: true}\n" +
					"    - {role: basic_member, actions: [custom_skill:read], defaultEnabled: true}\n"),
			equivalent: true,
		},
		{
			name:   "server-added admin grant that is off by default is ignored when not declared",
			config: withGrants("    - {role: basic_member, actions: [custom_skill:read]}\n"),
			actual: withGrants(
				"    - {role: admin, actions: [custom_skill:read, custom_skill:delete], defaultEnabled: false}\n" +
					"    - {role: basic_member, actions: [custom_skill:read], defaultEnabled: true}\n"),
			equivalent: true,
		},
		{
			name:   "grant order and action order are ignored",
			config: withGrants("    - {teamId: t1, actions: [custom_skill:read, custom_skill:delete]}\n    - {userId: u1, actions: [custom_skill:read]}\n"),
			actual: withGrants(
				"    - {userId: u1, actions: [custom_skill:read], defaultEnabled: true}\n" +
					"    - {teamId: t1, actions: [custom_skill:delete, custom_skill:read], defaultEnabled: true}\n"),
			equivalent: true,
		},
		{
			name:       "extra grant is drift",
			config:     withGrants("    - {role: basic_member, actions: [custom_skill:read]}\n"),
			actual:     withGrants("    - {role: basic_member, actions: [custom_skill:read]}\n    - {teamId: t1, actions: [custom_skill:read]}\n"),
			equivalent: false,
		},
		{
			name:       "changed actions are drift",
			config:     withGrants("    - {teamId: t1, actions: [custom_skill:read, custom_skill:delete]}\n"),
			actual:     withGrants("    - {teamId: t1, actions: [custom_skill:read]}\n"),
			equivalent: false,
		},
		{
			name:       "declared defaultEnabled is compared",
			config:     withGrants("    - {teamId: t1, actions: [custom_skill:read], defaultEnabled: false}\n"),
			actual:     withGrants("    - {teamId: t1, actions: [custom_skill:read], defaultEnabled: true}\n"),
			equivalent: false,
		},
		{
			name:       "declared admin entry compares only defaultEnabled",
			config:     withGrants("    - {role: admin, actions: [custom_skill:read], defaultEnabled: false}\n"),
			actual:     withGrants("    - {role: admin, actions: [custom_skill:delete, custom_skill:read], defaultEnabled: false}\n"),
			equivalent: true,
		},
		{
			name:       "declared admin entry that is off by default detects a grant turned on",
			config:     withGrants("    - {role: admin, actions: [custom_skill:read], defaultEnabled: false}\n"),
			actual:     withGrants("    - {role: admin, actions: [custom_skill:delete, custom_skill:read], defaultEnabled: true}\n"),
			equivalent: false,
		},
		{
			name:       "declared admin entry that is on by default matches a stripped document without it",
			config:     withGrants("    - {role: admin, actions: [custom_skill:read], defaultEnabled: true}\n"),
			actual:     "kind: Dash0Skill\nspec:\n  files:" + skillFile + "  permissions: []\n",
			equivalent: true,
		},
		{
			name:       "declared admin entry that is off by default does not match a stripped document without it",
			config:     withGrants("    - {role: admin, actions: [custom_skill:read], defaultEnabled: false}\n"),
			actual:     "kind: Dash0Skill\nspec:\n  files:" + skillFile + "  permissions: []\n",
			equivalent: false,
		},
		{
			name:       "empty permissions match only the admin grant",
			config:     withGrants("    []\n"),
			actual:     withGrants("    - {role: admin, actions: [custom_skill:read, custom_skill:delete], defaultEnabled: true}\n"),
			equivalent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.equivalent, customSkillEquivalent(t, tc.config, tc.actual))
		})
	}
}

func TestCustomSkillYAMLEquivalent_FullResponseWithPermissions(t *testing.T) {
	assert.False(t, customSkillEquivalent(t, customSkillConfigWithPermissions, customSkillAPIResponse),
		"the response lacks the team grant the config declares")
}

func TestCustomSkillYAMLEquivalent_InvalidYAML(t *testing.T) {
	_, err := CustomSkillYAMLEquivalent("spec: [", customSkillAPIResponse, nil)
	assert.Error(t, err)
}
