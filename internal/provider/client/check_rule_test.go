package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func TestNewDash0Client_CheckRule(t *testing.T) {
	// Verify client creation works (check rule methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", "auth_test-token", "test", 3)
	require.NoError(t, err)
	assert.NotNil(t, c)
}

// TestResolveCheckRule verifies that ResolveCheckRule resolves the id by matching
// on origin and returns it along with the library-built deep link including the
// dataset.
func TestResolveCheckRule(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]dash0.PrometheusAlertRuleApiListItem{
			{Id: "tf_other", Origin: strPtr("tf_other")},
			{Id: "tf_target", Origin: strPtr("tf_target")},
		})
	}))
	t.Cleanup(server.Close)

	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)

	c := &dash0Client{inner: inner, apiURL: "https://api.us-west-2.aws.dash0.com"}

	t.Run("match by origin returns id and library deep link with dataset", func(t *testing.T) {
		id, url, err := c.ResolveCheckRule(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "tf_target", id)
		assert.Equal(t, "https://app.dash0.com/goto/alerting/check-rules?check_rule_id=tf_target&dataset=production", url)
	})

	t.Run("no match returns empty strings and no error", func(t *testing.T) {
		id, url, err := c.ResolveCheckRule(t.Context(), "tf_missing", "production")
		require.NoError(t, err)
		assert.Equal(t, "", id)
		assert.Equal(t, "", url)
	})
}
