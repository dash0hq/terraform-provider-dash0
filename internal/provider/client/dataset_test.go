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

const datasetTestJSON = `{
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0Dataset",
  "metadata": {"name": "Staging", "labels": {"dash0.com/id": "staging"}},
  "spec": {
    "preferred": false,
    "retention": {"traces": "30d", "logs": "60d"},
    "privacy": {
      "ipAddresses": {"endUser": {"storage": "anonymize"}},
      "geoLocation": {"endUser": {"storage": "up_to_country"}},
      "redaction": {"cardholderData": {"enabled": true}, "genAi": {"conversation": "redact"}}
    },
    "processing": {"spanEventsToLogs": true}
  }
}`

// assertDatasetSpecShape checks the document shape survives the typed decode
// and encode in the client wrapper: no settings may be dropped.
func assertDatasetSpecShape(t *testing.T, doc map[string]any) {
	t.Helper()
	assert.Equal(t, "dash0.com/v1alpha1", doc["apiVersion"])
	spec, ok := doc["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"traces": "30d", "logs": "60d"}, spec["retention"])
	privacy, ok := spec["privacy"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"endUser": map[string]any{"storage": "anonymize"}}, privacy["ipAddresses"])
	assert.Equal(t, map[string]any{"endUser": map[string]any{"storage": "up_to_country"}}, privacy["geoLocation"])
	assert.Equal(t, map[string]any{
		"cardholderData": map[string]any{"enabled": true},
		"genAi":          map[string]any{"conversation": "redact"},
	}, privacy["redaction"])
	assert.Equal(t, map[string]any{"spanEventsToLogs": true}, spec["processing"])
}

// newDatasetTestServer starts an httptest server that records the last
// request and answers with status and body.
func newDatasetTestServer(t *testing.T, status int, body string, gotMethod, gotPath *string, gotBody *map[string]any) *dash0Client {
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

func datasetLabels(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	md, ok := body["metadata"].(map[string]any)
	require.True(t, ok)
	labels, ok := md["labels"].(map[string]any)
	require.True(t, ok)
	return labels
}

func TestCreateDataset_StampsOriginAndPostsSlug(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newDatasetTestServer(t, http.StatusOK, datasetTestJSON, &method, &path, &body)

	require.NoError(t, c.CreateDataset(context.Background(), "tf_abc", datasetTestJSON))

	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/api/datasets", path)
	labels := datasetLabels(t, body)
	assert.Equal(t, "staging", labels["dash0.com/id"])
	assert.Equal(t, "tf_abc", labels["dash0.com/origin"])
	assertDatasetSpecShape(t, body)
}

func TestCreateDataset_EmptyOriginLeavesOriginUnset(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newDatasetTestServer(t, http.StatusOK, datasetTestJSON, &method, &path, &body)

	require.NoError(t, c.CreateDataset(context.Background(), "", datasetTestJSON))

	assert.NotContains(t, datasetLabels(t, body), "dash0.com/origin")
}

func TestCreateDataset_MissingSlugErrors(t *testing.T) {
	var method, path string
	c := newDatasetTestServer(t, http.StatusOK, datasetTestJSON, &method, &path, nil)

	err := c.CreateDataset(context.Background(), "tf_abc",
		`{"kind":"Dash0Dataset","metadata":{"name":"Staging"},"spec":{}}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dash0.com/id")
	assert.Empty(t, method, "no request must be sent without a slug")
}

func TestGetDataset_StripsServerFieldsAndKeepsIdentity(t *testing.T) {
	var method, path string
	c := newDatasetTestServer(t, http.StatusOK, `{
	  "apiVersion": "dash0.com/v1alpha1",
	  "kind": "Dash0Dataset",
	  "metadata": {
	    "name": "Staging",
	    "labels": {"dash0.com/id": "staging", "dash0.com/origin": "tf_abc", "dash0.com/version": "7", "dash0.com/source": "terraform"},
	    "annotations": {"dash0.com/created-at": "2026-01-01T00:00:00Z"}
	  },
	  "spec": {
	    "preferred": true,
	    "permittedActions": ["dataset:read"],
	    "permissions": [{"role": "basic_member", "actions": ["dataset:read"]}],
	    "retention": {"traces": "30d", "logs": "60d"},
	    "privacy": {
	      "ipAddresses": {"endUser": {"storage": "anonymize"}},
	      "geoLocation": {"endUser": {"storage": "up_to_country"}},
	      "redaction": {"cardholderData": {"enabled": true}, "genAi": {"conversation": "redact"}}
	    },
	    "processing": {"spanEventsToLogs": true}
	  }
	}`, &method, &path, nil)

	out, err := c.GetDataset(context.Background(), "staging")
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, method)
	assert.Equal(t, "/api/datasets/staging", path)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	labels := datasetLabels(t, got)
	assert.Equal(t, "staging", labels["dash0.com/id"])
	assert.Equal(t, "tf_abc", labels["dash0.com/origin"])
	assert.NotContains(t, labels, "dash0.com/version")
	assert.NotContains(t, labels, "dash0.com/source")
	assert.NotContains(t, got["metadata"], "annotations")
	spec := got["spec"].(map[string]any)
	assert.Equal(t, true, spec["preferred"])
	for _, f := range []string{"permittedActions", "permissions"} {
		assert.NotContains(t, spec, f)
	}
	assertDatasetSpecShape(t, got)
}

func TestUpdateDataset_PutsBySlugAndMatchesBodySlug(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newDatasetTestServer(t, http.StatusOK, datasetTestJSON, &method, &path, &body)

	// Body carries a different slug; the path slug must win.
	err := c.UpdateDataset(context.Background(), "other", "tf_abc", datasetTestJSON)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, method)
	assert.Equal(t, "/api/datasets/other", path)
	labels := datasetLabels(t, body)
	assert.Equal(t, "other", labels["dash0.com/id"])
	assert.Equal(t, "tf_abc", labels["dash0.com/origin"])
	assertDatasetSpecShape(t, body)
}

func TestDeleteDataset(t *testing.T) {
	var method, path string
	c := newDatasetTestServer(t, http.StatusOK, `{}`, &method, &path, nil)

	require.NoError(t, c.DeleteDataset(context.Background(), "staging"))

	assert.Equal(t, http.MethodDelete, method)
	assert.Equal(t, "/api/datasets/staging", path)
}

func TestDataset_NotFoundMapsToIsNotFound(t *testing.T) {
	var method, path string
	c := newDatasetTestServer(t, http.StatusNotFound, `{"error":{"code":404,"message":"not found"}}`, &method, &path, nil)

	_, err := c.GetDataset(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, dash0.IsNotFound(err))
}
