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

func TestUnmarshalView(t *testing.T) {
	jsonStr := `{"kind":"View","metadata":{"name":"test"},"spec":{"display":{"name":"Test"}}}`
	def, err := unmarshalView(jsonStr)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalView_Invalid(t *testing.T) {
	_, err := unmarshalView("not json")
	assert.Error(t, err)
}

// TestGetViewURL verifies that GetViewURL resolves the id by matching on origin,
// selects the right page based on the view's type, and includes the dataset.
func TestGetViewURL(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]dash0.ViewApiListItem{
			{Id: "11111111-1111-1111-1111-111111111111", Origin: strPtr("tf_other"), Type: dash0.Logs},
			{Id: "33333333-3333-3333-3333-333333333333", Origin: strPtr("tf_target"), Type: dash0.Spans},
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

	t.Run("span view routes to traces explorer with dataset", func(t *testing.T) {
		got, err := c.GetViewURL(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "https://app.dash0.com/goto/traces/explorer?dataset=production&view_id=33333333-3333-3333-3333-333333333333", got)
	})

	t.Run("no match returns empty string and no error", func(t *testing.T) {
		got, err := c.GetViewURL(t.Context(), "tf_missing", "production")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})
}
