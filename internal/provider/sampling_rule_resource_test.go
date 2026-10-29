package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// --- Fixtures -------------------------------------------------------------

const samplingRuleValidYaml = `kind: Dash0Sampling
metadata:
  name: keep-errors
spec:
  enabled: true
  display:
    name: Keep error traces
  conditions:
    kind: error
    spec: {}
  rateLimit:
    rate: 1000
`

// samplingRuleAPIResponseJSON is what a GET returns for samplingRuleValidYaml:
// the same document with server-managed labels added.
const samplingRuleAPIResponseJSON = `{"kind":"Dash0Sampling","metadata":{"name":"keep-errors","labels":{"dash0.com/origin":"tf_origin","dash0.com/id":"11111111-1111-1111-1111-111111111111","dash0.com/dataset":"test-dataset","dash0.com/source":"terraform","dash0.com/version":"1"}},"spec":{"enabled":true,"display":{"name":"Keep error traces"},"conditions":{"kind":"error","spec":{}},"rateLimit":{"rate":1000}}}`

const samplingRuleYamlCustomLabels = `kind: Dash0Sampling
metadata:
  name: keep-errors
  labels:
    custom:
      team: platform
spec:
  enabled: true
  conditions:
    kind: error
    spec: {}
`

// samplingRuleYamlEnabledAbsent omits spec.enabled. ValidateConfig rejects it,
// and so do Create and Update, which protect a config whose YAML is unknown at
// plan time.
const samplingRuleYamlEnabledAbsent = `kind: Dash0Sampling
metadata:
  name: keep-errors
spec:
  conditions:
    kind: error
    spec: {}
`

// --- tfsdk plumbing helpers -----------------------------------------------

func samplingRuleTestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":             schema.StringAttribute{Computed: true},
			"id":                 schema.StringAttribute{Computed: true},
			"dataset":            schema.StringAttribute{Optional: true, Computed: true},
			"sampling_rule_yaml": schema.StringAttribute{Required: true},
		},
	}
}

func samplingRuleObjectType() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"origin":             tftypes.String,
			"id":                 tftypes.String,
			"dataset":            tftypes.String,
			"sampling_rule_yaml": tftypes.String,
		},
	}
}

// samplingRuleValue builds a raw tftypes object for the resource model. A nil
// pointer produces a null attribute.
func samplingRuleValue(origin, id, dataset, ruleYaml *string) tftypes.Value {
	str := func(p *string) tftypes.Value {
		if p == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *p)
	}
	return tftypes.NewValue(samplingRuleObjectType(), map[string]tftypes.Value{
		"origin":             str(origin),
		"id":                 str(id),
		"dataset":            str(dataset),
		"sampling_rule_yaml": str(ruleYaml),
	})
}

func samplingRuleState(origin, id, dataset, ruleYaml *string) tfsdk.State {
	return tfsdk.State{Raw: samplingRuleValue(origin, id, dataset, ruleYaml), Schema: samplingRuleTestSchema()}
}

func samplingRulePlan(origin, id, dataset, ruleYaml *string) tfsdk.Plan {
	return tfsdk.Plan{Raw: samplingRuleValue(origin, id, dataset, ruleYaml), Schema: samplingRuleTestSchema()}
}

func samplingRuleValidateConfigRequest(ruleYaml string) resource.ValidateConfigRequest {
	return resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: samplingRuleValue(nil, nil, nil, &ruleYaml), Schema: samplingRuleTestSchema()},
	}
}

// samplingRuleImportStateResponse builds an ImportStateResponse whose State is
// shaped the way the framework's ImportResourceState RPC does: every attribute
// null.
func samplingRuleImportStateResponse() *resource.ImportStateResponse {
	return &resource.ImportStateResponse{
		State: tfsdk.State{Raw: samplingRuleValue(nil, nil, nil, nil), Schema: samplingRuleTestSchema()},
	}
}

// --- Metadata / Schema / Configure ----------------------------------------

func TestSamplingRuleResource_Metadata(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)

	assert.Equal(t, "dash0_sampling_rule", resp.TypeName)
}

func TestSamplingRuleResource_Schema(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)
	assert.Len(t, resp.Schema.Attributes, 4, "schema must expose exactly origin, id, dataset, sampling_rule_yaml")

	assert.True(t, resp.Schema.Attributes["origin"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["id"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Optional)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["sampling_rule_yaml"].(schema.StringAttribute).Required)
}

func TestSamplingRuleResource_Configure(t *testing.T) {
	r := &SamplingRuleResource{}
	mockClient := &MockClient{}

	// nil provider data: no-op, no error (the framework calls Configure before
	// the provider itself is configured).
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)
	assert.Nil(t, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{
		ProviderData: resourceProviderData{client: mockClient, defaultDataset: "default"},
	}, resp)
	assert.Equal(t, mockClient, r.client)
	assert.Equal(t, "default", r.defaultDataset)
	assert.False(t, resp.Diagnostics.HasError())

	resp = &resource.ConfigureResponse{}
	assert.NotPanics(t, func() {
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "not-resourceProviderData"}, resp)
	})
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Unexpected Data Source Configure Type")
}

// --- Create ---------------------------------------------------------------

func TestSamplingRuleResource_Create_DefaultsDatasetFromProvider(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "provider-default"}

	mockClient.On("CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, "provider-default").Return(nil)
	mockClient.On("ResolveSamplingRule", mock.Anything, mock.Anything, "provider-default").
		Return("22222222-2222-2222-2222-222222222222", nil)

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, nil, strPtr(samplingRuleValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var state samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())

	assert.True(t, strings.HasPrefix(state.Origin.ValueString(), "tf_"), "origin must carry the tf_ prefix, got %q", state.Origin.ValueString())
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", state.ID.ValueString())
	assert.Equal(t, "provider-default", state.Dataset.ValueString())
	assert.Equal(t, samplingRuleValidYaml, state.SamplingRuleYaml.ValueString())
}

func TestSamplingRuleResource_Create_ExplicitDatasetWins(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "provider-default"}

	mockClient.On("CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, "explicit-dataset").Return(nil)
	mockClient.On("ResolveSamplingRule", mock.Anything, mock.Anything, "explicit-dataset").Return("an-id", nil)

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, strPtr("explicit-dataset"), strPtr(samplingRuleValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var state samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())
	assert.Equal(t, "explicit-dataset", state.Dataset.ValueString())
}

func TestSamplingRuleResource_Create_InvalidYAMLWritesNoState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "default"}

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, nil, strPtr("invalid: yaml: : :"))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
	assert.True(t, resp.State.Raw.IsNull(), "no state may be written when the configuration does not parse")
	mockClient.AssertNotCalled(t, "CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestSamplingRuleResource_Create_ResolveErrorWarnsAndKeepsState covers the
// best-effort id contract: a failing list endpoint must not fail an otherwise
// successful apply. Read's self-heal branch recovers the id later.
func TestSamplingRuleResource_Create_ResolveErrorWarnsAndKeepsState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, "default").Return(nil)
	mockClient.On("ResolveSamplingRule", mock.Anything, mock.Anything, "default").
		Return("", errors.New("list endpoint unavailable"))

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, nil, strPtr(samplingRuleValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a resolve failure must not fail the apply")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Unable to resolve sampling rule metadata")

	var state samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())
	assert.True(t, state.ID.IsNull(), "id must be null so Read can self-heal it")
	assert.True(t, strings.HasPrefix(state.Origin.ValueString(), "tf_"))
}

func TestSamplingRuleResource_Create_ClientErrorSurfaces(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, "default").
		Return(errors.New("403 forbidden: admin role required"))

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, nil, strPtr(samplingRuleValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	assert.True(t, resp.State.Raw.IsNull())
}

// --- Read -----------------------------------------------------------------

func TestSamplingRuleResource_Read_EquivalentKeepsUserFormatting(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("GetSamplingRule", mock.Anything, "tf_origin", "test-dataset").Return(samplingRuleAPIResponseJSON, nil)

	state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, samplingRuleValidYaml, result.SamplingRuleYaml.ValueString(),
		"an equivalent API response must not clobber the user's YAML formatting")
	// The id is already set, so the self-heal branch must not fire.
	mockClient.AssertNotCalled(t, "ResolveSamplingRule", mock.Anything, mock.Anything, mock.Anything)
}

func TestSamplingRuleResource_Read_DriftOverwritesState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	drifted := strings.Replace(samplingRuleAPIResponseJSON, `"rateLimit":{"rate":1000}`, `"rateLimit":{"rate":50}`, 1)
	require.NotEqual(t, samplingRuleAPIResponseJSON, drifted, "fixture must actually differ")

	mockClient.On("GetSamplingRule", mock.Anything, "tf_origin", "test-dataset").Return(drifted, nil)

	state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, drifted, result.SamplingRuleYaml.ValueString(),
		"a drifted API response must become the new source of truth")
}

// TestSamplingRuleResource_Read_NotFoundRemovesResource covers an out-of-band
// delete. Sampling rule deletes are hard, so a GET after one returns 404.
func TestSamplingRuleResource_Read_NotFoundRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("GetSamplingRule", mock.Anything, "tf_origin", "test-dataset").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})
	mockClient.On("GetDataset", mock.Anything, "test-dataset").Return("{}", nil)

	state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a 404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared so the next apply re-creates the rule")
}

func TestSamplingRuleResource_Read_ServerErrorLeavesStateUnmutated(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"500 server error", &dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"}},
		{"401 unauthorized", &dash0.APIError{StatusCode: 401, Status: "401 Unauthorized"}},
		{"plain network error", errors.New("connection refused")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockClient{}
			r := &SamplingRuleResource{client: mockClient}

			mockClient.On("GetSamplingRule", mock.Anything, "tf_origin", "test-dataset").Return("", tc.err)

			state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			mockClient.AssertExpectations(t)
			require.True(t, resp.Diagnostics.HasError(), "transient errors must surface, not silently remove the resource")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
			assert.False(t, resp.State.Raw.IsNull(), "state must survive a transient failure")
		})
	}
}

// TestSamplingRuleResource_Read_NullIDSelfHeals covers the recovery path for a
// transient resolve failure at Create: the next refresh must re-resolve rather
// than leave id null for the resource's lifetime.
func TestSamplingRuleResource_Read_NullIDSelfHeals(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("GetSamplingRule", mock.Anything, "tf_origin", "test-dataset").Return(samplingRuleAPIResponseJSON, nil)
	mockClient.On("ResolveSamplingRule", mock.Anything, "tf_origin", "test-dataset").
		Return("33333333-3333-3333-3333-333333333333", nil)

	state := samplingRuleState(strPtr("tf_origin"), nil, strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "33333333-3333-3333-3333-333333333333", result.ID.ValueString())
}

// --- Update ---------------------------------------------------------------

// TestSamplingRuleResource_Update_CarriesOriginAndIDFromState pins that Update
// does not regenerate the origin (which would orphan the server asset) or
// re-resolve the immutable id.
func TestSamplingRuleResource_Update_CarriesOriginAndIDFromState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	updatedYaml := strings.Replace(samplingRuleValidYaml, "rate: 1000", "rate: 50", 1)
	require.NotEqual(t, samplingRuleValidYaml, updatedYaml)

	mockClient.On("UpdateSamplingRule", mock.Anything, "tf_origin", mock.Anything, "test-dataset").Return(nil)

	req := resource.UpdateRequest{
		State: samplingRuleState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml)),
		// The framework leaves computed attributes unknown-or-null in the plan;
		// model that by passing them as null here.
		Plan: samplingRulePlan(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_origin", result.Origin.ValueString(), "origin must be carried from state, not regenerated")
	assert.Equal(t, "state-id", result.ID.ValueString(), "id must be carried from state, not re-resolved")
	assert.Equal(t, updatedYaml, result.SamplingRuleYaml.ValueString())
	mockClient.AssertNotCalled(t, "ResolveSamplingRule", mock.Anything, mock.Anything, mock.Anything)
}

func TestSamplingRuleResource_Update_ClientErrorDoesNotMutateState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	updatedYaml := strings.Replace(samplingRuleValidYaml, "rate: 1000", "rate: 50", 1)
	mockClient.On("UpdateSamplingRule", mock.Anything, "tf_origin", mock.Anything, "test-dataset").
		Return(errors.New("500 internal server error"))

	priorState := samplingRuleState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	req := resource.UpdateRequest{
		State: priorState,
		Plan:  samplingRulePlan(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)),
	}
	resp := resource.UpdateResponse{State: priorState}

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, samplingRuleValidYaml, result.SamplingRuleYaml.ValueString(),
		"a failed update must leave the prior state untouched")
}

// --- Delete ---------------------------------------------------------------

func TestSamplingRuleResource_Delete_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("DeleteSamplingRule", mock.Anything, "tf_origin", "test-dataset").Return(nil)

	state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
}

// TestSamplingRuleResource_Delete_NotFoundIsSuccess covers a destroy that
// follows an out-of-band delete: the rule is already gone, which is the
// desired end state, not an error.
func TestSamplingRuleResource_Delete_NotFoundIsSuccess(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("DeleteSamplingRule", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "an already-deleted rule is a successful destroy")
}

func TestSamplingRuleResource_Delete_ServerErrorSurfaces(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("DeleteSamplingRule", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"})

	state := samplingRuleState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError(), "the 404 short-circuit must not swallow a 5xx")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
}

// --- ImportState ----------------------------------------------------------

func TestSamplingRuleResource_ImportState_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("GetSamplingRule", mock.Anything, "tf_abc", "default").Return(samplingRuleAPIResponseJSON, nil)
	mockClient.On("ResolveSamplingRule", mock.Anything, "tf_abc", "default").Return("resolved-id", nil)

	resp := samplingRuleImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_abc"}, resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_abc", result.Origin.ValueString())
	assert.Equal(t, "default", result.Dataset.ValueString())
	assert.Equal(t, samplingRuleAPIResponseJSON, result.SamplingRuleYaml.ValueString())
	assert.Equal(t, "resolved-id", result.ID.ValueString())
}

func TestSamplingRuleResource_ImportState_SinglePartIDErrors(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	resp := samplingRuleImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "tf_abc"}, resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid Import ID")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "Expected import ID in the format 'dataset,origin'. Got: tf_abc")
	mockClient.AssertNotCalled(t, "GetSamplingRule", mock.Anything, mock.Anything, mock.Anything)
}

func TestSamplingRuleResource_ImportState_NotFoundWritesNoPartialState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	mockClient.On("GetSamplingRule", mock.Anything, "tf_missing", "default").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	resp := samplingRuleImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_missing"}, resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Error Importing Sampling Rule")

	var result samplingRuleModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.Origin.IsNull(), "no attribute may be written when the import target does not exist")
	assert.True(t, result.SamplingRuleYaml.IsNull())
	mockClient.AssertNotCalled(t, "ResolveSamplingRule", mock.Anything, mock.Anything, mock.Anything)
}

// --- ValidateConfig -------------------------------------------------------

func TestSamplingRuleResource_ValidateConfig_Valid(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(samplingRuleValidYaml), resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

func TestSamplingRuleResource_ValidateConfig_InvalidYAML(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest("invalid: yaml: ["), resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML in sampling_rule_yaml")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
	assert.Equal(t, "sampling_rule_yaml", withPath.Path().String())
}

func TestSamplingRuleResource_ValidateConfig_EmptyOrNonMapping(t *testing.T) {
	for name, input := range map[string]string{
		"empty":           "",
		"whitespace only": "   \n  ",
		"explicit null":   "null",
	} {
		t.Run(name, func(t *testing.T) {
			r := &SamplingRuleResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(input), resp)

			require.True(t, resp.Diagnostics.HasError(), "a document with no mapping must not reach apply")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "empty or not a YAML mapping")
		})
	}
}

// TestSamplingRuleResource_ValidateConfig_WrongKind includes the Dash0
// operator's Dash0SamplingRule kind, the likeliest copy-paste mistake.
func TestSamplingRuleResource_ValidateConfig_WrongKind(t *testing.T) {
	for _, kind := range []string{"Dash0SamplingRule", "Dash0View", ""} {
		t.Run(kind, func(t *testing.T) {
			r := &SamplingRuleResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(
				strings.Replace(samplingRuleValidYaml, "kind: Dash0Sampling\n", "kind: "+kind+"\n", 1)), resp)

			require.True(t, resp.Diagnostics.HasError())
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "kind")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "kind: Dash0Sampling")
		})
	}
}

func TestSamplingRuleResource_ValidateConfig_CustomLabelsWarn(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(samplingRuleYamlCustomLabels), resp)

	assert.False(t, resp.Diagnostics.HasError())
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels.custom")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "team",
		"the warning must name the offending key so the user can find it")

	withPath, ok := resp.Diagnostics.Warnings()[0].(diag.DiagnosticWithPath)
	require.True(t, ok)
	assert.Equal(t, "sampling_rule_yaml", withPath.Path().String())
}

// spec.enabled is required, not defaulted. SamplingSpec.Enabled is a
// non-pointer bool with no omitempty, so the provider always sends a value:
// accepting an omitted field would silently create a disabled rule, and a
// later apply would switch off one that had been enabled outside Terraform.
func TestSamplingRuleResource_ValidateConfig_EnabledAbsentErrors(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(samplingRuleYamlEnabledAbsent), resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach apply")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "sampling rule")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
	assert.Equal(t, "sampling_rule_yaml", withPath.Path().String())
}

// A bare `enabled:` is null, not absent: it is sent as false and normalization
// strips it, so it would create a disabled rule with a clean plan. A string is
// not a boolean either and would only fail at apply when decoded.
func TestSamplingRuleResource_ValidateConfig_EnabledNotBooleanErrors(t *testing.T) {
	for name, value := range map[string]string{
		"null":   "enabled:",
		"string": `enabled: "true"`,
	} {
		t.Run(name, func(t *testing.T) {
			r := &SamplingRuleResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(
				strings.Replace(samplingRuleValidYaml, "enabled: true", value, 1)), resp)

			require.True(t, resp.Diagnostics.HasError(), "a non-boolean spec.enabled must not reach apply")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
		})
	}
}

func TestSamplingRuleResource_ValidateConfig_EnabledFalseIsValid(t *testing.T) {
	r := &SamplingRuleResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), samplingRuleValidateConfigRequest(
		strings.Replace(samplingRuleValidYaml, "enabled: true", "enabled: false", 1)), resp)

	assert.False(t, resp.Diagnostics.HasError(), "an explicit spec.enabled: false is valid")
}

// The spec.enabled requirement must hold at apply time, not only at plan time.
// ValidateConfig returns early whenever sampling_rule_yaml is unknown, so
// Create and Update are the only places the invariant can still be enforced.
func TestSamplingRuleResource_Create_EnabledAbsentIsRejectedAtApply(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "default"}

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, nil, strPtr(samplingRuleYamlEnabledAbsent))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach the API")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
	assert.True(t, resp.State.Raw.IsNull(), "no state may be written when the invariant fails")
	mockClient.AssertNotCalled(t, "CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSamplingRuleResource_Update_EnabledAbsentIsRejectedAtApply(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}

	req := resource.UpdateRequest{
		State: samplingRuleState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml)),
		Plan:  samplingRulePlan(nil, nil, strPtr("test-dataset"), strPtr(samplingRuleYamlEnabledAbsent)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Update(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach the API")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
	mockClient.AssertNotCalled(t, "UpdateSamplingRule", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// The custom-labels warning has the same unknown-value blind spot as the hard
// errors, so Create and Update must emit it too.
func TestSamplingRuleResource_Create_CustomLabelsWarn(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient, defaultDataset: "default"}
	mockClient.On("CreateSamplingRule", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockClient.On("ResolveSamplingRule", mock.Anything, mock.Anything, mock.Anything).Return("an-id", nil)

	req := resource.CreateRequest{Plan: samplingRulePlan(nil, nil, nil, strPtr(samplingRuleYamlCustomLabels))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Create(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "custom labels are a warning, not an error")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount(), "the warning must survive the unknown-value path")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels.custom")
}

func TestSamplingRuleResource_Update_CustomLabelsWarn(t *testing.T) {
	mockClient := &MockClient{}
	r := &SamplingRuleResource{client: mockClient}
	mockClient.On("UpdateSamplingRule", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	req := resource.UpdateRequest{
		State: samplingRuleState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(samplingRuleValidYaml)),
		Plan:  samplingRulePlan(strPtr("tf_origin"), nil, strPtr("test-dataset"), strPtr(samplingRuleYamlCustomLabels)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: samplingRuleTestSchema()}}

	r.Update(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "custom labels are a warning, not an error")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount(), "the warning must survive the unknown-value path")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels.custom")
}
