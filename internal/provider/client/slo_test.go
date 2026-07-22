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

func TestUnmarshalSLO(t *testing.T) {
	jsonStr := `{"apiVersion":"openslo/v1","kind":"SLO","metadata":{"name":"test"},"spec":{"service":"checkout"}}`
	def, err := unmarshalSLO(jsonStr)
	require.NoError(t, err)
	assert.NotNil(t, def)
}

func TestUnmarshalSLO_Invalid(t *testing.T) {
	_, err := unmarshalSLO("not json")
	assert.Error(t, err)
}

// TestResolveSLO verifies that ResolveSLO resolves the id by matching on origin
// (read from metadata.labels."dash0.com/origin") and returns the library-built
// deep link including the dataset.
func TestResolveSLO(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]dash0.SloDefinition{
			{
				ApiVersion: "openslo/v1",
				Kind:       "SLO",
				Metadata: dash0.SloMetadata{
					Name: "other",
					Labels: &dash0.SloLabels{
						Dash0Comid:     strPtr("11111111-1111-1111-1111-111111111111"),
						Dash0Comorigin: strPtr("tf_other"),
					},
				},
			},
			{
				ApiVersion: "openslo/v1",
				Kind:       "SLO",
				Metadata: dash0.SloMetadata{
					Name: "target",
					Labels: &dash0.SloLabels{
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

	t.Run("match by origin returns id and library deep link with dataset", func(t *testing.T) {
		id, url, err := c.ResolveSLO(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "33333333-3333-3333-3333-333333333333", id)
		assert.Equal(t, "https://app.dash0.com/goto/alerting/slos/details?dataset=production&slo_id=33333333-3333-3333-3333-333333333333", url)
	})

	t.Run("no match returns empty strings and no error", func(t *testing.T) {
		id, url, err := c.ResolveSLO(t.Context(), "tf_missing", "production")
		require.NoError(t, err)
		assert.Equal(t, "", id)
		assert.Equal(t, "", url)
	})
}
