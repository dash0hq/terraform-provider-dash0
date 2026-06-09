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

func TestUnmarshalNotificationChannel(t *testing.T) {
	jsonStr := `{"kind":"Dash0NotificationChannel","metadata":{"name":"test"},"spec":{"type":"email_v2","config":{"recipients":["a@example.com"]}}}`
	def, err := unmarshalNotificationChannel(jsonStr)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalNotificationChannel_Invalid(t *testing.T) {
	_, err := unmarshalNotificationChannel("not json")
	assert.Error(t, err)
}

// TestResolveNotificationChannel verifies that ResolveNotificationChannel
// finds the server-assigned id by matching on origin and returns both the id
// and the library-built deep link. Notification channels are org-level, so
// the URL has no dataset query parameter.
func TestResolveNotificationChannel(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]dash0.NotificationChannelDefinition{
			{
				Kind: "Dash0NotificationChannel",
				Metadata: dash0.NotificationChannelMetadata{
					Name: "Other",
					Labels: &dash0.NotificationChannelLabels{
						Dash0Comid:     strPtr("11111111-1111-1111-1111-111111111111"),
						Dash0Comorigin: strPtr("tf_other"),
					},
				},
			},
			{
				Kind: "Dash0NotificationChannel",
				Metadata: dash0.NotificationChannelMetadata{
					Name: "Target",
					Labels: &dash0.NotificationChannelLabels{
						Dash0Comid:     strPtr("33333333-3333-3333-3333-333333333333"),
						Dash0Comorigin: strPtr("tf_target"),
					},
				},
			},
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

	t.Run("match by origin returns id and library deep link without dataset", func(t *testing.T) {
		id, url, err := c.ResolveNotificationChannel(t.Context(), "tf_target")
		require.NoError(t, err)
		assert.Equal(t, "33333333-3333-3333-3333-333333333333", id)
		assert.Equal(t, "https://app.dash0.com/goto/settings/notifications?channel_id=33333333-3333-3333-3333-333333333333", url)
	})

	t.Run("no match returns empty id and url and no error", func(t *testing.T) {
		id, url, err := c.ResolveNotificationChannel(t.Context(), "tf_missing")
		require.NoError(t, err)
		assert.Equal(t, "", id)
		assert.Equal(t, "", url)
	})
}
