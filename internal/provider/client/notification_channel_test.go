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

// TestGetNotificationChannelMetadata verifies that GetNotificationChannelMetadata
// resolves the server-assigned id (the bare dash0.com/id UUID) and the web app
// deep link for a channel by origin in a single request, returns empty values
// (no error) when the channel carries no id label, and propagates API errors.
// Notification channels are org-level, so the URL has no dataset query parameter.
func TestGetNotificationChannelMetadata(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	newClient := func(t *testing.T, serverURL string) *dash0Client {
		inner, err := dash0.NewClient(
			dash0.WithApiUrl(serverURL),
			dash0.WithAuthToken("auth_test-token"),
			dash0.WithUserAgent("test"),
		)
		require.NoError(t, err)
		return &dash0Client{inner: inner, apiURL: "https://api.us-west-2.aws.dash0.com"}
	}

	t.Run("returns the server-assigned id and deep link without dataset", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(dash0.NotificationChannelDefinition{
				Kind: "Dash0NotificationChannel",
				Metadata: dash0.NotificationChannelMetadata{
					Name: "Target",
					Labels: &dash0.NotificationChannelLabels{
						Dash0Comid:     strPtr("33333333-3333-3333-3333-333333333333"),
						Dash0Comorigin: strPtr("tf_target"),
					},
				},
			})
		}))
		t.Cleanup(server.Close)

		c := newClient(t, server.URL)
		id, url, err := c.GetNotificationChannelMetadata(t.Context(), "tf_target")
		require.NoError(t, err)
		assert.Equal(t, "33333333-3333-3333-3333-333333333333", id)
		assert.Equal(t, "https://app.dash0.com/goto/settings/notifications?channel_id=33333333-3333-3333-3333-333333333333", url)
	})

	t.Run("no id label returns empty values and no error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(dash0.NotificationChannelDefinition{
				Kind:     "Dash0NotificationChannel",
				Metadata: dash0.NotificationChannelMetadata{Name: "No ID"},
			})
		}))
		t.Cleanup(server.Close)

		c := newClient(t, server.URL)
		id, url, err := c.GetNotificationChannelMetadata(t.Context(), "tf_target")
		require.NoError(t, err)
		assert.Equal(t, "", id)
		assert.Equal(t, "", url)
	})

	t.Run("propagates API errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		c := newClient(t, server.URL)
		id, url, err := c.GetNotificationChannelMetadata(t.Context(), "tf_target")
		require.Error(t, err)
		assert.Equal(t, "", id)
		assert.Equal(t, "", url)
	})
}
