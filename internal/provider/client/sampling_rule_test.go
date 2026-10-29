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

const minimalSamplingRuleJSON = `{
	"kind": "Dash0Sampling",
	"metadata": {"name": "keep-errors"},
	"spec": {
		"enabled": true,
		"conditions": {"kind": "error", "spec": {}}
	}
}`

// echoSamplingRuleHandler responds 200 with a minimal valid sampling rule,
// which is what the API client needs in order to decode a successful PUT or
// GET.
func echoSamplingRuleHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(minimalSamplingRuleJSON))
}

func TestCreateSamplingRule_IssuesPutWithDatasetQueryParam(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSamplingRuleHandler)

	err := c.CreateSamplingRule(t.Context(), "tf_abc", minimalSamplingRuleJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/api/sampling-rules/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
}

// TestCreateSamplingRule_DoesNotStampOriginLabel pins that the wrapper does not
// add a dash0.com/origin or dash0.com/dataset label to the request body: the
// server derives both from the URL.
func TestCreateSamplingRule_DoesNotStampOriginLabel(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSamplingRuleHandler)

	err := c.CreateSamplingRule(t.Context(), "tf_abc", minimalSamplingRuleJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	var sent dash0.SamplingDefinition
	require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))

	if sent.Metadata.Labels != nil {
		assert.Nil(t, sent.Metadata.Labels.Dash0Comorigin, "wrapper must not stamp dash0.com/origin into the body")
		assert.Nil(t, sent.Metadata.Labels.Dash0Comdataset, "wrapper must not stamp dash0.com/dataset into the body")
	}
}

// TestCreateSamplingRule_PassesConditionTreeThrough pins that the condition
// union survives the typed round trip. SamplingCondition holds raw JSON, so a
// nested `and` tree with every leaf kind must reach the wire unchanged.
func TestCreateSamplingRule_PassesConditionTreeThrough(t *testing.T) {
	const conditions = `{
		"kind": "and",
		"spec": {
			"conditions": [
				{"kind": "ottl", "spec": {"ottl": "resource.attributes[\"deployment.environment.name\"] == \"production\""}},
				{"kind": "error", "spec": {}},
				{"kind": "probabilistic", "spec": {"rate": 0.5}}
			]
		}
	}`
	ruleJSON := `{
		"kind": "Dash0Sampling",
		"metadata": {"name": "keep-production-errors"},
		"spec": {
			"enabled": false,
			"display": {"name": "Keep production errors"},
			"conditions": ` + conditions + `,
			"rateLimit": {"rate": 1000}
		}
	}`

	c, recorded := newTSATestClient(t, echoSamplingRuleHandler)

	err := c.CreateSamplingRule(t.Context(), "tf_abc", ruleJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	var sent map[string]interface{}
	require.NoError(t, json.Unmarshal((*recorded)[0].body, &sent))
	spec := sent["spec"].(map[string]interface{})

	sentConditions, err := json.Marshal(spec["conditions"])
	require.NoError(t, err)
	assert.JSONEq(t, conditions, string(sentConditions))
	assert.Equal(t, false, spec["enabled"], "an explicit enabled: false must reach the wire")
	assert.Equal(t, map[string]interface{}{"rate": float64(1000)}, spec["rateLimit"])
	assert.Equal(t, map[string]interface{}{"name": "Keep production errors"}, spec["display"])
}

func TestCreateSamplingRule_MalformedJSONDoesNotIssueRequest(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		echoSamplingRuleHandler(w, r)
	})

	err := c.CreateSamplingRule(t.Context(), "tf_abc", "not json", "production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing sampling rule JSON")
	assert.Equal(t, int32(0), calls.Load())
}

func TestGetSamplingRule_ReturnsMarshalledBody(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSamplingRuleHandler)

	got, err := c.GetSamplingRule(t.Context(), "tf_abc", "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodGet, req.method)
	assert.Equal(t, "/api/sampling-rules/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
	assert.JSONEq(t, minimalSamplingRuleJSON, got)
}

func TestGetSamplingRule_NotFound(t *testing.T) {
	c, _ := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})

	_, err := c.GetSamplingRule(t.Context(), "tf_missing", "production")
	require.Error(t, err)
	assert.True(t, dash0.IsNotFound(err), "a 404 must surface as an error dash0.IsNotFound recognizes")
}

func TestUpdateSamplingRule_IssuesPutToExistingOrigin(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSamplingRuleHandler)

	err := c.UpdateSamplingRule(t.Context(), "tf_abc", minimalSamplingRuleJSON, "production")
	require.NoError(t, err)

	require.Len(t, *recorded, 1)
	req := (*recorded)[0]
	assert.Equal(t, http.MethodPut, req.method)
	assert.Equal(t, "/api/sampling-rules/tf_abc", req.path)
	assert.Equal(t, "dataset=production", req.query)
}

func TestUpdateSamplingRule_MalformedJSON(t *testing.T) {
	c, recorded := newTSATestClient(t, echoSamplingRuleHandler)

	err := c.UpdateSamplingRule(t.Context(), "tf_abc", "not json", "production")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing sampling rule JSON")
	assert.Empty(t, *recorded)
}

func TestDeleteSamplingRule(t *testing.T) {
	for name, status := range map[string]int{
		"200 OK":         http.StatusOK,
		"204 No Content": http.StatusNoContent,
	} {
		t.Run(name, func(t *testing.T) {
			c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			})

			err := c.DeleteSamplingRule(t.Context(), "tf_abc", "production")
			require.NoError(t, err)

			require.Len(t, *recorded, 1)
			req := (*recorded)[0]
			assert.Equal(t, http.MethodDelete, req.method)
			assert.Equal(t, "/api/sampling-rules/tf_abc", req.path)
			assert.Equal(t, "dataset=production", req.query)
		})
	}
}

func TestResolveSamplingRule(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	listHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rules := []dash0.SamplingDefinition{
			{
				Kind: dash0.Dash0Sampling,
				Metadata: dash0.SamplingMetadata{
					Name: "other",
					Labels: &dash0.SamplingLabels{
						Dash0Comid:     strPtr("11111111-1111-1111-1111-111111111111"),
						Dash0Comorigin: strPtr("tf_other"),
					},
				},
			},
			{
				// UI-created: no origin label, resolvable via the id fallback.
				Kind: dash0.Dash0Sampling,
				Metadata: dash0.SamplingMetadata{
					Name: "ui-created",
					Labels: &dash0.SamplingLabels{
						Dash0Comid: strPtr("22222222-2222-2222-2222-222222222222"),
					},
				},
			},
			{
				// No labels at all: must be skipped, not dereferenced.
				Kind:     dash0.Dash0Sampling,
				Metadata: dash0.SamplingMetadata{Name: "unlabelled"},
			},
			{
				Kind: dash0.Dash0Sampling,
				Metadata: dash0.SamplingMetadata{
					Name: "target",
					Labels: &dash0.SamplingLabels{
						Dash0Comid:     strPtr("33333333-3333-3333-3333-333333333333"),
						Dash0Comorigin: strPtr("tf_target"),
					},
				},
			},
		}
		// The empty condition union marshals as null; give every rule a valid one.
		for i := range rules {
			require.NoError(t, rules[i].Spec.Conditions.FromSamplingConditionError(dash0.SamplingConditionError{Kind: dash0.SamplingConditionErrorKindError}))
		}
		_ = json.NewEncoder(w).Encode(dash0.GetSamplingRulesResponse{SamplingRules: rules})
	}

	t.Run("matches on the origin label", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveSamplingRule(t.Context(), "tf_target", "production")
		require.NoError(t, err)
		assert.Equal(t, "33333333-3333-3333-3333-333333333333", id)
	})

	t.Run("falls back to the id label", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveSamplingRule(t.Context(), "22222222-2222-2222-2222-222222222222", "production")
		require.NoError(t, err)
		assert.Equal(t, "22222222-2222-2222-2222-222222222222", id)
	})

	t.Run("no match returns an empty id and no error", func(t *testing.T) {
		c, _ := newTSATestClient(t, listHandler)
		id, err := c.ResolveSamplingRule(t.Context(), "tf_missing", "production")
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
		id, err := c.ResolveSamplingRule(t.Context(), "tf_target", "production")
		require.Error(t, err)
		assert.Equal(t, "", id)
	})
}
