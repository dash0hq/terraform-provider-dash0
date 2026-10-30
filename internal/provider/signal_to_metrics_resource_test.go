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

const signalToMetricsValidYaml = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-errors
spec:
  enabled: true
  display:
    name: Checkout errors
  match:
    signal: spans
    filters:
      - key: service.name
        operator: is
        value: checkout
  output:
    name: checkout_errors
    interval: 60s
`

// signalToMetricsAPIResponseJSON is what a GET returns for
// signalToMetricsValidYaml: the same document with server-managed labels and
// timestamp annotations added.
const signalToMetricsAPIResponseJSON = `{"kind":"Dash0SignalToMetrics","metadata":{"name":"checkout-errors","labels":{"dash0.com/origin":"tf_origin","dash0.com/id":"signal_to_metrics_01abc","dash0.com/dataset":"test-dataset","dash0.com/source":"terraform","dash0.com/version":"1"},"annotations":{"dash0.com/created-at":"2026-10-30T09:13:08.959631Z","dash0.com/updated-at":"2026-10-30T09:13:08.959631Z"}},"spec":{"enabled":true,"display":{"name":"Checkout errors"},"match":{"signal":"spans","filters":[{"key":"service.name","operator":"is","value":"checkout"}]},"output":{"name":"checkout_errors","interval":"60s"}}}`

// signalToMetricsTombstoneJSON is what a GET returns after a delete: the rule
// with a dash0.com/deleted-at annotation, not a 404.
var signalToMetricsTombstoneJSON = strings.Replace(signalToMetricsAPIResponseJSON,
	`"annotations":{`, `"annotations":{"dash0.com/deleted-at":"2026-10-30T09:13:52.138067Z",`, 1)

const signalToMetricsYamlCustomLabels = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-errors
  labels:
    custom:
      team: checkout
      cost-center: payments
spec:
  enabled: true
  match:
    signal: spans
    filters: []
  output:
    name: checkout_errors
    interval: 60s
`

// signalToMetricsYamlEnabledAbsent omits spec.enabled. ValidateConfig rejects
// it, and so do Create and Update, which protect a config whose YAML is
// unknown at plan time.
const signalToMetricsYamlEnabledAbsent = `kind: Dash0SignalToMetrics
metadata:
  name: checkout-errors
spec:
  match:
    signal: spans
    filters: []
  output:
    name: checkout_errors
    interval: 60s
`

// --- tfsdk plumbing helpers -----------------------------------------------

func signalToMetricsTestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":                 schema.StringAttribute{Computed: true},
			"id":                     schema.StringAttribute{Computed: true},
			"dataset":                schema.StringAttribute{Optional: true, Computed: true},
			"signal_to_metrics_yaml": schema.StringAttribute{Required: true},
		},
	}
}

func signalToMetricsObjectType() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"origin":                 tftypes.String,
			"id":                     tftypes.String,
			"dataset":                tftypes.String,
			"signal_to_metrics_yaml": tftypes.String,
		},
	}
}

// signalToMetricsValue builds a raw tftypes object for the resource model. A
// nil pointer produces a null attribute.
func signalToMetricsValue(origin, id, dataset, ruleYaml *string) tftypes.Value {
	str := func(p *string) tftypes.Value {
		if p == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *p)
	}
	return tftypes.NewValue(signalToMetricsObjectType(), map[string]tftypes.Value{
		"origin":                 str(origin),
		"id":                     str(id),
		"dataset":                str(dataset),
		"signal_to_metrics_yaml": str(ruleYaml),
	})
}

func signalToMetricsState(origin, id, dataset, ruleYaml *string) tfsdk.State {
	return tfsdk.State{Raw: signalToMetricsValue(origin, id, dataset, ruleYaml), Schema: signalToMetricsTestSchema()}
}

func signalToMetricsPlan(origin, id, dataset, ruleYaml *string) tfsdk.Plan {
	return tfsdk.Plan{Raw: signalToMetricsValue(origin, id, dataset, ruleYaml), Schema: signalToMetricsTestSchema()}
}

func signalToMetricsValidateConfigRequest(ruleYaml string) resource.ValidateConfigRequest {
	return resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: signalToMetricsValue(nil, nil, nil, &ruleYaml), Schema: signalToMetricsTestSchema()},
	}
}

// signalToMetricsImportStateResponse builds an ImportStateResponse whose State
// is shaped the way the framework's ImportResourceState RPC does: every
// attribute null.
func signalToMetricsImportStateResponse() *resource.ImportStateResponse {
	return &resource.ImportStateResponse{
		State: tfsdk.State{Raw: signalToMetricsValue(nil, nil, nil, nil), Schema: signalToMetricsTestSchema()},
	}
}

// --- Metadata / Schema / Configure ----------------------------------------

func TestSignalToMetricsResource_Metadata(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)

	assert.Equal(t, "dash0_signal_to_metrics", resp.TypeName)
}

func TestSignalToMetricsResource_Schema(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)
	assert.Len(t, resp.Schema.Attributes, 4, "schema must expose exactly origin, id, dataset, signal_to_metrics_yaml")

	assert.True(t, resp.Schema.Attributes["origin"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["id"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Optional)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["signal_to_metrics_yaml"].(schema.StringAttribute).Required)
}

func TestSignalToMetricsResource_Configure(t *testing.T) {
	r := &SignalToMetricsResource{}
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

func TestSignalToMetricsResource_Create_DefaultsDatasetFromProvider(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient, defaultDataset: "provider-default"}

	mockClient.On("CreateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, "provider-default").Return(nil)
	mockClient.On("ResolveSignalToMetrics", mock.Anything, mock.Anything, "provider-default").
		Return("signal_to_metrics_01resolved", nil)

	req := resource.CreateRequest{Plan: signalToMetricsPlan(nil, nil, nil, strPtr(signalToMetricsValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var state signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())

	assert.True(t, strings.HasPrefix(state.Origin.ValueString(), "tf_"), "origin must carry the tf_ prefix, got %q", state.Origin.ValueString())
	assert.Equal(t, "signal_to_metrics_01resolved", state.ID.ValueString())
	assert.Equal(t, "provider-default", state.Dataset.ValueString())
	assert.Equal(t, signalToMetricsValidYaml, state.SignalToMetricsYaml.ValueString())
}

func TestSignalToMetricsResource_Create_ExplicitDatasetWins(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient, defaultDataset: "provider-default"}

	mockClient.On("CreateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, "explicit-dataset").Return(nil)
	mockClient.On("ResolveSignalToMetrics", mock.Anything, mock.Anything, "explicit-dataset").Return("an-id", nil)

	req := resource.CreateRequest{Plan: signalToMetricsPlan(nil, nil, strPtr("explicit-dataset"), strPtr(signalToMetricsValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var state signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())
	assert.Equal(t, "explicit-dataset", state.Dataset.ValueString())
}

func TestSignalToMetricsResource_Create_InvalidYAMLWritesNoState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient, defaultDataset: "default"}

	req := resource.CreateRequest{Plan: signalToMetricsPlan(nil, nil, nil, strPtr("invalid: yaml: : :"))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

	r.Create(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
	assert.True(t, resp.State.Raw.IsNull(), "no state may be written when the configuration does not parse")
	mockClient.AssertNotCalled(t, "CreateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestSignalToMetricsResource_Create_ResolveErrorWarnsAndKeepsState covers the
// best-effort id contract: a failing list endpoint must not fail an otherwise
// successful apply. Read's self-heal branch recovers the id later.
func TestSignalToMetricsResource_Create_ResolveErrorWarnsAndKeepsState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, "default").Return(nil)
	mockClient.On("ResolveSignalToMetrics", mock.Anything, mock.Anything, "default").
		Return("", errors.New("list endpoint unavailable"))

	req := resource.CreateRequest{Plan: signalToMetricsPlan(nil, nil, nil, strPtr(signalToMetricsValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a resolve failure must not fail the apply")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Unable to resolve signal-to-metrics rule metadata")

	var state signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())
	assert.True(t, state.ID.IsNull(), "id must be null so Read can self-heal it")
	assert.True(t, strings.HasPrefix(state.Origin.ValueString(), "tf_"))
}

func TestSignalToMetricsResource_Create_ClientErrorSurfaces(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, "default").
		Return(errors.New("400 bad request: invalid OTTL expression"))

	req := resource.CreateRequest{Plan: signalToMetricsPlan(nil, nil, nil, strPtr(signalToMetricsValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	assert.True(t, resp.State.Raw.IsNull())
}

// --- Read -----------------------------------------------------------------

func TestSignalToMetricsResource_Read_EquivalentKeepsUserFormatting(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return(signalToMetricsAPIResponseJSON, nil)

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, signalToMetricsValidYaml, result.SignalToMetricsYaml.ValueString(),
		"an equivalent API response must not clobber the user's YAML formatting")
	// The id is already set, so the self-heal branch must not fire.
	mockClient.AssertNotCalled(t, "ResolveSignalToMetrics", mock.Anything, mock.Anything, mock.Anything)
}

func TestSignalToMetricsResource_Read_DriftOverwritesState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	drifted := strings.Replace(signalToMetricsAPIResponseJSON, `"name":"checkout_errors"`, `"name":"checkout_failures"`, 1)
	require.NotEqual(t, signalToMetricsAPIResponseJSON, drifted, "fixture must actually differ")

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return(drifted, nil)

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, drifted, result.SignalToMetricsYaml.ValueString(),
		"a drifted API response must become the new source of truth")
}

// TestSignalToMetricsResource_Read_TombstoneRemovesResource covers an
// out-of-band delete. Deletes are soft, so a GET after one returns 200 with a
// dash0.com/deleted-at annotation rather than a 404.
func TestSignalToMetricsResource_Read_TombstoneRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return(signalToMetricsTombstoneJSON, nil)

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a tombstone must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared so the next apply re-creates the rule")
}

// TestSignalToMetricsResource_Read_NotFoundRemovesResource covers a rule that
// is gone entirely (for example purged), in a dataset that still exists.
func TestSignalToMetricsResource_Read_NotFoundRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})
	mockClient.On("GetDataset", mock.Anything, "test-dataset").Return("{}", nil)

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a 404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared so the next apply re-creates the rule")
}

// TestSignalToMetricsResource_Read_DeletedDatasetKeepsState covers a rule whose
// dataset was deleted: the GET answers 404, but the rule comes back when the
// dataset is restored, so dropping it from state would create a duplicate.
func TestSignalToMetricsResource_Read_DeletedDatasetKeepsState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})
	mockClient.On("GetDataset", mock.Anything, "test-dataset").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Dataset is deleted")
	assert.False(t, resp.State.Raw.IsNull(), "the rule must stay in state while its dataset is deleted")
}

func TestSignalToMetricsResource_Read_ServerErrorLeavesStateUnmutated(t *testing.T) {
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
			r := &SignalToMetricsResource{client: mockClient}

			mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return("", tc.err)

			state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			mockClient.AssertExpectations(t)
			require.True(t, resp.Diagnostics.HasError(), "transient errors must surface, not silently remove the resource")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
			assert.False(t, resp.State.Raw.IsNull(), "state must survive a transient failure")
		})
	}
}

// TestSignalToMetricsResource_Read_NullIDSelfHeals covers the recovery path for
// a transient resolve failure at Create: the next refresh must re-resolve
// rather than leave id null for the resource's lifetime.
func TestSignalToMetricsResource_Read_NullIDSelfHeals(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return(signalToMetricsAPIResponseJSON, nil)
	mockClient.On("ResolveSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").
		Return("signal_to_metrics_01healed", nil)

	state := signalToMetricsState(strPtr("tf_origin"), nil, strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "signal_to_metrics_01healed", result.ID.ValueString())
}

// --- Update ---------------------------------------------------------------

// TestSignalToMetricsResource_Update_CarriesOriginAndIDFromState pins that
// Update does not regenerate the origin (which would orphan the server asset)
// or re-resolve the immutable id.
func TestSignalToMetricsResource_Update_CarriesOriginAndIDFromState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	updatedYaml := strings.Replace(signalToMetricsValidYaml, "interval: 60s", "interval: 5m", 1)
	require.NotEqual(t, signalToMetricsValidYaml, updatedYaml)

	mockClient.On("UpdateSignalToMetrics", mock.Anything, "tf_origin", mock.Anything, "test-dataset").Return(nil)

	req := resource.UpdateRequest{
		State: signalToMetricsState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml)),
		// The framework leaves computed attributes unknown-or-null in the plan;
		// model that by passing them as null here.
		Plan: signalToMetricsPlan(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_origin", result.Origin.ValueString(), "origin must be carried from state, not regenerated")
	assert.Equal(t, "state-id", result.ID.ValueString(), "id must be carried from state, not re-resolved")
	assert.Equal(t, updatedYaml, result.SignalToMetricsYaml.ValueString())
	mockClient.AssertNotCalled(t, "ResolveSignalToMetrics", mock.Anything, mock.Anything, mock.Anything)
}

func TestSignalToMetricsResource_Update_ClientErrorDoesNotMutateState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	updatedYaml := strings.Replace(signalToMetricsValidYaml, "interval: 60s", "interval: 5m", 1)
	mockClient.On("UpdateSignalToMetrics", mock.Anything, "tf_origin", mock.Anything, "test-dataset").
		Return(errors.New("500 internal server error"))

	priorState := signalToMetricsState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	req := resource.UpdateRequest{
		State: priorState,
		Plan:  signalToMetricsPlan(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)),
	}
	resp := resource.UpdateResponse{State: priorState}

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, signalToMetricsValidYaml, result.SignalToMetricsYaml.ValueString(),
		"a failed update must leave the prior state untouched")
}

// --- Delete ---------------------------------------------------------------

func TestSignalToMetricsResource_Delete_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("DeleteSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return(nil)

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
}

// TestSignalToMetricsResource_Delete_NotFoundIsSuccess covers a destroy that
// follows an out-of-band delete: deleting a tombstoned rule answers 404, and
// the rule being gone is the desired end state, not an error.
func TestSignalToMetricsResource_Delete_NotFoundIsSuccess(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("DeleteSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "an already-deleted rule is a successful destroy")
}

func TestSignalToMetricsResource_Delete_ServerErrorSurfaces(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("DeleteSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"})

	state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError(), "the 404 short-circuit must not swallow a 5xx")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
}

// --- ImportState ----------------------------------------------------------

func TestSignalToMetricsResource_ImportState_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_abc", "test-dataset").Return(signalToMetricsAPIResponseJSON, nil)
	mockClient.On("ResolveSignalToMetrics", mock.Anything, "tf_abc", "test-dataset").Return("resolved-id", nil)

	resp := signalToMetricsImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "test-dataset,tf_abc"}, resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_abc", result.Origin.ValueString())
	assert.Equal(t, "test-dataset", result.Dataset.ValueString())
	assert.Equal(t, signalToMetricsAPIResponseJSON, result.SignalToMetricsYaml.ValueString())
	assert.Equal(t, "resolved-id", result.ID.ValueString())
}

func TestSignalToMetricsResource_ImportState_SinglePartIDErrors(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	resp := signalToMetricsImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "tf_abc"}, resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid Import ID")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "Expected import ID in the format 'dataset,origin'. Got: tf_abc")
	mockClient.AssertNotCalled(t, "GetSignalToMetrics", mock.Anything, mock.Anything, mock.Anything)
}

func TestSignalToMetricsResource_ImportState_NotFoundWritesNoPartialState(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_missing", "default").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	resp := signalToMetricsImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_missing"}, resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Error Importing Signal-to-Metrics Rule")

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.Origin.IsNull(), "no attribute may be written when the import target does not exist")
	assert.True(t, result.SignalToMetricsYaml.IsNull())
	mockClient.AssertNotCalled(t, "ResolveSignalToMetrics", mock.Anything, mock.Anything, mock.Anything)
}

// TestSignalToMetricsResource_ImportState_TombstoneRefused pins the soft-delete
// guard: a GET succeeds against a deleted rule, and adopting the tombstone
// would leave state pointing at a rule that produces no metrics.
func TestSignalToMetricsResource_ImportState_TombstoneRefused(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_abc", "test-dataset").Return(signalToMetricsTombstoneJSON, nil)

	resp := signalToMetricsImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "test-dataset,tf_abc"}, resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "has been deleted")

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.Origin.IsNull(), "no attribute may be written for a tombstone")
	mockClient.AssertNotCalled(t, "ResolveSignalToMetrics", mock.Anything, mock.Anything, mock.Anything)
}

// TestSignalToMetricsResource_ImportState_WrongDatasetRefused pins the dataset
// guard. A GET by origin finds the rule whatever dataset the request names, but
// a PUT moves the rule into the dataset it names, so an import under the wrong
// dataset would silently move the rule on the next apply.
func TestSignalToMetricsResource_ImportState_WrongDatasetRefused(t *testing.T) {
	mockClient := &MockClient{}
	r := &SignalToMetricsResource{client: mockClient}

	// signalToMetricsAPIResponseJSON carries dash0.com/dataset: test-dataset.
	mockClient.On("GetSignalToMetrics", mock.Anything, "tf_abc", "default").Return(signalToMetricsAPIResponseJSON, nil)

	resp := signalToMetricsImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_abc"}, resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), `belongs to dataset "test-dataset", not "default"`)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "test-dataset,tf_abc", "the error must name the import ID to use")

	var result signalToMetricsModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.Origin.IsNull(), "no attribute may be written for a rule in another dataset")
}

// --- ValidateConfig -------------------------------------------------------

func TestSignalToMetricsResource_ValidateConfig_Valid(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(signalToMetricsValidYaml), resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

// A document copied from the API carries server-managed labels and timestamp
// annotations; it must validate, since that is the documented import flow.
func TestSignalToMetricsResource_ValidateConfig_APIDocumentIsValid(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(signalToMetricsAPIResponseJSON), resp)

	assert.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

func TestSignalToMetricsResource_ValidateConfig_InvalidYAML(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest("invalid: yaml: ["), resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML in signal_to_metrics_yaml")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
	assert.Equal(t, "signal_to_metrics_yaml", withPath.Path().String())
}

func TestSignalToMetricsResource_ValidateConfig_EmptyOrNonMapping(t *testing.T) {
	for name, input := range map[string]string{
		"empty":           "",
		"whitespace only": "   \n  ",
		"explicit null":   "null",
	} {
		t.Run(name, func(t *testing.T) {
			r := &SignalToMetricsResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(input), resp)

			require.True(t, resp.Diagnostics.HasError(), "a document with no mapping must not reach apply")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "empty or not a YAML mapping")
		})
	}
}

func TestSignalToMetricsResource_ValidateConfig_WrongKind(t *testing.T) {
	for _, kind := range []string{"Dash0SignalToMetricsRule", "Dash0Sampling", ""} {
		t.Run(kind, func(t *testing.T) {
			r := &SignalToMetricsResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(
				strings.Replace(signalToMetricsValidYaml, "kind: Dash0SignalToMetrics\n", "kind: "+kind+"\n", 1)), resp)

			require.True(t, resp.Diagnostics.HasError())
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "kind")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "kind: Dash0SignalToMetrics")
		})
	}
}

// metadata.labels.custom is rejected, not warned about: SignalToMetricsLabels
// has no custom field, so the labels would never reach the API.
func TestSignalToMetricsResource_ValidateConfig_CustomLabelsError(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(signalToMetricsYamlCustomLabels), resp)

	require.True(t, resp.Diagnostics.HasError(), "custom labels would be dropped, so they must not reach apply")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "metadata.labels.custom")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "cost-center, team",
		"the error must name the offending keys, sorted, so the user can find them")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok)
	assert.Equal(t, "signal_to_metrics_yaml", withPath.Path().String())
}

func TestSignalToMetricsResource_ValidateConfig_EmptyCustomLabelsAreValid(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(
		strings.Replace(signalToMetricsValidYaml, "  name: checkout-errors\n", "  name: checkout-errors\n  labels:\n    custom: {}\n", 1)), resp)

	assert.False(t, resp.Diagnostics.HasError(), "an empty custom map loses nothing")
}

// spec.enabled is required, not defaulted. SignalToMetricsSpec.Enabled is a
// non-pointer bool with no omitempty, so the provider always sends a value:
// accepting an omitted field would silently create a disabled rule, and a
// later apply would switch off one that had been enabled outside Terraform.
func TestSignalToMetricsResource_ValidateConfig_EnabledAbsentErrors(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(signalToMetricsYamlEnabledAbsent), resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach apply")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "signal to metrics rule")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
	assert.Equal(t, "signal_to_metrics_yaml", withPath.Path().String())
}

// A bare `enabled:` is null, not absent: it is sent as false and normalization
// strips it, so it would create a disabled rule with a clean plan. A string is
// not a boolean either and would only fail at apply when decoded.
func TestSignalToMetricsResource_ValidateConfig_EnabledNotBooleanErrors(t *testing.T) {
	for name, value := range map[string]string{
		"null":   "enabled:",
		"string": `enabled: "true"`,
	} {
		t.Run(name, func(t *testing.T) {
			r := &SignalToMetricsResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(
				strings.Replace(signalToMetricsValidYaml, "enabled: true", value, 1)), resp)

			require.True(t, resp.Diagnostics.HasError(), "a non-boolean spec.enabled must not reach apply")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
		})
	}
}

func TestSignalToMetricsResource_ValidateConfig_EnabledFalseIsValid(t *testing.T) {
	r := &SignalToMetricsResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), signalToMetricsValidateConfigRequest(
		strings.Replace(signalToMetricsValidYaml, "enabled: true", "enabled: false", 1)), resp)

	assert.False(t, resp.Diagnostics.HasError(), "an explicit spec.enabled: false is valid")
}

// The spec.enabled and custom-label checks must hold at apply time, not only
// at plan time. ValidateConfig returns early whenever signal_to_metrics_yaml is
// unknown, so Create and Update are the only places they can still be enforced.
func TestSignalToMetricsResource_Create_InvalidDocumentIsRejectedAtApply(t *testing.T) {
	for name, document := range map[string]string{
		"spec.enabled absent":    signalToMetricsYamlEnabledAbsent,
		"metadata.labels.custom": signalToMetricsYamlCustomLabels,
	} {
		t.Run(name, func(t *testing.T) {
			mockClient := &MockClient{}
			r := &SignalToMetricsResource{client: mockClient, defaultDataset: "default"}

			req := resource.CreateRequest{Plan: signalToMetricsPlan(nil, nil, nil, strPtr(document))}
			resp := resource.CreateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

			r.Create(context.Background(), req, &resp)

			require.True(t, resp.Diagnostics.HasError(), "an invalid document must not reach the API")
			assert.True(t, resp.State.Raw.IsNull(), "no state may be written when the invariant fails")
			mockClient.AssertNotCalled(t, "CreateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestSignalToMetricsResource_Update_InvalidDocumentIsRejectedAtApply(t *testing.T) {
	for name, document := range map[string]string{
		"spec.enabled absent":    signalToMetricsYamlEnabledAbsent,
		"metadata.labels.custom": signalToMetricsYamlCustomLabels,
	} {
		t.Run(name, func(t *testing.T) {
			mockClient := &MockClient{}
			r := &SignalToMetricsResource{client: mockClient}

			req := resource.UpdateRequest{
				State: signalToMetricsState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(signalToMetricsValidYaml)),
				Plan:  signalToMetricsPlan(nil, nil, strPtr("test-dataset"), strPtr(document)),
			}
			resp := resource.UpdateResponse{State: tfsdk.State{Schema: signalToMetricsTestSchema()}}

			r.Update(context.Background(), req, &resp)

			require.True(t, resp.Diagnostics.HasError(), "an invalid document must not reach the API")
			mockClient.AssertNotCalled(t, "UpdateSignalToMetrics", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}
