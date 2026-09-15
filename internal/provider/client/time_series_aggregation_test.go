package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const minimalTimeSeriesAggregationJSON = `{
	"kind": "Dash0TimeSeriesAggregation",
	"metadata": {"name": "http-latency-rollup"},
	"spec": {
		"enabled": true,
		"match": {"metricNameMatcher": {"operator": "is", "value": "http.server.duration"}},
		"sample": {"interval": "5m"}
	}
}`

// recordedRequest captures the wire-level details of a single request handled
// by the test server.
type recordedRequest struct {
	method string
	path   string
	query  string
	body   []byte
}

// newTSATestClient spins up an httptest server with the given handler and
// returns a dash0Client wired to it, plus a pointer to the recorded requests.
func newTSATestClient(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*dash0Client, *[]recordedRequest) {
	t.Helper()

	var recorded []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   body,
		})
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	inner, err := dash0.NewClient(
		dash0.WithApiUrl(server.URL),
		dash0.WithAuthToken("auth_test-token"),
		dash0.WithUserAgent("test"),
	)
	require.NoError(t, err)

	return &dash0Client{inner: inner, apiURL: server.URL}, &recorded
}

// echoTSAHandler responds 200 with a minimal valid aggregation document, which
// is what the API client needs in order to decode a successful PUT or GET.
func echoTSAHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(minimalTimeSeriesAggregationJSON))
}

func TestCreateTimeSeriesAggregation_IssuesPutWithDatasetQueryParam(t *testing.T) {
	c, recorded := newTSATestClient(t, echoTSAHandler)

	err := c.CreateTimeSeriesAggregation(t.Context(), "tf_abc", minimalTimeSeriesAggregationJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/api/time-series-aggregations/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
}

// TestCreateTimeSeriesAggregation_DoesNotStampOriginLabel pins KTD2: the
// wrapper must NOT add a dash0.com/origin label to the request body. The
// server derives the origin from the URL path. Reintroducing a stamping
// helper (as the recording rule and spam filter wrappers have) would break
// this test on purpose.
func TestCreateTimeSeriesAggregation_DoesNotStampOriginLabel(t *testing.T) {
	c, recorded := newTSATestClient(t, echoTSAHandler)

	err := c.CreateTimeSeriesAggregation(t.Context(), "tf_abc", minimalTimeSeriesAggregationJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	var sent dash0.TimeSeriesAggregationDefinition
	require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))

	if sent.Metadata.Labels != nil {
		assert.Nil(t, sent.Metadata.Labels.Dash0Comorigin, "wrapper must not stamp dash0.com/origin into the body")
		assert.Nil(t, sent.Metadata.Labels.Dash0Comdataset, "wrapper must not stamp dash0.com/dataset into the body")
	}
}

func TestCreateTimeSeriesAggregation_PassesUserLabelsThrough(t *testing.T) {
	const withLabels = `{
		"kind": "Dash0TimeSeriesAggregation",
		"metadata": {
			"name": "http-latency-rollup",
			"labels": {"custom": {"team": "platform"}}
		},
		"spec": {
			"enabled": true,
			"match": {"metricNameMatcher": {"operator": "is", "value": "http.server.duration"}},
			"sample": {"interval": "5m"}
		}
	}`

	c, recorded := newTSATestClient(t, echoTSAHandler)

	err := c.CreateTimeSeriesAggregation(t.Context(), "tf_abc", withLabels, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	var sent dash0.TimeSeriesAggregationDefinition
	require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))
	require.NotNil(t, sent.Metadata.Labels)
	require.NotNil(t, sent.Metadata.Labels.Custom)
	assert.Equal(t, map[string]string{"team": "platform"}, *sent.Metadata.Labels.Custom)
}

func TestCreateTimeSeriesAggregation_MalformedJSONDoesNotIssueRequest(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		echoTSAHandler(w, r)
	})

	err := c.CreateTimeSeriesAggregation(t.Context(), "tf_abc", "not json", "production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing time series aggregation JSON")
	assert.Equal(t, int32(0), calls.Load())
}

func TestGetTimeSeriesAggregation_ReturnsMarshalledBody(t *testing.T) {
	c, recorded := newTSATestClient(t, echoTSAHandler)

	got, err := c.GetTimeSeriesAggregation(t.Context(), "tf_abc", "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, "/api/time-series-aggregations/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
	assert.JSONEq(t, minimalTimeSeriesAggregationJSON, got)
}

func TestGetTimeSeriesAggregation_NotFound(t *testing.T) {
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})

	_, err := c.GetTimeSeriesAggregation(t.Context(), "tf_missing", "production")
	require.Error(t, err)
	assert.True(t, dash0.IsNotFound(err), "a 404 must surface as an error dash0.IsNotFound recognizes")
}

// TestGetTimeSeriesAggregation_ReturnsTombstoneVerbatim pins KTD7's division of
// labour: TSA deletes are soft, so a GET after a delete returns 200 with a
// dash0.com/deleted-at annotation. The wrapper stays a thin passthrough and
// hands the tombstone to the resource layer, which owns gone-detection.
func TestGetTimeSeriesAggregation_ReturnsTombstoneVerbatim(t *testing.T) {
	const tombstone = `{
		"kind": "Dash0TimeSeriesAggregation",
		"metadata": {
			"name": "http-latency-rollup",
			"annotations": {"dash0.com/deleted-at": "2026-09-14T10:00:00Z"}
		},
		"spec": {
			"enabled": true,
			"match": {"metricNameMatcher": {"operator": "is", "value": "http.server.duration"}},
			"sample": {"interval": "5m"}
		}
	}`

	c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(tombstone))
	})

	got, err := c.GetTimeSeriesAggregation(t.Context(), "tf_abc", "production")
	require.NoError(t, err, "a tombstone is a successful read, not an error")

	var def dash0.TimeSeriesAggregationDefinition
	require.NoError(t, json.Unmarshal([]byte(got), &def))
	require.NotNil(t, def.Metadata.Annotations)
	assert.NotNil(t, def.Metadata.Annotations.Dash0ComdeletedAt, "deleted-at annotation must survive to the resource layer")
}

func TestUpdateTimeSeriesAggregation_IssuesPutToExistingOrigin(t *testing.T) {
	c, recorded := newTSATestClient(t, echoTSAHandler)

	err := c.UpdateTimeSeriesAggregation(t.Context(), "tf_abc", minimalTimeSeriesAggregationJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/api/time-series-aggregations/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
}

func TestUpdateTimeSeriesAggregation_MalformedJSON(t *testing.T) {
	c, recorded := newTSATestClient(t, echoTSAHandler)

	err := c.UpdateTimeSeriesAggregation(t.Context(), "tf_abc", "not json", "production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing time series aggregation JSON")
	assert.Empty(t, *recorded)
}

func TestDeleteTimeSeriesAggregation(t *testing.T) {
	for name, status := range map[string]int{
		"200 OK":         http.StatusOK,
		"204 No Content": http.StatusNoContent,
	} {
		t.Run(name, func(t *testing.T) {
			c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})

			err := c.DeleteTimeSeriesAggregation(t.Context(), "tf_abc", "production")
			require.NoError(t, err)

			require.Len(t, *recorded, 1)
			req := (*recorded)[0]
			assert.Equal(t, http.MethodDelete, req.method)
			assert.Equal(t, "/api/time-series-aggregations/tf_abc", req.path)
			assert.Equal(t, "dataset=production", req.query)
		})
	}
}

func TestResolveTimeSeriesAggregation(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	listHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dash0.TimeSeriesAggregationListResponse{
			TimeSeriesAggregations: []dash0.TimeSeriesAggregationDefinition{
				{
					Kind: "Dash0TimeSeriesAggregation",
					Metadata: dash0.TimeSeriesAggregationMetadata{
						Name: "other",
						Labels: &dash0.TimeSeriesAggregationLabels{
							Dash0Comid:     strPtr("11111111-1111-1111-1111-111111111111"),
							Dash0Comorigin: strPtr("tf_other"),
						},
					},
				},
				{
					// UI-created: no origin label, resolvable via the id fallback.
					Kind: "Dash0TimeSeriesAggregation",
					Metadata: dash0.TimeSeriesAggregationMetadata{
						Name: "ui-created",
						Labels: &dash0.TimeSeriesAggregationLabels{
							Dash0Comid: strPtr("22222222-2222-2222-2222-222222222222"),
						},
					},
				},
				{
					Kind: "Dash0TimeSeriesAggregation",
					Metadata: dash0.TimeSeriesAggregationMetadata{
						Name: "target",
						Labels: &dash0.TimeSeriesAggregationLabels{
							Dash0Comid:     strPtr("33333333-3333-3333-3333-333333333333"),
							Dash0Comorigin: strPtr("tf_target"),
						},
					},
				},
			},
		})
	}

	t.Run("matches on the origin label", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveTimeSeriesAggregation(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "33333333-3333-3333-3333-333333333333", id)
	})

	t.Run("falls back to the id label", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveTimeSeriesAggregation(t.Context(), "22222222-2222-2222-2222-222222222222", "production")
		require.NoError(t, err)
		assert.Equal(t, "22222222-2222-2222-2222-222222222222", id)
	})

	t.Run("no match returns an empty id and no error", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveTimeSeriesAggregation(t.Context(), "tf_missing", "production")
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
		id, err := c.ResolveTimeSeriesAggregation(t.Context(), "tf_target", "production")
		require.Error(t, err)
		assert.Equal(t, "", id)
	})
}
