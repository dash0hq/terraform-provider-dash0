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

func TestUnmarshalDashboard(t *testing.T) {
	json := `{"kind":"Dashboard","metadata":{"name":"test"},"spec":{"title":"Test"}}`
	def, err := unmarshalDashboard(json)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalDashboard_Invalid(t *testing.T) {
	_, err := unmarshalDashboard("not json")
	assert.Error(t, err)
}

func TestMatchOriginID(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	accessor := func(item *dash0.DashboardApiListItem) (string, *string) {
		return item.Id, item.Origin
	}
	items := []*dash0.DashboardApiListItem{
		nil, // tolerate nil entries
		{Id: "11111111-1111-1111-1111-111111111111", Origin: strPtr("tf_abc")},
		{Id: "22222222-2222-2222-2222-222222222222", Origin: nil}, // UI-created, no origin
		{Id: "33333333-3333-3333-3333-333333333333", Origin: strPtr("tf_target")},
	}

	t.Run("match by origin returns the internal id", func(t *testing.T) {
		assert.Equal(t, "33333333-3333-3333-3333-333333333333", matchOriginID(items, "tf_target", accessor))
	})

	t.Run("no match returns empty", func(t *testing.T) {
		assert.Equal(t, "", matchOriginID(items, "tf_missing", accessor))
	})
}

// TestGetDashboardURL verifies that GetDashboardURL resolves the internal id by
// matching on origin and returns the library-built deep link. The host
// derivation and URL format themselves are the responsibility of the
// dash0-api-client-go library and are covered by its own tests.
func TestGetDashboardURL(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]dash0.DashboardApiListItem{
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

	// inner points at the test server; apiURL drives the deep link host.
	c := &dash0Client{inner: inner, apiURL: "https://api.us-west-2.aws.dash0.com"}

	t.Run("match by origin returns the library deep link", func(t *testing.T) {
		got, err := c.GetDashboardURL(t.Context(), "tf_target", "default")
		require.NoError(t, err)
		assert.Equal(t, "https://app.dash0.com/goto/dashboards?dashboard_id=33333333-3333-3333-3333-333333333333", got)
	})

	t.Run("no match returns empty string and no error", func(t *testing.T) {
		got, err := c.GetDashboardURL(t.Context(), "tf_missing", "default")
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})
}
