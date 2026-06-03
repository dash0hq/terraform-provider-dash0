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

func TestUnmarshalSyntheticCheck(t *testing.T) {
	jsonStr := `{"kind":"SyntheticCheck","metadata":{"name":"test"},"spec":{"plugin":{"kind":"http"}}}`
	def, err := unmarshalSyntheticCheck(jsonStr)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalSyntheticCheck_Invalid(t *testing.T) {
	_, err := unmarshalSyntheticCheck("not json")
	assert.Error(t, err)
}

// TestGetSyntheticCheckURL verifies that GetSyntheticCheckURL resolves the id by
// matching on origin and returns the library-built deep link including the
// dataset.
func TestGetSyntheticCheckURL(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]dash0.SyntheticChecksApiListItem{
			{Id: "11111111-1111-1111-1111-111111111111", Origin: strPtr("tf_other")},
			{Id: "33333333-3333-3333-3333-333333333333", Origin: strPtr("tf_target")},
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

	t.Run("match by origin returns the library deep link with dataset", func(t *testing.T) {
		got, err := c.GetSyntheticCheckURL(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "https://app.dash0.com/goto/alerting/synthetics?check_id=33333333-3333-3333-3333-333333333333&dataset=production", got)
	})

	t.Run("no match returns empty string and no error", func(t *testing.T) {
		got, err := c.GetSyntheticCheckURL(t.Context(), "tf_missing", "production")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})
}
