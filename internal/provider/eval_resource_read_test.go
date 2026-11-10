package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testEvalClient is a minimal stand-in for the API client: the drift table
// only needs to control what a GET returns. Like the real wrapper, it decodes
// the server's document into dash0.GenerativeAiEvaluationDefinition and
// re-encodes it, so fields the typed round trip would drop or reshape are
// covered.
type testEvalClient struct {
	client.Client
	serverResponse string
}

func (c *testEvalClient) GetEval(_ context.Context, _, _ string) (string, error) {
	var evaluation dash0.GenerativeAiEvaluationDefinition
	if err := json.Unmarshal([]byte(c.serverResponse), &evaluation); err != nil {
		return "", err
	}
	b, err := json.Marshal(evaluation)
	return string(b), err
}

// TestEvalResource_ReadWithDiffs is the drift-detection table. It exercises
// Read end to end — the typed round trip and ResourceYAMLEquivalent — because
// that combination decides whether a plan comes back clean.
//
// The server responses mirror what the live API returned when probed: it
// stores display.description, judge.allowPartialPrompt, filter values and the
// categorical options verbatim, and echoes no default for an omitted field.
func TestEvalResource_ReadWithDiffs(t *testing.T) {
	const configYaml = `kind: Dash0GenerativeAiEvaluation
metadata:
  name: answer-relevance
  annotations:
    dash0.com/folder-path: /quality
    dash0.com/sharing: team:team_01abc
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
      - key: service.name
        operator: is
        value: chat
  sample:
    rate: 0.25
  judge:
    model: light
    systemPrompt: You judge answers.
    userPrompt: "Question: {{ gen_ai.input.messages }}"
    output:
      kind: categorical
      spec:
        options:
          - label: relevant
            intent: positive
          - label: off-topic
`
	const serverLabels = `"labels":{"dash0.com/origin":"tf_abc","dash0.com/id":"eval_01abc","dash0.com/dataset":"default","dash0.com/source":"terraform","dash0.com/version":"3"}`
	const serverTimestamps = `"dash0.com/created-at":"2026-11-09T09:13:08.959631Z","dash0.com/updated-at":"2026-11-09T09:13:51.514134Z"`
	const userAnnotations = `"dash0.com/folder-path":"/quality","dash0.com/sharing":"team:team_01abc"`
	const match = `"match":{"filters":[{"key":"gen_ai.operation.name","operator":"is_set"},{"key":"service.name","operator":"is","value":"chat"}]}`
	const options = `[{"label":"relevant","intent":"positive"},{"label":"off-topic"}]`
	response := func(annotations, enabled, display, match, judgeExtra, options string) string {
		return `{"kind":"Dash0GenerativeAiEvaluation","metadata":{"name":"answer-relevance",` + serverLabels +
			`,"annotations":{` + annotations + `}},"spec":{"enabled":` + enabled + `,"display":` + display + `,` + match +
			`,"sample":{"rate":0.25},"judge":{"model":"light","systemPrompt":"You judge answers.","userPrompt":"Question: {{ gen_ai.input.messages }}"` +
			judgeExtra + `,"output":{"kind":"categorical","spec":{"options":` + options + `}}}}}`
	}
	const display = `{"name":"Answer relevance"}`
	unchanged := response(userAnnotations+","+serverTimestamps, "true", display, match, "", options)

	tests := []struct {
		name              string
		stateYaml         string
		serverResponse    string
		expectYamlUpdated bool
		why               string
	}{
		{
			name:              "server labels and timestamps added - no drift",
			stateYaml:         configYaml,
			serverResponse:    unchanged,
			expectYamlUpdated: false,
			why:               "server-managed labels and timestamp annotations must not cause drift",
		},
		{
			name:              "created-by annotation added - no drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations+","+serverTimestamps+`,"dash0.com/created-by":"user_01abc"`, "true", display, match, "", options),
			expectYamlUpdated: false,
			why:               "dash0.com/created-by is server-managed",
		},
		{
			name:              "folder path changed out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(`"dash0.com/folder-path":"/moved","dash0.com/sharing":"team:team_01abc",`+serverTimestamps, "true", display, match, "", options),
			expectYamlUpdated: true,
			why:               "the API stores and returns dash0.com/folder-path, so a change to it is drift",
		},
		{
			name:              "sharing removed out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(`"dash0.com/folder-path":"/quality",`+serverTimestamps, "true", display, match, "", options),
			expectYamlUpdated: true,
			why:               "the API stores and returns dash0.com/sharing, so a removal is drift",
		},
		{
			name:              "disabled out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations, "false", display, match, "", options),
			expectYamlUpdated: true,
			why:               "an eval switched off in the UI must show up in the plan, since spec.enabled is required in the config",
		},
		{
			name:              "description added out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations, "true", `{"name":"Answer relevance","description":"Added in the UI"}`, match, "", options),
			expectYamlUpdated: true,
			why:               "the typed round trip must keep display.description",
		},
		{
			name:              "allowPartialPrompt false on the server, omitted in config - no drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations, "true", display, match, `,"allowPartialPrompt":false`, options),
			expectYamlUpdated: false,
			why:               "false is the default for allowPartialPrompt, and normalization drops zero values",
		},
		{
			name:              "allowPartialPrompt switched on out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations, "true", display, match, `,"allowPartialPrompt":true`, options),
			expectYamlUpdated: true,
			why:               "a judge that now accepts partial prompts is a real change",
		},
		{
			name:              "filter value changed out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations, "true", display, strings.Replace(match, `"value":"chat"`, `"value":"search"`, 1), "", options),
			expectYamlUpdated: true,
			why:               "the typed round trip must keep filter values",
		},
		{
			name:              "intent added to an option out of band - drift",
			stateYaml:         configYaml,
			serverResponse:    response(userAnnotations, "true", display, match, "", `[{"label":"relevant","intent":"positive"},{"label":"off-topic","intent":"negative"}]`),
			expectYamlUpdated: true,
			why:               "the typed round trip must keep the intent of a categorical option",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &testEvalClient{serverResponse: tc.serverResponse}
			r := &EvalResource{client: c}

			state := evalState(strPtr("tf_abc"), strPtr("eval_01abc"), strPtr("default"), strPtr(tc.stateYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			assert.Equal(t, 0, resp.Diagnostics.WarningsCount(), "a well-formed response must not warn")

			var result evalModel
			require.False(t, resp.State.Get(context.Background(), &result).HasError())

			if tc.expectYamlUpdated {
				assert.NotEqual(t, tc.stateYaml, result.EvalYaml.ValueString(), tc.why)
			} else {
				assert.Equal(t, tc.stateYaml, result.EvalYaml.ValueString(), tc.why)
			}
		})
	}
}
