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

func TestNewDash0Client_CheckRule(t *testing.T) {
	// Verify client creation works (check rule methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), false, "test", 3)
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

// singleRuleWithTopLevelAnnotation is adapted from
// fixtures/check-rule-annotation-parity/multi-rule-with-top-level-annotations.yaml
// (see plans/005-check-rule-annotation-parity.md) down to a single rule, since
// Terraform's UnmarshalPrometheusRule path only supports one rule per resource.
// The rule declares no annotations of its own, so it must inherit the
// top-level dash0.com/notification-channel-ids annotation in full once
// dash0yaml.UnmarshalPrometheusRule merges metadata.annotations into the
// rule's own annotations.
const singleRuleWithTopLevelAnnotation = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: checkout-check-rules
  namespace: monitoring
  annotations:
    dash0.com/notification-channel-ids: "3fa42d0c-6b8e-4c1a-9f2d-111111111111,3fa42d0c-6b8e-4c1a-9f2d-222222222222"
spec:
  groups:
    - name: Alerting
      interval: 1m
      rules:
        - alert: CheckoutHighLatency
          expr: up{job="checkout"} == 0
          labels:
            team: checkout
`

// TestCreateCheckRule_MergesTopLevelAnnotations proves that the
// dash0-api-client-go fix (mergeAnnotations, called from
// dash0yaml.UnmarshalPrometheusRule) flows through the provider's client
// wrapper: a top-level metadata.annotations entry not repeated on the single
// rule must end up in the JSON body sent to the API.
func TestCreateCheckRule_MergesTopLevelAnnotations(t *testing.T) {
	var capturedBody dash0.PrometheusAlertRule
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &capturedBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(dash0.PrometheusAlertRule{
			Name:       "CheckoutHighLatency",
			Expression: `up{job="checkout"} == 0`,
		})
	}))
	t.Cleanup(server.Close)

	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)

	c := &dash0Client{inner: inner, apiURL: server.URL}

	err = c.CreateCheckRule(context.Background(), "tf_test-origin", singleRuleWithTopLevelAnnotation, "test-dataset")
	require.NoError(t, err)

	require.NotNil(t, capturedBody.Annotations)
	require.NotNil(t, capturedBody.Annotations.AdditionalProperties)
	assert.Equal(t,
		"3fa42d0c-6b8e-4c1a-9f2d-111111111111,3fa42d0c-6b8e-4c1a-9f2d-222222222222",
		capturedBody.Annotations.AdditionalProperties["dash0.com/notification-channel-ids"],
		"the rule had no annotations of its own, so it must inherit the top-level metadata.annotations entry in full")
}
