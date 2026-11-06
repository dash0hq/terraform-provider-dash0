package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const telemetryTransformationRuleJSON = `{
  "kind":"Dash0TelemetryTransformationRule",
  "metadata":{
    "name":"normalize-logs",
    "labels":{"dash0.com/id":"copied-id","dash0.com/origin":"copied-origin","dash0.com/dataset":"copied-dataset","dash0.com/source":"ui"},
    "annotations":{"dash0.com/created-at":"2026-11-01T00:00:00Z","dash0.com/updated-at":"2026-11-02T14:03:08.123456789Z","dash0.com/deleted-at":"2026-11-01T01:00:00Z"}
  },
  "spec":{
    "enabled":false,"signal":"logs","context":"log","priority":0,
    "conditions":["attributes[\"env\"] == \"production\""],
    "transformations":[
      {"name":"first","statements":["set(attributes[\"stage\"], \"one\")","set(attributes[\"stage\"], \"two\")"]},
      {"name":"second","statements":["set(attributes[\"done\"], true)"]}
    ],
    "examples":[{"name":"sample","otlp":{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1793628000123456789","body":{"stringValue":"sample log"}}]}]}]}}]
  }
}`

func TestTelemetryTransformationRuleWrites(t *testing.T) {
	for _, dataset := range []string{"default", "production"} {
		for _, create := range []bool{true, false} {
			t.Run(dataset+map[bool]string{true: "/create", false: "/update"}[create], func(t *testing.T) {
				c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"kind":"Dash0TelemetryTransformationRule","metadata":{"name":"normalize-logs","labels":{"dash0.com/id":"ttr_server-id"}},"spec":{"enabled":false,"signal":"logs","context":"log","transformations":[]}}`))
				})
				if create {
					id, err := c.CreateTelemetryTransformationRule(t.Context(), "tf_fresh", telemetryTransformationRuleJSON, dataset)
					require.NoError(t, err)
					assert.Equal(t, "ttr_server-id", id)
				} else {
					require.NoError(t, c.UpdateTelemetryTransformationRule(t.Context(), "ttr_existing", telemetryTransformationRuleJSON, dataset))
				}
				require.Len(t, *recorded, 1)
				request := (*recorded)[0]
				assert.Equal(t, "dataset="+dataset, request.query)
				if create {
					assert.Equal(t, http.MethodPost, request.method)
					assert.Equal(t, "/api/telemetry-transformation-rules", request.path)
				} else {
					assert.Equal(t, http.MethodPut, request.method)
					assert.Equal(t, "/api/telemetry-transformation-rules/ttr_existing", request.path)
				}

				var sent, original map[string]interface{}
				require.NoError(t, json.Unmarshal(request.body, &sent))
				require.NoError(t, json.Unmarshal([]byte(telemetryTransformationRuleJSON), &original))
				assert.Equal(t, original["spec"], sent["spec"], "ordered transformations, conditions, zero priority, and stored examples must survive unchanged")
				metadata := sent["metadata"].(map[string]interface{})
				labels := metadata["labels"].(map[string]interface{})
				assert.NotContains(t, labels, "dash0.com/id")
				assert.NotContains(t, labels, "dash0.com/source")
				assert.Equal(t, dataset, labels["dash0.com/dataset"])
				if create {
					assert.Equal(t, "tf_fresh", labels["dash0.com/origin"])
				} else {
					assert.Equal(t, "copied-origin", labels["dash0.com/origin"], "import by ID must not rewrite the immutable origin")
				}
				assert.Equal(t, map[string]interface{}{"dash0.com/updated-at": "2026-11-02T14:03:08.123456789Z"}, metadata["annotations"])
			})
		}
	}
}

func TestTelemetryTransformationRuleUpdateWithoutExamples(t *testing.T) {
	for _, examples := range []string{"", `,"examples":[]`} {
		t.Run(examples, func(t *testing.T) {
			c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"kind":"Dash0TelemetryTransformationRule","metadata":{"name":"rule"},"spec":{"enabled":false,"signal":"logs","context":"log","transformations":[]}}`))
			})
			body := `{"kind":"Dash0TelemetryTransformationRule","metadata":{"name":"rule"},"spec":{"enabled":false,"signal":"logs","context":"log","transformations":[]` + examples + `}}`
			require.NoError(t, c.UpdateTelemetryTransformationRule(t.Context(), "tf_origin", body, "production"))
			require.Len(t, *recorded, 1, "updates must not fetch or merge a rule's stored examples")
			var sent map[string]interface{}
			require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))
			spec := sent["spec"].(map[string]interface{})
			if examples == "" {
				assert.NotContains(t, spec, "examples")
			} else {
				assert.Equal(t, []interface{}{}, spec["examples"])
			}
		})
	}
}

func TestTelemetryTransformationRuleErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing", http.StatusNotFound, `{"error":{"code":404,"message":"missing"}}`},
		{"stale", http.StatusBadRequest, `{"error":{"code":400,"type":"superseded_version","message":"rule was updated"}}`},
		{"invalid", http.StatusBadRequest, `{"error":{"code":400,"type":"invalid_rule","message":"statement is not permitted"},"violations":[{"attribute":"body","message":"protected field"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Trace-Id", "transformation-trace")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := c.UpdateTelemetryTransformationRule(t.Context(), "tf_origin", telemetryTransformationRuleJSON, "production")
			var apiErr *dash0.APIError
			require.True(t, errors.As(err, &apiErr), "%v", err)
			assert.Equal(t, tc.status, apiErr.StatusCode)
			assert.Equal(t, tc.body, apiErr.Body)
			assert.Equal(t, "transformation-trace", apiErr.TraceID)
			require.Len(t, *recorded, 1, "a failed update must never become a create")
			assert.Equal(t, http.MethodPut, (*recorded)[0].method)
		})
	}
}

func TestTelemetryTransformationRuleRejectsUnsupportedInput(t *testing.T) {
	for _, document := range []string{
		"not JSON",
		`{"kind":"Dash0TelemetryTransformationRule","spec":{"enabled":false,"statements":["set(body, 1)"]}}`,
		`{"kind":"Dash0TelemetryTransformationRule","metadata":{"labels":{"custom":{"team":"ignored"}}}}`,
	} {
		c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		_, err := c.CreateTelemetryTransformationRule(t.Context(), "tf_fresh", document, "production")
		require.Error(t, err)
		require.Error(t, c.UpdateTelemetryTransformationRule(t.Context(), "tf_origin", document, "production"))
		assert.Empty(t, *recorded, "unrecognized fields must not be silently dropped on write")
	}
}
