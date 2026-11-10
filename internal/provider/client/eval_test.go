package client

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minimalEvalJSON = `{
	"kind": "Dash0GenerativeAiEvaluation",
	"metadata": {"name": "answer-relevance"},
	"spec": {
		"enabled": true,
		"display": {"name": "Answer relevance"},
		"match": {"filters": [{"key": "gen_ai.operation.name", "operator": "is_set"}]},
		"sample": {"rate": 0.25},
		"judge": {
			"model": "light",
			"systemPrompt": "You judge answers.",
			"userPrompt": "{{ gen_ai.input.messages }}",
			"output": {"kind": "categorical", "spec": {"options": [{"label": "good", "intent": "positive"}]}}
		}
	}
}`

func echoEvalHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(minimalEvalJSON))
}

// A document copied from the API carries every server-managed label and
// timestamp. The API rejects a stale dash0.com/version, so the wrapper must
// send only the dataset and origin of the resource, and the spec unchanged.
func TestPutEval_SendsResourceIdentityAndSpec(t *testing.T) {
	copiedFromAPI := `{
		"kind": "Dash0GenerativeAiEvaluation",
		"metadata": {
			"name": "answer-relevance",
			"labels": {
				"dash0.com/dataset": "other",
				"dash0.com/id": "eval_01other",
				"dash0.com/origin": "tf_other",
				"dash0.com/source": "terraform",
				"dash0.com/version": "7"
			},
			"annotations": {
				"dash0.com/folder-path": "/quality",
				"dash0.com/created-at": "2026-11-09T09:13:08Z",
				"dash0.com/created-by": "user_01abc",
				"dash0.com/updated-at": "2026-11-09T09:13:08Z"
			}
		},
		"spec": {
			"enabled": true,
			"display": {"name": "Answer relevance"},
			"match": {"filters": [{"key": "gen_ai.operation.name", "operator": "is_set"}]},
			"sample": {"rate": 0.25},
			"judge": {
				"model": "light",
				"systemPrompt": "You judge answers.",
				"userPrompt": "{{ gen_ai.input.messages }}",
				"output": {"kind": "categorical", "spec": {"options": [{"label": "good", "intent": "positive"}]}}
			}
		}
	}`

	for name, write := range map[string]func(c Client) error{
		"create": func(c Client) error {
			return c.CreateEval(t.Context(), "tf_abc", copiedFromAPI, "production")
		},
		"update": func(c Client) error {
			return c.UpdateEval(t.Context(), "tf_abc", copiedFromAPI, "production")
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, recorded := newTSATestClient(t, echoEvalHandler)

			require.NoError(t, write(c))

			require.Len(t, *recorded, 1)
			req := (*recorded)[0]
			assert.Equal(t, http.MethodPut, req.method)
			assert.Equal(t, "/api/generative-ai-evaluations/tf_abc", req.path)
			assert.Equal(t, "dataset=production", req.query)

			var sent map[string]interface{}
			require.NoError(t, json.Unmarshal(req.body, &sent))
			metadata := sent["metadata"].(map[string]interface{})
			assert.Equal(t, map[string]interface{}{"dash0.com/dataset": "production", "dash0.com/origin": "tf_abc"}, metadata["labels"])
			assert.Equal(t, map[string]interface{}{"dash0.com/folder-path": "/quality"}, metadata["annotations"])

			var expected map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(copiedFromAPI), &expected))
			assert.Equal(t, expected["spec"], sent["spec"], "the spec, including the judge output union, must survive the typed round trip")
		})
	}
}

func TestResolveEval(t *testing.T) {
	list := `{"generativeAiEvaluations": [
		{"kind": "Dash0GenerativeAiEvaluation", "metadata": {"name": "a", "labels": {"dash0.com/id": "eval_01a", "dash0.com/origin": "tf_a"}}, "spec": {"enabled": true, "display": {"name": "a"}, "match": {"filters": []}, "sample": {"rate": 1}, "judge": {"model": "light", "systemPrompt": "s", "userPrompt": "u", "output": {"kind": "boolean"}}}},
		{"kind": "Dash0GenerativeAiEvaluation", "metadata": {"name": "b", "labels": {"dash0.com/id": "eval_01b", "dash0.com/origin": "tf_b"}}, "spec": {"enabled": true, "display": {"name": "b"}, "match": {"filters": []}, "sample": {"rate": 1}, "judge": {"model": "light", "systemPrompt": "s", "userPrompt": "u", "output": {"kind": "boolean"}}}}
	]}`
	c, recorded := newTSATestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(list))
	})

	id, err := c.ResolveEval(t.Context(), "tf_b", "production")
	require.NoError(t, err)
	assert.Equal(t, "eval_01b", id)
	assert.Equal(t, "/api/generative-ai-evaluations", (*recorded)[0].path)
	assert.Equal(t, "dataset=production", (*recorded)[0].query)

	id, err = c.ResolveEval(t.Context(), "tf_missing", "production")
	require.NoError(t, err)
	assert.Empty(t, id)
}
