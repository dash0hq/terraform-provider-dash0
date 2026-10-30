package client

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const minimalSignalToMetricsJSON = `{
	"kind": "Dash0SignalToMetrics",
	"metadata": {"name": "checkout-errors"},
	"spec": {
		"enabled": true,
		"display": {"name": "Checkout errors"},
		"match": {
			"signal": "spans",
			"filters": [{"key": "service.name", "operator": "is", "value": "checkout"}]
		},
		"output": {"name": "checkout_errors", "interval": "60s"}
	}
}`

// echoSignalToMetricsHandler responds 200 with a minimal valid rule, which is
// what the API client needs in order to decode a successful PUT or GET.
func echoSignalToMetricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(minimalSignalToMetricsJSON))
}

func TestCreateSignalToMetrics_IssuesPutWithDatasetQueryParam(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSignalToMetricsHandler)

	err := c.CreateSignalToMetrics(t.Context(), "tf_abc", minimalSignalToMetricsJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/api/signal-to-metrics/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
}

// TestPutSignalToMetrics_ReplacesServerManagedLabels pins the body labels the
// wrapper sends. A document copied from the API carries every server-managed
// label; the API rejects a stale dash0.com/version and a dash0.com/dataset that
// disagrees with ?dataset= (both with 400), so those are stripped and the
// dataset label is set to the resource's dataset. The origin comes from the
// URL and the id must not address another rule, so neither is sent.
func TestPutSignalToMetrics_ReplacesServerManagedLabels(t *testing.T) {
	copiedFromAPI := `{
		"kind": "Dash0SignalToMetrics",
		"metadata": {
			"name": "checkout-errors",
			"labels": {
				"dash0.com/dataset": "other",
				"dash0.com/id": "signal_to_metrics_01other",
				"dash0.com/origin": "tf_other",
				"dash0.com/source": "terraform",
				"dash0.com/version": "7"
			},
			"annotations": {
				"dash0.com/created-at": "2026-10-30T09:13:08.959631Z",
				"dash0.com/updated-at": "2026-10-30T09:13:08.959631Z"
			}
		},
		"spec": {
			"enabled": true,
			"match": {"signal": "spans", "filters": []},
			"output": {"name": "checkout_errors", "interval": "60s"}
		}
	}`

	for _, tc := range []struct {
		name  string
		write func(c Client) error
	}{
		{"create without labels", func(c Client) error {
			return c.CreateSignalToMetrics(t.Context(), "tf_abc", minimalSignalToMetricsJSON, "production")
		}},
		{"create with a document copied from the API", func(c Client) error {
			return c.CreateSignalToMetrics(t.Context(), "tf_abc", copiedFromAPI, "production")
		}},
		{"update with a document copied from the API", func(c Client) error {
			return c.UpdateSignalToMetrics(t.Context(), "tf_abc", copiedFromAPI, "production")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorded := newTSATestClient(t, echoSignalToMetricsHandler)

			require.NoError(t, tc.write(c))

			require.Len(t, *recorded, 1)
			var sent map[string]interface{}
			require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))
			metadata := sent["metadata"].(map[string]interface{})
			assert.Equal(t, map[string]interface{}{"dash0.com/dataset": "production"}, metadata["labels"],
				"only the dataset label may be sent, set to the resource's dataset")

			// Server timestamps are not sent back.
			annotations, _ := metadata["annotations"].(map[string]interface{})
			assert.NotContains(t, annotations, "dash0.com/created-at")
			assert.NotContains(t, annotations, "dash0.com/updated-at")
		})
	}
}

// TestCreateSignalToMetrics_PassesSpecThrough pins that every baseline field
// survives the typed round trip: the filter and matcher unions, the output
// options, and the dataset routing fields.
func TestCreateSignalToMetrics_PassesSpecThrough(t *testing.T) {
	const spec = `{
		"enabled": false,
		"display": {"name": "Checkout latency"},
		"targetDatasetMode": "both",
		"alternativeDatasetId": "metrics-only",
		"match": {
			"signal": "spans",
			"filters": [
				{"key": "service.name", "operator": "is_one_of", "values": ["checkout", "cart"]},
				{"key": "otel.span.duration", "operator": "gt", "value": "100"}
			]
		},
		"output": {
			"name": "checkout_latency",
			"description": "Checkout span latency",
			"interval": "1m",
			"type": "exponential-histogram",
			"value": "span.end_time_unix_nano - span.start_time_unix_nano",
			"count": "1",
			"maxSize": 80,
			"keepSignalAttributes": [{"operator": "starts_with", "value": "http."}],
			"keepResourceAttributes": [{"operator": "is", "value": "service.name"}]
		}
	}`
	ruleJSON := `{"kind": "Dash0SignalToMetrics", "metadata": {"name": "checkout-latency"}, "spec": ` + spec + `}`

	c, recorded := newTSATestClient(t, echoSignalToMetricsHandler)

	err := c.CreateSignalToMetrics(t.Context(), "tf_abc", ruleJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	var sent map[string]interface{}
	require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))
	sentSpec, err := json.Marshal(sent["spec"])
	require.NoError(t, err)
	assert.JSONEq(t, spec, string(sentSpec), "an explicit enabled: false and every output option must reach the wire")
}

func TestCreateSignalToMetrics_MalformedJSONDoesNotIssueRequest(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		echoSignalToMetricsHandler(w, r)
	})

	err := c.CreateSignalToMetrics(t.Context(), "tf_abc", "not json", "production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing signal-to-metrics rule JSON")
	assert.Equal(t, int32(0), calls.Load())
}

func TestGetSignalToMetrics_ReturnsMarshalledBody(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSignalToMetricsHandler)

	got, err := c.GetSignalToMetrics(t.Context(), "tf_abc", "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, "/api/signal-to-metrics/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
	assert.JSONEq(t, minimalSignalToMetricsJSON, got)
}

// TestGetSignalToMetrics_ReturnsTombstoneVerbatim pins the soft-delete
// contract: a deleted rule comes back as a 200 carrying dash0.com/deleted-at,
// and the wrapper must pass that annotation through for the resource layer.
func TestGetSignalToMetrics_ReturnsTombstoneVerbatim(t *testing.T) {
	const tombstone = `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-errors","annotations":{"dash0.com/deleted-at":"2026-10-30T09:13:52.138067Z"}},"spec":{"enabled":true,"display":{"name":""},"match":{"signal":"spans","filters":[]},"output":{"name":"checkout_errors","interval":"60s"}}}`
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(tombstone))
	})

	got, err := c.GetSignalToMetrics(t.Context(), "tf_abc", "production")
	require.NoError(t, err)
	assert.Contains(t, got, `"dash0.com/deleted-at":"2026-10-30T09:13:52.138067Z"`)
}

func TestGetSignalToMetrics_NotFound(t *testing.T) {
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})

	_, err := c.GetSignalToMetrics(t.Context(), "tf_missing", "production")
	require.Error(t, err)
	assert.True(t, dash0.IsNotFound(err), "a 404 must surface as an error dash0.IsNotFound recognizes")
}

func TestUpdateSignalToMetrics_IssuesPutToExistingOrigin(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSignalToMetricsHandler)

	err := c.UpdateSignalToMetrics(t.Context(), "tf_abc", minimalSignalToMetricsJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/api/signal-to-metrics/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
}

func TestUpdateSignalToMetrics_MalformedJSON(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSignalToMetricsHandler)

	err := c.UpdateSignalToMetrics(t.Context(), "tf_abc", "not json", "production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing signal-to-metrics rule JSON")
	assert.Empty(t, *recorded)
}

func TestDeleteSignalToMetrics(t *testing.T) {
	for name, status := range map[string]int{
		"200 OK":         http.StatusOK,
		"204 No Content": http.StatusNoContent,
	} {
		t.Run(name, func(t *testing.T) {
			c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})

			err := c.DeleteSignalToMetrics(t.Context(), "tf_abc", "production")
			require.NoError(t, err)

			require.Len(t, *recorded, 1)
			req := (*recorded)[0]
			assert.Equal(t, http.MethodDelete, req.method)
			assert.Equal(t, "/api/signal-to-metrics/tf_abc", req.path)
			assert.Equal(t, "dataset=production", req.query)
		})
	}

	// Deleting an already-deleted rule returns 404; the resource layer treats
	// that as success, so it must stay recognizable.
	t.Run("404 on an already-deleted rule", func(t *testing.T) {
		c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"Not Found"}}`))
		})

		err := c.DeleteSignalToMetrics(t.Context(), "tf_abc", "production")
		require.Error(t, err)
		assert.True(t, dash0.IsNotFound(err))
	})
}

func TestResolveSignalToMetrics(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	listHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rules := []dash0.SignalToMetricsDefinition{
			{
				Kind: dash0.Dash0SignalToMetrics,
				Metadata: dash0.SignalToMetricsMetadata{
					Name: "other",
					Labels: &dash0.SignalToMetricsLabels{
						Dash0Comid:     strPtr("signal_to_metrics_01other"),
						Dash0Comorigin: strPtr("tf_other"),
					},
				},
			},
			{
				// UI-created: no origin label, resolvable via the id fallback.
				Kind: dash0.Dash0SignalToMetrics,
				Metadata: dash0.SignalToMetricsMetadata{
					Name: "ui-created",
					Labels: &dash0.SignalToMetricsLabels{
						Dash0Comid: strPtr("signal_to_metrics_01uicreated"),
					},
				},
			},
			{
				// No labels at all: must be skipped, not dereferenced.
				Kind:     dash0.Dash0SignalToMetrics,
				Metadata: dash0.SignalToMetricsMetadata{Name: "unlabelled"},
			},
			{
				Kind: dash0.Dash0SignalToMetrics,
				Metadata: dash0.SignalToMetricsMetadata{
					Name: "target",
					Labels: &dash0.SignalToMetricsLabels{
						Dash0Comid:     strPtr("signal_to_metrics_01target"),
						Dash0Comorigin: strPtr("tf_target"),
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(dash0.GetSignalToMetricsResponse{SignalToMetrics: rules})
	}

	t.Run("matches on the origin label", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveSignalToMetrics(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "signal_to_metrics_01target", id)
	})

	t.Run("falls back to the id label", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveSignalToMetrics(t.Context(), "signal_to_metrics_01uicreated", "production")
		require.NoError(t, err)
		assert.Equal(t, "signal_to_metrics_01uicreated", id)
	})

	t.Run("no match returns an empty id and no error", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveSignalToMetrics(t.Context(), "tf_missing", "production")
		require.NoError(t, err)
		assert.Equal(t, "", id)
	})

	// The resource layer downgrades a list failure to a warning. Swallowing the
	// error here would make that branch unreachable, so it must propagate.
	t.Run("propagates an error when the list call fails", func(t *testing.T) {
		c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		})
		id, err := c.ResolveSignalToMetrics(t.Context(), "tf_target", "production")
		require.Error(t, err)
		assert.Equal(t, "", id)
	})
}
