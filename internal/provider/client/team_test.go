package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// newTeamTestClient wires a dash0Client to the provided httptest server and
// returns both. Test authors must close the server via t.Cleanup themselves;
// this helper is intentionally minimal so each test controls the handler.
func newTeamTestClient(t *testing.T, server *httptest.Server) *dash0Client {
	t.Helper()
	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)
	return &dash0Client{inner: inner, apiURL: server.URL}
}

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

// TestCreateTeam_StampsOriginAndUsesPUT asserts the write-path contract:
// - HTTP method is PUT (create-or-replace, per the origin pattern)
// - URL path is /api/teams/<origin>
// - Body carries metadata.labels["dash0.com/origin"] = <origin>
// A regression that stopped calling setTeamOrigin — or that swapped PUT for
// POST — would fail one of these assertions.
func TestCreateTeam_StampsOriginAndUsesPUT(t *testing.T) {
	teamJSON := `{
	  "kind": "Dash0Team",
	  "metadata": {"name": "backend-team"},
	  "spec": {
	    "display": {"name": "Backend Team", "color": {"from": "#111", "to": "#222"}},
	    "members": []
	  }
	}`

	var seenMethod, seenPath string
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seenBody)
		w.Header().Set("Content-Type", "application/json")
		// Echo the request body back — enough to satisfy UpsertTeam's response
		// contract (it wants a valid TeamDefinitionV1Alpha1).
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	c := newTeamTestClient(t, server)
	require.NoError(t, c.CreateTeam(t.Context(), "tf_backend", teamJSON))

	assert.Equal(t, http.MethodPut, seenMethod, "CreateTeam must use PUT (create-or-replace via UpsertTeam)")
	assert.Equal(t, "/api/teams/tf_backend", seenPath, "origin must be the last path segment")

	metadata, _ := seenBody["metadata"].(map[string]interface{})
	require.NotNil(t, metadata)
	labels, _ := metadata["labels"].(map[string]interface{})
	require.NotNil(t, labels, "outbound body must carry metadata.labels")
	assert.Equal(t, "tf_backend", labels["dash0.com/origin"],
		"dash0.com/origin must be stamped into the body so the server records the caller's origin")
}

// TestUpdateTeam_StampsOriginAndUsesPUT covers the same contract as
// CreateTeam — Update also goes through UpsertTeam and must stamp the origin.
func TestUpdateTeam_StampsOriginAndUsesPUT(t *testing.T) {
	teamJSON := `{
	  "kind": "Dash0Team",
	  "metadata": {"name": "backend-team"},
	  "spec": {
	    "display": {"name": "Renamed", "color": {"from": "#111", "to": "#222"}},
	    "members": []
	  }
	}`

	var seenMethod string
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	c := newTeamTestClient(t, server)
	require.NoError(t, c.UpdateTeam(t.Context(), "tf_backend", teamJSON))

	assert.Equal(t, http.MethodPut, seenMethod)
	metadata, _ := seenBody["metadata"].(map[string]interface{})
	labels, _ := metadata["labels"].(map[string]interface{})
	require.NotNil(t, labels)
	assert.Equal(t, "tf_backend", labels["dash0.com/origin"])
}

// TestDeleteTeam_UsesDELETE asserts the DELETE-by-origin contract.
func TestDeleteTeam_UsesDELETE(t *testing.T) {
	var seenMethod, seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		// The API returns 204 No Content on successful delete (matches
		// DeleteTeam's accepted-status set in the library).
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	c := newTeamTestClient(t, server)
	require.NoError(t, c.DeleteTeam(t.Context(), "tf_backend"))

	assert.Equal(t, http.MethodDelete, seenMethod)
	assert.Equal(t, "/api/teams/tf_backend", seenPath)
}

// TestResolveTeam covers the id-extraction contract:
//   - team present with dash0.com/id label → returns that value
//   - team present but id label missing → returns "" and no error (indistinguishable
//     from "team fresh, id not yet propagated"; caller surfaces null-id downstream)
//   - transport error → surfaces
//
// ResolveTeam intentionally does not return a URL (unlike Resolve* on other
// resources) — the Dash0 web app has no per-team deep-link page today.
func TestResolveTeam(t *testing.T) {
	respWithID := map[string]interface{}{
		"team": map[string]interface{}{
			"apiVersion": "dash0.com/v1alpha1",
			"kind":       "Dash0Team",
			"metadata": map[string]interface{}{
				"name": "backend-team",
				"labels": map[string]interface{}{
					"dash0.com/id":     "00000000-0000-0000-0000-000000000001",
					"dash0.com/origin": "tf_backend",
				},
			},
			"spec": map[string]interface{}{
				"display": map[string]interface{}{
					"name":  "Backend Team",
					"color": map[string]interface{}{"from": "#111", "to": "#222"},
				},
				"members": []interface{}{},
			},
		},
		"members":         []interface{}{},
		"dashboards":      []interface{}{},
		"checkRules":      []interface{}{},
		"syntheticChecks": []interface{}{},
		"views":           []interface{}{},
		"datasets":        []interface{}{},
	}
	respNoID := map[string]interface{}{
		"team": map[string]interface{}{
			"apiVersion": "dash0.com/v1alpha1",
			"kind":       "Dash0Team",
			"metadata": map[string]interface{}{
				"name":   "backend-team",
				"labels": map[string]interface{}{"dash0.com/origin": "tf_backend"},
			},
			"spec": map[string]interface{}{
				"display": map[string]interface{}{
					"name":  "Backend Team",
					"color": map[string]interface{}{"from": "#111", "to": "#222"},
				},
				"members": []interface{}{},
			},
		},
		"members":         []interface{}{},
		"dashboards":      []interface{}{},
		"checkRules":      []interface{}{},
		"syntheticChecks": []interface{}{},
		"views":           []interface{}{},
		"datasets":        []interface{}{},
	}

	cases := []struct {
		name       string
		body       map[string]interface{}
		status     int
		expectID   string
		expectErr  bool
		errMessage string
	}{
		{
			name:     "team with dash0.com/id returns the id",
			body:     respWithID,
			status:   http.StatusOK,
			expectID: "00000000-0000-0000-0000-000000000001",
		},
		{
			name:     "team without id label returns empty and no error",
			body:     respNoID,
			status:   http.StatusOK,
			expectID: "",
		},
		{
			name:      "server error surfaces",
			body:      map[string]interface{}{"error": map[string]interface{}{"message": "boom"}},
			status:    http.StatusInternalServerError,
			expectErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			t.Cleanup(server.Close)

			c := newTeamTestClient(t, server)
			id, err := c.ResolveTeam(t.Context(), "tf_backend")

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectID, id)
			}
		})
	}
}
