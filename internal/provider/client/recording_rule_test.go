package client

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func TestNewDash0Client_RecordingRule(t *testing.T) {
	// Verify client creation works (recording rule methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), false, "test", 3, "")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

// A soft-deleted rule comes back as 200 with dash0.com/deleted-at. The wrapper
// strips other server fields but must keep this one so Read can drop the rule.
func TestGetRecordingRule_KeepsDeletedAtAnnotation(t *testing.T) {
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind": "Dash0RecordingRule",
			"metadata": {
				"name": "r",
				"annotations": {
					"dash0.com/deleted-at": "2026-01-01T00:00:00Z",
					"dash0.com/updated-at": "2026-01-01T00:00:00Z"
				}
			},
			"spec": {}
		}`))
	})

	got, err := c.GetRecordingRule(t.Context(), "tf_abc", "production")
	require.NoError(t, err)
	assert.Contains(t, got, `"dash0.com/deleted-at":"2026-01-01T00:00:00Z"`)
	assert.NotContains(t, got, "dash0.com/updated-at")
}
