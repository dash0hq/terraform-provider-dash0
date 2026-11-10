package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const evalValidYaml = `kind: Dash0GenerativeAiEvaluation
metadata:
  name: answer-relevance
  annotations:
    dash0.com/folder-path: /quality
spec:
  enabled: true
  display:
    name: Answer relevance
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0.25
  judge:
    model: light
    systemPrompt: You judge answers.
    userPrompt: "Question: {{ gen_ai.input.messages }}"
    output:
      kind: numeric
      spec:
        threshold: 0.5
`

// evalAPIResponseJSON is what a GET returns for
// evalValidYaml: the same document with server-managed labels
// and timestamp annotations added.
const evalAPIResponseJSON = `{"kind":"Dash0GenerativeAiEvaluation","metadata":{"name":"answer-relevance","labels":{"dash0.com/origin":"tf_origin","dash0.com/id":"eval_01abc","dash0.com/dataset":"test-dataset","dash0.com/source":"terraform","dash0.com/version":"3"},"annotations":{"dash0.com/folder-path":"/quality","dash0.com/created-at":"2026-11-09T09:13:08Z","dash0.com/updated-at":"2026-11-09T09:13:08Z"}},"spec":{"enabled":true,"display":{"name":"Answer relevance"},"match":{"filters":[{"key":"gen_ai.operation.name","operator":"is_set"}]},"sample":{"rate":0.25},"judge":{"model":"light","systemPrompt":"You judge answers.","userPrompt":"Question: {{ gen_ai.input.messages }}","output":{"kind":"numeric","spec":{"threshold":0.5}}}}}`

var evalTombstoneJSON = strings.Replace(evalAPIResponseJSON,
	`"annotations":{`, `"annotations":{"dash0.com/deleted-at":"2026-11-09T10:00:00Z",`, 1)

func evalTestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":          schema.StringAttribute{Computed: true},
			"id":              schema.StringAttribute{Computed: true},
			"dataset":         schema.StringAttribute{Optional: true, Computed: true},
			evalYAMLAttribute: schema.StringAttribute{Required: true},
		},
	}
}

func evalValue(origin, id, dataset, evalYaml *string) tftypes.Value {
	str := func(p *string) tftypes.Value {
		if p == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *p)
	}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"origin":          tftypes.String,
		"id":              tftypes.String,
		"dataset":         tftypes.String,
		evalYAMLAttribute: tftypes.String,
	}}
	return tftypes.NewValue(objectType, map[string]tftypes.Value{
		"origin":          str(origin),
		"id":              str(id),
		"dataset":         str(dataset),
		evalYAMLAttribute: str(evalYaml),
	})
}

func evalState(origin, id, dataset, evalYaml *string) tfsdk.State {
	return tfsdk.State{Raw: evalValue(origin, id, dataset, evalYaml), Schema: evalTestSchema()}
}

func evalValidate(t *testing.T, evalYaml string) *resource.ValidateConfigResponse {
	t.Helper()
	r := &EvalResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: evalValue(nil, nil, nil, &evalYaml), Schema: evalTestSchema()},
	}, resp)
	return resp
}

func readEval(t *testing.T, stateYaml, apiResponse string) evalModel {
	t.Helper()
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}
	mockClient.On("GetEval", mock.Anything, "tf_origin", "test-dataset").Return(apiResponse, nil)

	state := evalState(strPtr("tf_origin"), strPtr("eval_01abc"), strPtr("test-dataset"), strPtr(stateYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var result evalModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	return result
}

func TestEvalResource_Create_PutsToGeneratedOriginInDefaultDataset(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient, defaultDataset: "provider-default"}

	var origin string
	mockClient.On("CreateEval", mock.Anything, mock.Anything, mock.Anything, "provider-default").
		Run(func(args mock.Arguments) { origin = args.String(1) }).Return(nil)
	mockClient.On("ResolveEval", mock.Anything, mock.Anything, "provider-default").Return("eval_01abc", nil)

	plan := tfsdk.Plan{Raw: evalValue(nil, nil, nil, strPtr(evalValidYaml)), Schema: evalTestSchema()}
	resp := resource.CreateResponse{State: tfsdk.State{Raw: plan.Raw, Schema: plan.Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.True(t, strings.HasPrefix(origin, "tf_"), "origin %q must carry the tf_ prefix", origin)

	var result evalModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, origin, result.Origin.ValueString())
	assert.Equal(t, "provider-default", result.Dataset.ValueString())
	assert.Equal(t, "eval_01abc", result.ID.ValueString())
}

func TestEvalResource_Create_ResolveErrorWarnsAndKeepsState(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateEval", mock.Anything, mock.Anything, mock.Anything, "default").Return(nil)
	mockClient.On("ResolveEval", mock.Anything, mock.Anything, "default").
		Return("", errors.New("list endpoint unavailable"))

	plan := tfsdk.Plan{Raw: evalValue(nil, nil, nil, strPtr(evalValidYaml)), Schema: evalTestSchema()}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: evalTestSchema()}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a resolve failure must not fail the apply")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Unable to resolve eval metadata")

	var result evalModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.ID.IsNull(), "id must be null so Read can self-heal it")
	assert.True(t, strings.HasPrefix(result.Origin.ValueString(), "tf_"))
}

func TestEvalResource_Create_ClientErrorWritesNoState(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateEval", mock.Anything, mock.Anything, mock.Anything, "default").
		Return(errors.New("400 bad request: duplicate metadata.name"))

	plan := tfsdk.Plan{Raw: evalValue(nil, nil, nil, strPtr(evalValidYaml)), Schema: evalTestSchema()}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: evalTestSchema()}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	assert.True(t, resp.State.Raw.IsNull())
	mockClient.AssertNotCalled(t, "ResolveEval", mock.Anything, mock.Anything, mock.Anything)
}

// Folder path and sharing are stored and returned, so an equivalent response
// that carries them must keep the user's YAML.
func TestEvalResource_Read_EquivalentKeepsUserYAML(t *testing.T) {
	result := readEval(t, evalValidYaml, evalAPIResponseJSON)
	assert.Equal(t, evalValidYaml, result.EvalYaml.ValueString())
}

func TestEvalResource_Read_DriftOverwritesState(t *testing.T) {
	for name, drifted := range map[string]string{
		"spec":        strings.Replace(evalAPIResponseJSON, `"rate":0.25`, `"rate":0.5`, 1),
		"folder path": strings.Replace(evalAPIResponseJSON, `"/quality"`, `"/other"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, evalAPIResponseJSON, drifted)
			result := readEval(t, evalValidYaml, drifted)
			assert.Equal(t, drifted, result.EvalYaml.ValueString())
		})
	}
}

func TestEvalResource_Read_TombstoneRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}
	mockClient.On("GetEval", mock.Anything, "tf_origin", "test-dataset").Return(evalTombstoneJSON, nil)

	state := evalState(strPtr("tf_origin"), strPtr("eval_01abc"), strPtr("test-dataset"), strPtr(evalValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared so the next apply re-creates the eval")
}

func TestEvalResource_Read_NotFoundRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}
	mockClient.On("GetEval", mock.Anything, "tf_origin", "test-dataset").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})
	mockClient.On("GetDataset", mock.Anything, "test-dataset").Return("{}", nil)

	state := evalState(strPtr("tf_origin"), strPtr("eval_01abc"), strPtr("test-dataset"), strPtr(evalValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a 404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared so the next apply re-creates the eval")
}

func TestEvalResource_Read_ServerErrorLeavesStateUnmutated(t *testing.T) {
	for name, err := range map[string]error{
		"500 server error":    &dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"},
		"401 unauthorized":    &dash0.APIError{StatusCode: 401, Status: "401 Unauthorized"},
		"plain network error": errors.New("connection refused"),
	} {
		t.Run(name, func(t *testing.T) {
			mockClient := &MockClient{}
			r := &EvalResource{client: mockClient}
			mockClient.On("GetEval", mock.Anything, "tf_origin", "test-dataset").Return("", err)

			state := evalState(strPtr("tf_origin"), strPtr("eval_01abc"), strPtr("test-dataset"), strPtr(evalValidYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			mockClient.AssertExpectations(t)
			require.True(t, resp.Diagnostics.HasError(), "transient errors must surface, not silently remove the resource")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
			assert.False(t, resp.State.Raw.IsNull(), "state must survive a transient failure")
		})
	}
}

// A transient resolve failure at Create leaves id null; the next refresh must
// resolve it rather than leave it null for the resource's lifetime.
func TestEvalResource_Read_NullIDSelfHeals(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}
	mockClient.On("GetEval", mock.Anything, "tf_origin", "test-dataset").Return(evalAPIResponseJSON, nil)
	mockClient.On("ResolveEval", mock.Anything, "tf_origin", "test-dataset").Return("eval_01healed", nil)

	state := evalState(strPtr("tf_origin"), nil, strPtr("test-dataset"), strPtr(evalValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())
	var result evalModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "eval_01healed", result.ID.ValueString())
}

// Update must not regenerate the origin, which would orphan the eval on the
// server, or re-resolve the immutable id.
func TestEvalResource_Update_CarriesOriginAndIDFromState(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}

	updatedYaml := strings.Replace(evalValidYaml, "threshold: 0.5", "threshold: 0.7", 1)
	require.NotEqual(t, evalValidYaml, updatedYaml)
	mockClient.On("UpdateEval", mock.Anything, "tf_origin", mock.Anything, "test-dataset").Return(nil)

	req := resource.UpdateRequest{
		State: evalState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(evalValidYaml)),
		// The framework leaves computed attributes unknown-or-null in the plan.
		Plan: tfsdk.Plan{Raw: evalValue(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)), Schema: evalTestSchema()},
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: evalTestSchema()}}
	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var result evalModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_origin", result.Origin.ValueString(), "origin must be carried from state, not regenerated")
	assert.Equal(t, "state-id", result.ID.ValueString(), "id must be carried from state, not re-resolved")
	assert.Equal(t, updatedYaml, result.EvalYaml.ValueString())
	mockClient.AssertNotCalled(t, "ResolveEval", mock.Anything, mock.Anything, mock.Anything)
}

func TestEvalResource_Update_ClientErrorDoesNotMutateState(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}

	updatedYaml := strings.Replace(evalValidYaml, "threshold: 0.5", "threshold: 0.7", 1)
	mockClient.On("UpdateEval", mock.Anything, "tf_origin", mock.Anything, "test-dataset").
		Return(errors.New("500 internal server error"))

	priorState := evalState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(evalValidYaml))
	req := resource.UpdateRequest{
		State: priorState,
		Plan:  tfsdk.Plan{Raw: evalValue(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)), Schema: evalTestSchema()},
	}
	resp := resource.UpdateResponse{State: priorState}
	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	var result evalModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, evalValidYaml, result.EvalYaml.ValueString(), "a failed update must leave the prior state untouched")
}

func TestEvalResource_Delete_NotFoundIsSuccess(t *testing.T) {
	mockClient := &MockClient{}
	r := &EvalResource{client: mockClient}
	mockClient.On("DeleteEval", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	state := evalState(strPtr("tf_origin"), strPtr("eval_01abc"), strPtr("test-dataset"), strPtr(evalValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
}

func TestEvalResource_ImportState(t *testing.T) {
	t.Run("adopts a live eval", func(t *testing.T) {
		mockClient := &MockClient{}
		r := &EvalResource{client: mockClient}
		mockClient.On("GetEval", mock.Anything, "tf_abc", "test-dataset").Return(evalAPIResponseJSON, nil)
		mockClient.On("ResolveEval", mock.Anything, "tf_abc", "test-dataset").Return("eval_01abc", nil)

		resp := &resource.ImportStateResponse{State: evalState(nil, nil, nil, nil)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: "test-dataset,tf_abc"}, resp)

		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		var result evalModel
		require.False(t, resp.State.Get(context.Background(), &result).HasError())
		assert.Equal(t, "tf_abc", result.Origin.ValueString())
		assert.Equal(t, "test-dataset", result.Dataset.ValueString())
		assert.Equal(t, "eval_01abc", result.ID.ValueString())
	})

	t.Run("rejects a single-part ID", func(t *testing.T) {
		mockClient := &MockClient{}
		r := &EvalResource{client: mockClient}

		resp := &resource.ImportStateResponse{State: evalState(nil, nil, nil, nil)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: "tf_abc"}, resp)

		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid Import ID")
		mockClient.AssertNotCalled(t, "GetEval", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("writes no partial state when the eval does not exist", func(t *testing.T) {
		mockClient := &MockClient{}
		r := &EvalResource{client: mockClient}
		mockClient.On("GetEval", mock.Anything, "tf_missing", "test-dataset").
			Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

		resp := &resource.ImportStateResponse{State: evalState(nil, nil, nil, nil)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: "test-dataset,tf_missing"}, resp)

		mockClient.AssertExpectations(t)
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Error Importing Eval")
		var result evalModel
		require.False(t, resp.State.Get(context.Background(), &result).HasError())
		assert.True(t, result.Origin.IsNull(), "no attribute may be written when the import target does not exist")
		assert.True(t, result.EvalYaml.IsNull())
		mockClient.AssertNotCalled(t, "ResolveEval", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("refuses a tombstone", func(t *testing.T) {
		mockClient := &MockClient{}
		r := &EvalResource{client: mockClient}
		mockClient.On("GetEval", mock.Anything, "tf_abc", "test-dataset").Return(evalTombstoneJSON, nil)

		resp := &resource.ImportStateResponse{State: evalState(nil, nil, nil, nil)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: "test-dataset,tf_abc"}, resp)

		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "has been deleted")
		mockClient.AssertNotCalled(t, "ResolveEval", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestEvalResource_ValidateConfig(t *testing.T) {
	valid := map[string]string{
		"user document":            evalValidYaml,
		"document copied from API": evalAPIResponseJSON,
		"disabled":                 strings.Replace(evalValidYaml, "enabled: true", "enabled: false", 1),
	}
	for name, evalYaml := range valid {
		t.Run(name, func(t *testing.T) {
			resp := evalValidate(t, evalYaml)
			assert.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		})
	}

	invalid := map[string]struct {
		yaml    string
		summary string
	}{
		"wrong kind": {
			strings.Replace(evalValidYaml, "Dash0GenerativeAiEvaluation", "Dash0SignalToMetrics", 1),
			"wrong kind",
		},
		"missing name": {
			strings.Replace(evalValidYaml, "  name: answer-relevance\n", "", 1),
			"metadata.name is missing",
		},
		"enabled absent": {
			strings.Replace(evalValidYaml, "  enabled: true\n", "", 1),
			"enabled",
		},
		"operation name filter with values": {
			strings.Replace(evalValidYaml, "operator: is_set", "operator: is_set\n        values: [chat]", 1),
			"gen_ai.operation.name",
		},
		"operation name filter missing": {
			strings.Replace(evalValidYaml, "gen_ai.operation.name", "service.name", 1),
			"gen_ai.operation.name",
		},
		"custom label": {
			strings.Replace(evalValidYaml, "  annotations:\n", "  labels:\n    team: ai\n  annotations:\n", 1),
			"metadata.labels",
		},
		"unknown annotation": {
			strings.Replace(evalValidYaml, "    dash0.com/folder-path: /quality\n", "    dash0.com/folder-path: /quality\n    owner: ai\n", 1),
			"metadata.annotations",
		},
	}
	for name, tc := range invalid {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, evalValidYaml, tc.yaml, "fixture must differ from the valid document")
			resp := evalValidate(t, tc.yaml)
			require.True(t, resp.Diagnostics.HasError())
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tc.summary)
		})
	}
}

// The API refuses to change metadata.name or the judge output kind, so the
// plan must replace the eval instead of showing an update that fails at apply.
func TestEvalResource_PlanRequiresReplaceOnImmutableChange(t *testing.T) {
	categorical := strings.Replace(evalValidYaml,
		"      kind: numeric\n      spec:\n        threshold: 0.5\n",
		"      kind: categorical\n      spec:\n        options:\n          - label: good\n", 1)

	for name, tc := range map[string]struct {
		state, config string
		replace       bool
	}{
		"name changes":               {evalValidYaml, strings.Replace(evalValidYaml, "name: answer-relevance", "name: answer-quality", 1), true},
		"output kind changes":        {evalValidYaml, categorical, true},
		"threshold changes":          {evalValidYaml, strings.Replace(evalValidYaml, "threshold: 0.5", "threshold: 0.7", 1), false},
		"display name changes":       {evalValidYaml, strings.Replace(evalValidYaml, "name: Answer relevance", "name: Relevance", 1), false},
		"state is the API JSON":      {evalAPIResponseJSON, evalValidYaml, false},
		"name changes from API JSON": {evalAPIResponseJSON, strings.Replace(evalValidYaml, "name: answer-relevance", "name: answer-quality", 1), true},
	} {
		t.Run(name, func(t *testing.T) {
			require.NotEqual(t, tc.state, tc.config)
			req := planmodifier.StringRequest{
				StateValue:  types.StringValue(tc.state),
				ConfigValue: types.StringValue(tc.config),
				PlanValue:   types.StringValue(tc.config),
			}
			resp := &stringplanmodifier.RequiresReplaceIfFuncResponse{}
			requiresReplaceOnImmutableEvalChange(context.Background(), req, resp)
			assert.Equal(t, tc.replace, resp.RequiresReplace)
		})
	}
}
