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
//
// The YAML below is verified to survive a round trip through the typed
// dash0.TimeSeriesAggregationDefinition: matchers use the `operator` key (with
// values such as `is`), not `type`/`equals`. A wrong key would be silently
// dropped by the typed round trip rather than rejected, producing a test that
// passes for the wrong reason.

const tsaValidYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: http-server-request-duration-rollup
spec:
  enabled: true
  display:
    name: HTTP server request duration rollup
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`

// tsaAPIResponseJSON is what a live GET returns for tsaValidYaml: the same
// document with server-managed labels added and spec.priority defaulted.
const tsaAPIResponseJSON = `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"http-server-request-duration-rollup","labels":{"dash0.com/origin":"tf_origin","dash0.com/id":"11111111-1111-1111-1111-111111111111","dash0.com/dataset":"test-dataset","dash0.com/source":"terraform","dash0.com/version":"1"}},"spec":{"enabled":true,"display":{"name":"HTTP server request duration rollup"},"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"5m"},"priority":2}}`

// tsaTombstoneJSON is what a GET by origin returns after a DELETE: HTTP 200
// with a dash0.com/deleted-at annotation. Time series aggregation deletes are
// soft, so this document — not a 404 — is the out-of-band-delete signal.
const tsaTombstoneJSON = `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"http-server-request-duration-rollup","annotations":{"dash0.com/deleted-at":"2026-09-14T10:11:12Z"},"labels":{"dash0.com/origin":"tf_origin"}},"spec":{"enabled":true,"match":{"metricNameMatcher":{"operator":"is","value":"http.server.request.duration"}},"sample":{"interval":"5m"}}}`

// --- tfsdk plumbing helpers -----------------------------------------------

func tsaTestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":                       schema.StringAttribute{Computed: true},
			"id":                           schema.StringAttribute{Computed: true},
			"dataset":                      schema.StringAttribute{Optional: true, Computed: true},
			"time_series_aggregation_yaml": schema.StringAttribute{Required: true},
		},
	}
}

func tsaObjectType() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"origin":                       tftypes.String,
			"id":                           tftypes.String,
			"dataset":                      tftypes.String,
			"time_series_aggregation_yaml": tftypes.String,
		},
	}
}

// tsaValue builds a raw tftypes object for the resource model. A nil pointer
// produces a null attribute.
func tsaValue(origin, id, dataset, tsaYaml *string) tftypes.Value {
	str := func(p *string) tftypes.Value {
		if p == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *p)
	}
	return tftypes.NewValue(tsaObjectType(), map[string]tftypes.Value{
		"origin":                       str(origin),
		"id":                           str(id),
		"dataset":                      str(dataset),
		"time_series_aggregation_yaml": str(tsaYaml),
	})
}

func tsaState(origin, id, dataset, tsaYaml *string) tfsdk.State {
	return tfsdk.State{Raw: tsaValue(origin, id, dataset, tsaYaml), Schema: tsaTestSchema()}
}

func tsaPlan(origin, id, dataset, tsaYaml *string) tfsdk.Plan {
	return tfsdk.Plan{Raw: tsaValue(origin, id, dataset, tsaYaml), Schema: tsaTestSchema()}
}

func tsaValidateConfigRequest(tsaYaml string) resource.ValidateConfigRequest {
	return resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: tsaValue(nil, nil, nil, &tsaYaml), Schema: tsaTestSchema()},
	}
}

// tsaImportStateResponse builds an ImportStateResponse whose State is shaped
// the way the framework's ImportResourceState RPC does: every attribute null.
func tsaImportStateResponse() *resource.ImportStateResponse {
	return &resource.ImportStateResponse{
		State: tfsdk.State{Raw: tsaValue(nil, nil, nil, nil), Schema: tsaTestSchema()},
	}
}

// --- Metadata / Schema / Configure ----------------------------------------

func TestTimeSeriesAggregationResource_Metadata(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)

	assert.Equal(t, "dash0_time_series_aggregation", resp.TypeName)
}

// TestTimeSeriesAggregationResource_Schema pins the attribute set. The `url`
// assertion is deliberate: time series aggregations have no deeplink asset
// type, and this resource was derived from view_resource.go, which does have
// one. A copy-paste regression would show up here.
func TestTimeSeriesAggregationResource_Schema(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)

	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "id")
	assert.Contains(t, resp.Schema.Attributes, "dataset")
	assert.Contains(t, resp.Schema.Attributes, "time_series_aggregation_yaml")
	assert.NotContains(t, resp.Schema.Attributes, "url",
		"time series aggregations have no deeplink; a url attribute would be a copy-paste regression from view_resource.go")
	assert.Len(t, resp.Schema.Attributes, 4, "schema must expose exactly origin, id, dataset, time_series_aggregation_yaml")

	assert.True(t, resp.Schema.Attributes["origin"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["id"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Optional)
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["time_series_aggregation_yaml"].(schema.StringAttribute).Required)
}

func TestTimeSeriesAggregationResource_Configure(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	mockClient := &MockClient{}

	// nil provider data: no-op, no error (the framework calls Configure before
	// the provider itself is configured).
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)
	assert.Nil(t, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// Valid provider data.
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{
		ProviderData: resourceProviderData{client: mockClient, defaultDataset: "default"},
	}, resp)
	assert.Equal(t, mockClient, r.client)
	assert.Equal(t, "default", r.defaultDataset)
	assert.False(t, resp.Diagnostics.HasError())

	// Wrong provider data type: a diagnostic, not a panic.
	resp = &resource.ConfigureResponse{}
	assert.NotPanics(t, func() {
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "not-resourceProviderData"}, resp)
	})
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Unexpected Data Source Configure Type")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "string")
}

// --- Create ---------------------------------------------------------------

func TestTimeSeriesAggregationResource_Create_DefaultsDatasetFromProvider(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "provider-default"}

	mockClient.On("CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, "provider-default").Return(nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, "provider-default").
		Return("22222222-2222-2222-2222-222222222222", nil)

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr(tsaValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var state timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())

	assert.True(t, strings.HasPrefix(state.Origin.ValueString(), "tf_"), "origin must carry the tf_ prefix, got %q", state.Origin.ValueString())
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", state.ID.ValueString())
	assert.Equal(t, "provider-default", state.Dataset.ValueString())
	assert.Equal(t, tsaValidYaml, state.TimeSeriesAggregationYaml.ValueString())
}

func TestTimeSeriesAggregationResource_Create_ExplicitDatasetWins(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "provider-default"}

	mockClient.On("CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, "explicit-dataset").Return(nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, "explicit-dataset").Return("an-id", nil)

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, strPtr("explicit-dataset"), strPtr(tsaValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var state timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())
	assert.Equal(t, "explicit-dataset", state.Dataset.ValueString())
}

func TestTimeSeriesAggregationResource_Create_InvalidYAMLWritesNoState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "default"}

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr("invalid: yaml: : :"))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
	assert.True(t, resp.State.Raw.IsNull(), "no state may be written when the configuration does not parse")
	mockClient.AssertNotCalled(t, "CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestTimeSeriesAggregationResource_Create_ResolveErrorWarnsAndKeepsState
// covers the best-effort id contract: a failing list endpoint must not fail an
// otherwise successful apply. Read's self-heal branch recovers the id later.
func TestTimeSeriesAggregationResource_Create_ResolveErrorWarnsAndKeepsState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, "default").Return(nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, "default").
		Return("", errors.New("list endpoint unavailable"))

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr(tsaValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a resolve failure must not fail the apply")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Unable to resolve time series aggregation metadata")

	var state timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &state).HasError())
	assert.True(t, state.ID.IsNull(), "id must be null so Read can self-heal it")
	assert.True(t, strings.HasPrefix(state.Origin.ValueString(), "tf_"))
	assert.Equal(t, "default", state.Dataset.ValueString())
	assert.Equal(t, tsaValidYaml, state.TimeSeriesAggregationYaml.ValueString())
}

func TestTimeSeriesAggregationResource_Create_ClientErrorSurfaces(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "default"}

	mockClient.On("CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, "default").
		Return(errors.New("403 forbidden: organization-admin role required"))

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr(tsaValidYaml))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	assert.True(t, resp.State.Raw.IsNull())
}

// --- Read -----------------------------------------------------------------

func TestTimeSeriesAggregationResource_Read_EquivalentKeepsUserFormatting(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").Return(tsaAPIResponseJSON, nil)

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, tsaValidYaml, result.TimeSeriesAggregationYaml.ValueString(),
		"an equivalent API response must not clobber the user's YAML formatting")
	// The id is already set, so the self-heal branch must not fire.
	mockClient.AssertNotCalled(t, "ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything)
}

func TestTimeSeriesAggregationResource_Read_DriftOverwritesState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	drifted := strings.Replace(tsaAPIResponseJSON, `"interval":"5m"`, `"interval":"30m"`, 1)
	require.NotEqual(t, tsaAPIResponseJSON, drifted, "fixture must actually differ")

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").Return(drifted, nil)

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, drifted, result.TimeSeriesAggregationYaml.ValueString(),
		"a drifted API response must become the new source of truth")
}

func TestTimeSeriesAggregationResource_Read_NotFoundRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a 404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared so the next apply re-creates the aggregation")
}

// TestTimeSeriesAggregationResource_Read_SoftDeleteTombstoneRemovesResource is
// the single most important test in this file. Time series aggregation deletes
// are soft: after a DELETE, a GET by origin returns 200 with a
// metadata.annotations["dash0.com/deleted-at"] tombstone, never a 404. A Read
// branch keyed only on IsNotFound would never fire, Terraform would believe the
// resource still exists, and an out-of-band delete would wedge every subsequent
// plan.
func TestTimeSeriesAggregationResource_Read_SoftDeleteTombstoneRemovesResource(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").Return(tsaTombstoneJSON, nil)

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "a soft-delete tombstone must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(),
		"a 200 carrying dash0.com/deleted-at means the aggregation is gone; state must be cleared")
}

func TestTimeSeriesAggregationResource_Read_ServerErrorLeavesStateUnmutated(t *testing.T) {
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
			r := &TimeSeriesAggregationResource{client: mockClient}

			mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").Return("", tc.err)

			state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

			mockClient.AssertExpectations(t)
			require.True(t, resp.Diagnostics.HasError(), "transient errors must surface, not silently remove the resource")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
			assert.False(t, resp.State.Raw.IsNull(), "state must survive a transient failure")

			var result timeSeriesAggregationModel
			require.False(t, resp.State.Get(context.Background(), &result).HasError())
			assert.Equal(t, tsaValidYaml, result.TimeSeriesAggregationYaml.ValueString())
		})
	}
}

// TestTimeSeriesAggregationResource_Read_NullIDSelfHeals covers the recovery
// path for a transient resolve failure at Create: the next refresh must
// re-resolve rather than leave id null for the resource's lifetime.
func TestTimeSeriesAggregationResource_Read_NullIDSelfHeals(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").Return(tsaAPIResponseJSON, nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").
		Return("33333333-3333-3333-3333-333333333333", nil)

	state := tsaState(strPtr("tf_origin"), nil, strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "33333333-3333-3333-3333-333333333333", result.ID.ValueString())
}

// --- Update ---------------------------------------------------------------

// TestTimeSeriesAggregationResource_Update_CarriesOriginAndIDFromState pins
// that Update does not regenerate the origin (which would orphan the server
// asset) or re-resolve the immutable id.
func TestTimeSeriesAggregationResource_Update_CarriesOriginAndIDFromState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	updatedYaml := strings.Replace(tsaValidYaml, "interval: 5m", "interval: 30m", 1)
	require.NotEqual(t, tsaValidYaml, updatedYaml)

	mockClient.On("UpdateTimeSeriesAggregation", mock.Anything, "tf_origin", mock.Anything, "test-dataset").Return(nil)

	req := resource.UpdateRequest{
		State: tsaState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(tsaValidYaml)),
		// The framework leaves computed attributes unknown-or-null in the plan;
		// model that by passing them as null here.
		Plan: tsaPlan(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_origin", result.Origin.ValueString(), "origin must be carried from state, not regenerated")
	assert.Equal(t, "state-id", result.ID.ValueString(), "id must be carried from state, not re-resolved")
	assert.Equal(t, updatedYaml, result.TimeSeriesAggregationYaml.ValueString())
	mockClient.AssertNotCalled(t, "ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything)
}

func TestTimeSeriesAggregationResource_Update_ClientErrorDoesNotMutateState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	updatedYaml := strings.Replace(tsaValidYaml, "interval: 5m", "interval: 30m", 1)
	mockClient.On("UpdateTimeSeriesAggregation", mock.Anything, "tf_origin", mock.Anything, "test-dataset").
		Return(errors.New("500 internal server error"))

	priorState := tsaState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	req := resource.UpdateRequest{
		State: priorState,
		Plan:  tsaPlan(nil, nil, strPtr("test-dataset"), strPtr(updatedYaml)),
	}
	resp := resource.UpdateResponse{State: priorState}

	r.Update(context.Background(), req, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, tsaValidYaml, result.TimeSeriesAggregationYaml.ValueString(),
		"a failed update must leave the prior state untouched")
}

func TestTimeSeriesAggregationResource_Update_InvalidYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	req := resource.UpdateRequest{
		State: tsaState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(tsaValidYaml)),
		Plan:  tsaPlan(nil, nil, strPtr("test-dataset"), strPtr("invalid: yaml: : :")),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Update(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
	mockClient.AssertNotCalled(t, "UpdateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// --- Delete ---------------------------------------------------------------

func TestTimeSeriesAggregationResource_Delete_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("DeleteTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").Return(nil)

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError())
}

// TestTimeSeriesAggregationResource_Delete_NotFoundIsSuccess covers a destroy
// that follows an out-of-band delete: the aggregation is already gone, which is
// the desired end state, not an error.
func TestTimeSeriesAggregationResource_Delete_NotFoundIsSuccess(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("DeleteTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	assert.False(t, resp.Diagnostics.HasError(), "an already-deleted aggregation is a successful destroy")
}

func TestTimeSeriesAggregationResource_Delete_ServerErrorSurfaces(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("DeleteTimeSeriesAggregation", mock.Anything, "tf_origin", "test-dataset").
		Return(&dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"})

	state := tsaState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tsaValidYaml))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError(), "the 404 short-circuit must not swallow a 5xx")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
}

// --- ImportState ----------------------------------------------------------

func TestTimeSeriesAggregationResource_ImportState_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_abc", "default").Return(tsaAPIResponseJSON, nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, "tf_abc", "default").Return("resolved-id", nil)

	resp := tsaImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_abc"}, resp)

	mockClient.AssertExpectations(t)
	require.False(t, resp.Diagnostics.HasError())

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.Equal(t, "tf_abc", result.Origin.ValueString())
	assert.Equal(t, "default", result.Dataset.ValueString())
	assert.Equal(t, tsaAPIResponseJSON, result.TimeSeriesAggregationYaml.ValueString())
	assert.Equal(t, "resolved-id", result.ID.ValueString())
}

func TestTimeSeriesAggregationResource_ImportState_SinglePartIDErrors(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	resp := tsaImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "tf_abc"}, resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid Import ID")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "Expected import ID in the format 'dataset,origin'. Got: tf_abc")
	mockClient.AssertNotCalled(t, "GetTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything)
}

func TestTimeSeriesAggregationResource_ImportState_NotFoundWritesNoPartialState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_missing", "default").
		Return("", &dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	resp := tsaImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_missing"}, resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Error Importing Time Series Aggregation")

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.Origin.IsNull(), "no attribute may be written when the import target does not exist")
	assert.True(t, result.Dataset.IsNull())
	assert.True(t, result.TimeSeriesAggregationYaml.IsNull())
	mockClient.AssertNotCalled(t, "ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything)
}

// TestTimeSeriesAggregationResource_ImportState_TombstoneRejected pins that a
// soft-deleted aggregation cannot be adopted. Because the GET succeeds with a
// 200, the not-found branch above does not cover this case.
func TestTimeSeriesAggregationResource_ImportState_TombstoneRejected(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	mockClient.On("GetTimeSeriesAggregation", mock.Anything, "tf_deleted", "default").Return(tsaTombstoneJSON, nil)

	resp := tsaImportStateResponse()
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_deleted"}, resp)

	mockClient.AssertExpectations(t)
	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Error Importing Time Series Aggregation")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "dash0.com/deleted-at")

	var result timeSeriesAggregationModel
	require.False(t, resp.State.Get(context.Background(), &result).HasError())
	assert.True(t, result.Origin.IsNull(), "a tombstone must not be adopted into state")
	assert.True(t, result.TimeSeriesAggregationYaml.IsNull())
	mockClient.AssertNotCalled(t, "ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything)
}

// --- ValidateConfig -------------------------------------------------------

func TestTimeSeriesAggregationResource_ValidateConfig_Valid(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest(tsaValidYaml), resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

func TestTimeSeriesAggregationResource_ValidateConfig_InvalidYAML(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest("invalid: yaml: ["), resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML in time_series_aggregation_yaml")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
	assert.Equal(t, "time_series_aggregation_yaml", withPath.Path().String())
}

// Empty, whitespace-only, and explicitly null documents unmarshal into a nil
// map without erroring, so they reach the shape guard rather than the YAML
// syntax branch above it. Scalars and sequences error earlier and are covered
// by the InvalidYAML test.
func TestTimeSeriesAggregationResource_ValidateConfig_EmptyOrNonMapping(t *testing.T) {
	for name, input := range map[string]string{
		"empty":           "",
		"whitespace only": "   \n  ",
		"explicit null":   "null",
	} {
		t.Run(name, func(t *testing.T) {
			r := &TimeSeriesAggregationResource{}
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), tsaValidateConfigRequest(input), resp)

			require.True(t, resp.Diagnostics.HasError(), "a document with no mapping must not reach apply")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "empty or not a YAML mapping")

			withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
			require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
			assert.Equal(t, "time_series_aggregation_yaml", withPath.Path().String())
		})
	}
}

func TestTimeSeriesAggregationResource_ValidateConfig_WrongKind(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest(`
kind: Dash0View
metadata:
  name: not-an-aggregation
spec:
  display:
    name: Not An Aggregation
`), resp)

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "kind")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "kind: Dash0TimeSeriesAggregation")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "Dash0View")
}

func TestTimeSeriesAggregationResource_ValidateConfig_CustomLabelsWarn(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest(`
kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
  labels:
    custom:
      team: platform
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`), resp)

	assert.False(t, resp.Diagnostics.HasError())
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels.custom")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "team",
		"the warning must name the offending key so the user can find it")
}

// spec.enabled is required, not defaulted. TimeSeriesAggregationSpec.Enabled is
// a non-pointer bool with no omitempty, so the provider always sends a value:
// accepting an omitted field would silently create a disabled aggregation, and
// a later apply would switch off one that had been enabled outside Terraform.
func TestTimeSeriesAggregationResource_ValidateConfig_EnabledAbsentErrors(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest(`
kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`), resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach apply")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")

	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	require.True(t, ok, "the diagnostic must be attribute-scoped so Terraform can render it inline")
	assert.Equal(t, "time_series_aggregation_yaml", withPath.Path().String())
}

// An explicit spec.enabled: false is a legitimate declaration of intent and
// must pass — the rule is "declare it", not "enable it".
func TestTimeSeriesAggregationResource_ValidateConfig_EnabledFalseIsValid(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest(`
kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  enabled: false
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`), resp)

	assert.False(t, resp.Diagnostics.HasError(), "an explicit spec.enabled: false is valid")
}

// tsaYamlEnabledAbsent omits spec.enabled. ValidateConfig rejects it, but the
// tests below exercise the apply-time backstop, which is what actually protects
// a config whose YAML is unknown at plan time.
const tsaYamlCustomLabels = `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
  labels:
    custom:
      team: platform
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`

const tsaYamlEnabledAbsent = `kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
`

// The spec.enabled requirement must hold at apply time, not only at plan time.
// ValidateConfig returns early whenever time_series_aggregation_yaml is unknown
// — the ordinary case when it interpolates another resource's computed
// attribute — so Create is the only place the invariant can still be enforced.
// Without this the document reaches the API with enabled=false, which
// normalization then strips as an absent zero value, leaving a disabled
// aggregation whose plan stays clean forever.
func TestTimeSeriesAggregationResource_Create_EnabledAbsentIsRejectedAtApply(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "default"}

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr(tsaYamlEnabledAbsent))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach the API")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
	assert.True(t, resp.State.Raw.IsNull(), "no state may be written when the invariant fails")
	mockClient.AssertNotCalled(t, "CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTimeSeriesAggregationResource_Update_EnabledAbsentIsRejectedAtApply(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}

	req := resource.UpdateRequest{
		State: tsaState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(tsaValidYaml)),
		Plan:  tsaPlan(nil, nil, strPtr("test-dataset"), strPtr(tsaYamlEnabledAbsent)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Update(context.Background(), req, &resp)

	require.True(t, resp.Diagnostics.HasError(), "an omitted spec.enabled must not reach the API")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "spec.enabled")
	mockClient.AssertNotCalled(t, "UpdateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// The backstop keys on absence, not on falsiness: an explicit
// spec.enabled: false must still apply cleanly.
func TestTimeSeriesAggregationResource_Create_EnabledFalseAppliesAtApply(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "default"}

	yamlEnabledFalse := strings.Replace(tsaValidYaml, "enabled: true", "enabled: false", 1)
	require.Contains(t, yamlEnabledFalse, "enabled: false", "fixture must actually declare spec.enabled: false")

	mockClient.On("CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything).Return("an-id", nil)

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr(yamlEnabledFalse))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "an explicit spec.enabled: false is a valid declaration")
	mockClient.AssertCalled(t, "CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// The custom-labels warning has the same unknown-value blind spot as the hard
// errors: a document interpolated from a computed attribute skips ValidateConfig
// entirely, so Create and Update are the only places it can still be emitted.
// These two tests pin that it is, since a warning that silently stops firing
// looks exactly like a document with no custom labels.
func TestTimeSeriesAggregationResource_Create_CustomLabelsWarn(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient, defaultDataset: "default"}
	mockClient.On("CreateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything).Return("an-id", nil)

	req := resource.CreateRequest{Plan: tsaPlan(nil, nil, nil, strPtr(tsaYamlCustomLabels))}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Create(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "custom labels are a warning, not an error")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount(), "the warning must survive the unknown-value path")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels.custom")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "team")
}

func TestTimeSeriesAggregationResource_Update_CustomLabelsWarn(t *testing.T) {
	mockClient := &MockClient{}
	r := &TimeSeriesAggregationResource{client: mockClient}
	mockClient.On("UpdateTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockClient.On("ResolveTimeSeriesAggregation", mock.Anything, mock.Anything, mock.Anything).Return("an-id", nil)

	req := resource.UpdateRequest{
		State: tsaState(strPtr("tf_origin"), strPtr("state-id"), strPtr("test-dataset"), strPtr(tsaValidYaml)),
		Plan:  tsaPlan(strPtr("tf_origin"), nil, strPtr("test-dataset"), strPtr(tsaYamlCustomLabels)),
	}
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: tsaTestSchema()}}

	r.Update(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "custom labels are a warning, not an error")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount(), "the warning must survive the unknown-value path")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels.custom")
}

// TestTimeSeriesAggregationResource_ValidateConfig_ResourceContextIsNotValidated
// pins a deliberate, user-settled decision: resource-level attribute context is
// passed through unvalidated because the sibling Dash0 CLI validates nothing
// there, and making Terraform stricter than the CLI for the same API would
// diverge the two. A later maintainer must not silently add validation here.
func TestTimeSeriesAggregationResource_ValidateConfig_ResourceContextIsNotValidated(t *testing.T) {
	r := &TimeSeriesAggregationResource{}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), tsaValidateConfigRequest(`
kind: Dash0TimeSeriesAggregation
metadata:
  name: rollup
spec:
  enabled: true
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
  attributeModifications:
    - kind: drop_attributes
      spec:
        context: resource
        keyMatcher:
          operator: is
          value: http.route
`), resp)

	assert.False(t, resp.Diagnostics.HasError(), "spec.attributeModifications[].spec.context must not be validated")
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount(), "spec.attributeModifications[].spec.context must not warn either")
}
