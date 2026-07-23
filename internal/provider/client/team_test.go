package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// TestUnmarshalTeam is a basic sanity check that the local JSON→struct helper
// decodes a minimal CRD envelope.
func TestUnmarshalTeam(t *testing.T) {
	jsonStr := `{
	  "kind": "Dash0Team",
	  "metadata": {"name": "backend-team"},
	  "spec": {
	    "display": {"name": "Backend Team", "color": {"from": "#111111", "to": "#222222"}},
	    "members": []
	  }
	}`
	def, err := unmarshalTeam(jsonStr)
	require.NoError(t, err)
	assert.Equal(t, "backend-team", def.Metadata.Name)
	assert.Equal(t, "Backend Team", def.Spec.Display.Name)
}

func TestUnmarshalTeam_Invalid(t *testing.T) {
	_, err := unmarshalTeam("not json")
	assert.Error(t, err)
}

// TestSetTeamOrigin covers the write-side stamp: origin is stamped into
// metadata.labels["dash0.com/origin"] regardless of whether labels was
// previously initialized.
func TestSetTeamOrigin(t *testing.T) {
	def := &dash0.TeamDefinitionV1Alpha1{}
	setTeamOrigin(def, "tf_abc")
	require.NotNil(t, def.Metadata.Labels)
	require.NotNil(t, def.Metadata.Labels.Dash0Comorigin)
	assert.Equal(t, "tf_abc", *def.Metadata.Labels.Dash0Comorigin)

	// Re-stamping preserves other labels.
	other := "user-label"
	def.Metadata.Labels.AdditionalProperties = map[string]string{"team-lead": other}
	setTeamOrigin(def, "tf_xyz")
	assert.Equal(t, "tf_xyz", *def.Metadata.Labels.Dash0Comorigin)
	assert.Equal(t, "user-label", def.Metadata.Labels.AdditionalProperties["team-lead"])
}

// TestGetTeam_NormalizesMembersAndStripsServerFields exercises the read-path
// contract: member internal IDs are resolved to email addresses, and
// server-managed labels/annotations are cleared before the JSON is handed to
// state normalization. dash0.com/origin is preserved (client-settable on
// create, immutable thereafter).
func TestGetTeam_NormalizesMembersAndStripsServerFields(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/teams/"):
			// Return the enriched GetTeamResponse the server actually emits:
			// the CRD envelope under `.team`, plus (empty for this test) arrays
			// of accessible assets.
			team := map[string]interface{}{
				"apiVersion": "dash0.com/v1alpha1",
				"kind":       "Dash0Team",
				"metadata": map[string]interface{}{
					"name": "backend-team",
					"labels": map[string]interface{}{
						"dash0.com/id":     "00000000-0000-0000-0000-000000000001",
						"dash0.com/origin": "tf_backend",
						"dash0.com/source": "terraform",
					},
					"annotations": map[string]interface{}{
						"dash0.com/created-at": "2026-01-15T10:00:00Z",
						"dash0.com/updated-at": "2026-01-15T12:00:00Z",
					},
				},
				"spec": map[string]interface{}{
					"display": map[string]interface{}{
						"name":        "Backend Team",
						"description": "Owns backend services.",
						"color": map[string]interface{}{
							"from": "#6366F1",
							"to":   "#8B5CF6",
						},
					},
					"members": []string{
						"00000000-0000-0000-0000-0000000000A1",
						"00000000-0000-0000-0000-0000000000A2",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"team":            team,
				"members":         []interface{}{},
				"dashboards":      []interface{}{},
				"checkRules":      []interface{}{},
				"syntheticChecks": []interface{}{},
				"views":           []interface{}{},
				"datasets":        []interface{}{},
			})
		case r.URL.Path == "/api/members":
			// The two members expected on the team, plus one unrelated
			// entry to make sure filtering by ID works.
			_ = json.NewEncoder(w).Encode([]dash0.MemberDefinition{
				{
					Kind:     "Dash0Member",
					Metadata: dash0.MemberMetadata{Name: "alice", Labels: &dash0.MemberLabels{Dash0Comid: strPtr("00000000-0000-0000-0000-0000000000A1")}},
					Spec:     dash0.MemberSpec{Display: dash0.MemberDisplay{Email: strPtr("alice@example.com")}},
				},
				{
					Kind:     "Dash0Member",
					Metadata: dash0.MemberMetadata{Name: "bob", Labels: &dash0.MemberLabels{Dash0Comid: strPtr("00000000-0000-0000-0000-0000000000A2")}},
					Spec:     dash0.MemberSpec{Display: dash0.MemberDisplay{Email: strPtr("bob@example.com")}},
				},
				{
					Kind:     "Dash0Member",
					Metadata: dash0.MemberMetadata{Name: "eve", Labels: &dash0.MemberLabels{Dash0Comid: strPtr("00000000-0000-0000-0000-0000000000AF")}},
					Spec:     dash0.MemberSpec{Display: dash0.MemberDisplay{Email: strPtr("eve@example.com")}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)

	c := &dash0Client{inner: inner, apiURL: server.URL}

	result, err := c.GetTeam(t.Context(), "tf_backend")
	require.NoError(t, err)

	// Round-trip the JSON so assertions can look at the resolved shape.
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(result), &got))

	metadata := got["metadata"].(map[string]interface{})
	labels, _ := metadata["labels"].(map[string]interface{})

	// dash0.com/id, dash0.com/source: stripped.
	assert.NotContains(t, labels, "dash0.com/id", "server-managed dash0.com/id should be stripped")
	assert.NotContains(t, labels, "dash0.com/source", "server-managed dash0.com/source should be stripped")

	// dash0.com/origin: preserved (immutable, useful for reconciliation).
	assert.Equal(t, "tf_backend", labels["dash0.com/origin"], "dash0.com/origin should be preserved")

	// created-at / updated-at annotations: stripped.
	if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
		assert.NotContains(t, annotations, "dash0.com/created-at")
		assert.NotContains(t, annotations, "dash0.com/updated-at")
	}

	// Members: internal IDs rewritten to emails, in the same order.
	spec := got["spec"].(map[string]interface{})
	members := spec["members"].([]interface{})
	require.Len(t, members, 2)
	assert.Equal(t, "alice@example.com", members[0])
	assert.Equal(t, "bob@example.com", members[1])
}

// TestGetTeam_MembersEndpointFailureSurfaces asserts that a transient
// /api/members outage is returned as an error rather than silently written
// into state as id-form YAML. Without this, a partial members-endpoint
// failure would poison state.TeamYaml with raw internal IDs and produce
// spurious drift on every subsequent plan until the outage resolves.
func TestGetTeam_MembersEndpointFailureSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/teams/"):
			team := map[string]interface{}{
				"apiVersion": "dash0.com/v1alpha1",
				"kind":       "Dash0Team",
				"metadata":   map[string]interface{}{"name": "backend-team"},
				"spec": map[string]interface{}{
					"display": map[string]interface{}{
						"name":  "Backend Team",
						"color": map[string]interface{}{"from": "#111111", "to": "#222222"},
					},
					"members": []string{"00000000-0000-0000-0000-0000000000A1"},
				},
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"team":            team,
				"members":         []interface{}{},
				"dashboards":      []interface{}{},
				"checkRules":      []interface{}{},
				"syntheticChecks": []interface{}{},
				"views":           []interface{}{},
				"datasets":        []interface{}{},
			})
		case r.URL.Path == "/api/members":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"members endpoint unavailable"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)

	c := &dash0Client{inner: inner, apiURL: server.URL}

	result, err := c.GetTeam(t.Context(), "tf_backend")
	assert.Error(t, err, "members-endpoint failure must surface as an error, not silently return id-form YAML")
	assert.Empty(t, result, "no JSON should be returned when member resolution fails")
	assert.Contains(t, err.Error(), "resolve team members to emails", "error must identify the resolution failure")
}
