package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const customSkillResponseJSON = `{
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
      "dash0.com/enabled": "true"
    }
  },
  "spec": {
    "files": [{"relativePath": "SKILL.md", "content": "---\nname: incident-response\n---\n"}],
    "permissions": [
      {"role": "admin", "actions": ["custom_skill:delete", "custom_skill:read"], "defaultEnabled": true},
      {"role": "basic_member", "actions": ["custom_skill:read"], "defaultEnabled": true}
    ],
    "permittedActions": ["custom_skill:read"]
  }
}`

// newCustomSkillTestClient returns a client backed by a server that answers
// every request with response and records the last request.
func newCustomSkillTestClient(t *testing.T, response string) (*dash0Client, *http.Request, *[]byte) {
	t.Helper()
	var lastRequest http.Request
	var lastBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRequest = *r
		lastBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)

	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)
	return &dash0Client{inner: inner, apiURL: server.URL}, &lastRequest, &lastBody
}

func TestCreateCustomSkill_PutsByOriginAndReturnsID(t *testing.T) {
	c, req, body := newCustomSkillTestClient(t, customSkillResponseJSON)

	id, err := c.CreateCustomSkill(t.Context(), "tf_abc",
		`{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/id":"client-supplied"}},"spec":{"files":[{"relativePath":"SKILL.md","content":"x"}]}}`)
	require.NoError(t, err)

	assert.Equal(t, "8b3f6a52-6d8d-4a0f-9b40-0e6a3c6c2f10", id)
	assert.Equal(t, http.MethodPut, req.Method)
	assert.Equal(t, "/api/agent0/custom-skills/tf_abc", req.URL.Path)

	var sent dash0.SkillDefinition
	require.NoError(t, json.Unmarshal(*body, &sent))
	assert.Equal(t, "tf_abc", dash0.GetCustomSkillOrigin(&sent))
	assert.Empty(t, dash0.GetCustomSkillID(&sent), "the API rejects a client-supplied id on create")
}

func TestUpdateCustomSkill_DropsVersion(t *testing.T) {
	c, req, body := newCustomSkillTestClient(t, customSkillResponseJSON)

	err := c.UpdateCustomSkill(t.Context(), "tf_abc",
		`{"kind":"Dash0Skill","metadata":{"labels":{"dash0.com/version":"2"}},"spec":{"files":[{"relativePath":"SKILL.md","content":"x"}]}}`)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, req.Method)
	assert.Equal(t, "/api/agent0/custom-skills/tf_abc", req.URL.Path)

	var sent dash0.SkillDefinition
	require.NoError(t, json.Unmarshal(*body, &sent))
	require.NotNil(t, sent.Metadata.Labels)
	assert.Nil(t, sent.Metadata.Labels.Dash0Comversion)
}

func TestGetCustomSkill_StripsServerFields(t *testing.T) {
	c, req, _ := newCustomSkillTestClient(t, customSkillResponseJSON)

	got, err := c.GetCustomSkill(t.Context(), "tf_abc")
	require.NoError(t, err)
	assert.Equal(t, "/api/agent0/custom-skills/tf_abc", req.URL.Path)

	var def dash0.SkillDefinition
	require.NoError(t, json.Unmarshal([]byte(got), &def))
	assert.Nil(t, def.Metadata.Name)
	assert.Nil(t, def.Metadata.Annotations)
	assert.Nil(t, def.Spec.PermittedActions)
	require.NotNil(t, def.Metadata.Labels)
	assert.Nil(t, def.Metadata.Labels.Dash0Comversion)
	require.NotNil(t, def.Spec.Permissions)
	require.Len(t, *def.Spec.Permissions, 1, "the default admin grant is dropped")
	assert.Equal(t, "basic_member", *(*def.Spec.Permissions)[0].Role)

	id, origin := CustomSkillIdentity(got)
	assert.Equal(t, "8b3f6a52-6d8d-4a0f-9b40-0e6a3c6c2f10", id)
	assert.Equal(t, "tf_abc", origin)
}

func TestGetCustomSkill_KeepsDeletedAt(t *testing.T) {
	c, _, _ := newCustomSkillTestClient(t,
		`{"kind":"Dash0Skill","metadata":{"annotations":{"dash0.com/deleted-at":"2026-10-03T10:00:00Z","dash0.com/created-at":"2026-10-01T10:00:00Z"}},"spec":{"files":[]}}`)

	got, err := c.GetCustomSkill(t.Context(), "tf_abc")
	require.NoError(t, err)

	var def dash0.SkillDefinition
	require.NoError(t, json.Unmarshal([]byte(got), &def))
	require.NotNil(t, def.Metadata.Annotations)
	assert.NotNil(t, def.Metadata.Annotations.Dash0ComdeletedAt)
	assert.Nil(t, def.Metadata.Annotations.Dash0ComcreatedAt)
}

func TestCustomSkillIdentity_InvalidJSON(t *testing.T) {
	id, origin := CustomSkillIdentity("not json")
	assert.Empty(t, id)
	assert.Empty(t, origin)
}
