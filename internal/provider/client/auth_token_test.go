package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const (
	authTokenTestID = "11111111-1111-1111-1111-111111111111"

	authTokenTestJSON = `{
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0AuthToken",
  "metadata": {"name": "ci"},
  "spec": {"actions": ["dashboards:read"]}
}`

	// authTokenResponseJSON is what the API returns: id, origin and the secret.
	authTokenResponseJSON = `{
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0AuthToken",
  "metadata": {"name": "ci", "labels": {"dash0.com/id": "` + authTokenTestID + `", "dash0.com/origin": "tf_abc"}},
  "spec": {"actions": ["dashboards:read"], "token": "auth_secret"}
}`
)

// newAuthTokenTestServer records the last request and answers with status and body.
func newAuthTokenTestServer(t *testing.T, status int, body string, gotMethod, gotPath *string, gotBody *map[string]any) *dash0Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotMethod, *gotPath = r.Method, r.URL.Path
		if gotBody != nil {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				require.NoError(t, json.Unmarshal(raw, gotBody))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return newTeamTestClient(t, server)
}

func TestCreateAuthToken_PutsByOriginAndReturnsIDAndToken(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newAuthTokenTestServer(t, http.StatusCreated, authTokenResponseJSON, &method, &path, &body)

	// A client-supplied secret must never reach the server.
	withSecret := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci"},"spec":{"actions":["*"],"token":"auth_mine"}}`
	id, token, err := c.CreateAuthToken(context.Background(), "tf_abc", withSecret)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, method)
	assert.Equal(t, "/api/auth-tokens/tf_abc", path)
	assert.Equal(t, authTokenTestID, id)
	assert.Equal(t, "auth_secret", token)
	labels := body["metadata"].(map[string]any)["labels"].(map[string]any)
	assert.Equal(t, "tf_abc", labels["dash0.com/origin"])
	assert.NotContains(t, body["spec"], "token")
}

func TestCreateAuthToken_ResponseWithoutSecretErrors(t *testing.T) {
	var method, path string
	c := newAuthTokenTestServer(t, http.StatusCreated, authTokenTestJSON, &method, &path, nil)

	_, _, err := c.CreateAuthToken(context.Background(), "tf_abc", authTokenTestJSON)
	require.Error(t, err)
}

func TestGetAuthToken_ReturnsSecretSeparately(t *testing.T) {
	var method, path string
	c := newAuthTokenTestServer(t, http.StatusOK, authTokenResponseJSON, &method, &path, nil)

	out, token, err := c.GetAuthToken(context.Background(), authTokenTestID)
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, method)
	assert.Equal(t, "/api/auth-tokens/"+authTokenTestID, path)
	assert.Equal(t, "auth_secret", token)
	assert.NotContains(t, out, "auth_secret", "the secret must not land in the YAML state")
	assert.Contains(t, out, authTokenTestID)
}

// The API returns the secret only on create; a GET without one is not an error.
func TestGetAuthToken_WithoutSecret(t *testing.T) {
	var method, path string
	body := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci","labels":{"dash0.com/id":"` + authTokenTestID + `"}},"spec":{"actions":["dashboards:read"]}}`
	c := newAuthTokenTestServer(t, http.StatusOK, body, &method, &path, nil)

	out, token, err := c.GetAuthToken(context.Background(), authTokenTestID)
	require.NoError(t, err)
	assert.Empty(t, token)
	assert.Contains(t, out, authTokenTestID)
}

func TestUpdateAuthToken_PutsByIDWithoutSecret(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newAuthTokenTestServer(t, http.StatusOK, authTokenResponseJSON, &method, &path, &body)

	require.NoError(t, c.UpdateAuthToken(context.Background(), authTokenTestID, "tf_abc",
		`{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci"},"spec":{"actions":["*"],"token":"auth_mine"}}`))

	assert.Equal(t, http.MethodPut, method)
	assert.Equal(t, "/api/auth-tokens/"+authTokenTestID, path)
	labels := body["metadata"].(map[string]any)["labels"].(map[string]any)
	assert.Equal(t, "tf_abc", labels["dash0.com/origin"])
	assert.NotContains(t, body["spec"], "token")
}

func TestDeleteAuthToken(t *testing.T) {
	var method, path string
	c := newAuthTokenTestServer(t, http.StatusOK, `{}`, &method, &path, nil)

	require.NoError(t, c.DeleteAuthToken(context.Background(), authTokenTestID))

	assert.Equal(t, http.MethodDelete, method)
	assert.Equal(t, "/api/auth-tokens/"+authTokenTestID, path)
}

func TestAuthToken_NotFoundMapsToIsNotFound(t *testing.T) {
	var method, path string
	c := newAuthTokenTestServer(t, http.StatusNotFound, `{"error":{"code":404,"message":"not found"}}`, &method, &path, nil)

	_, _, err := c.GetAuthToken(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, dash0.IsNotFound(err))
}
