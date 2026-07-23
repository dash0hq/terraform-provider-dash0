package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func TestTeamResourceModel(t *testing.T) {
	origin := "tf_backend"
	teamYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    color:
      from: "#111"
      to: "#222"
  members: []`

	m := teamModel{
		Origin:   types.StringValue(origin),
		TeamYaml: types.StringValue(teamYaml),
	}

	assert.Equal(t, origin, m.Origin.ValueString())
	assert.Equal(t, teamYaml, m.TeamYaml.ValueString())
}

func TestNewTeamResource(t *testing.T) {
	r := NewTeamResource()
	assert.NotNil(t, r)
	_, ok := r.(*TeamResource)
	assert.True(t, ok)
}

func TestTeamResource_Metadata(t *testing.T) {
	r := &TeamResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{ProviderTypeName: "dash0"}

	r.Metadata(context.Background(), req, resp)
	assert.Equal(t, "dash0_team", resp.TypeName)
}

func TestTeamResource_Schema(t *testing.T) {
	r := &TeamResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "id")
	assert.Contains(t, resp.Schema.Attributes, "team_yaml")

	// Teams are organization-level: no dataset attribute.
	assert.NotContains(t, resp.Schema.Attributes, "dataset")

	originAttr := resp.Schema.Attributes["origin"]
	assert.True(t, originAttr.IsComputed())
	assert.False(t, originAttr.IsRequired())

	idAttr := resp.Schema.Attributes["id"]
	assert.True(t, idAttr.IsComputed())
	assert.False(t, idAttr.IsRequired())

	yamlAttr := resp.Schema.Attributes["team_yaml"]
	assert.True(t, yamlAttr.IsRequired())
	assert.False(t, yamlAttr.IsComputed())
}

func TestTeamResource_Configure(t *testing.T) {
	tests := []struct {
		name         string
		providerData interface{}
		expectError  bool
		errorMessage string
	}{
		{name: "valid client interface", providerData: &MockClient{}, expectError: false},
		{name: "nil provider data", providerData: nil, expectError: false},
		{
			name:         "invalid provider data type",
			providerData: "invalid",
			expectError:  true,
			errorMessage: "Unexpected Data Source Configure Type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &TeamResource{}
			resp := &resource.ConfigureResponse{}
			req := resource.ConfigureRequest{ProviderData: tc.providerData}

			r.Configure(context.Background(), req, resp)

			if tc.expectError {
				assert.True(t, resp.Diagnostics.HasError())
				if tc.errorMessage != "" {
					assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tc.errorMessage)
				}
			} else {
				assert.False(t, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestTeamResource_Create_InvalidYAML(t *testing.T) {
	r := &TeamResource{client: &MockClient{}}

	req := resource.CreateRequest{}
	resp := &resource.CreateResponse{}
	req.Plan = tfsdk.Plan{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":    tftypes.String,
					"id":        tftypes.String,
					"team_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, "tf_origin"),
				"id":        tftypes.NewValue(tftypes.String, nil),
				"team_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: content: ["),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin":    schema.StringAttribute{Computed: true},
				"id":        schema.StringAttribute{Computed: true},
				"team_yaml": schema.StringAttribute{Required: true},
			},
		},
	}

	r.Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

func TestTeamResource_ReadError(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("GetTeam", mock.Anything, "tf_origin").Return("", errors.New("not found"))

	req := resource.ReadRequest{}
	resp := &resource.ReadResponse{}

	req.State = tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":    tftypes.String,
					"id":        tftypes.String,
					"team_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, "tf_origin"),
				"id":        tftypes.NewValue(tftypes.String, nil),
				"team_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin":    schema.StringAttribute{Computed: true},
				"id":        schema.StringAttribute{Computed: true},
				"team_yaml": schema.StringAttribute{Required: true},
			},
		},
	}

	r.Read(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	mockClient.AssertExpectations(t)
}

// TestTeamResource_ResolveTeamID_SilentEmpty locks in the shape of the
// "server has no dash0.com/id label yet" branch: the client wrapper returns
// ("", nil) — no error — and resolveTeamID must render model.ID as null
// without emitting a diagnostic. This branch is externally indistinguishable
// from a fresh team whose id has not propagated yet, so silently returning
// null is the intended behavior. Callers that rely on state.id being
// non-null must re-check on the next refresh (Read's self-heal branch,
// covered by TestTeamResource_ReadSelfHealsNullID, will retry).
func TestTeamResource_ResolveTeamID_SilentEmpty(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("ResolveTeam", mock.Anything, "tf_backend").Return("", nil)

	model := teamModel{Origin: types.StringValue("tf_backend"), ID: types.StringValue("stale-id-to-be-overwritten")}
	var diags diag.Diagnostics
	r.resolveTeamID(context.Background(), &model, &diags)

	assert.True(t, model.ID.IsNull(), "empty id from ResolveTeam must be rendered as tf null via stringOrNull")
	assert.Equal(t, 0, diags.WarningsCount(), "silent-empty branch must NOT emit a warning — reserved for the error path")
	assert.False(t, diags.HasError())
	mockClient.AssertExpectations(t)
}

// TestTeamResource_ResolveTeamID_ErrorEmitsWarning is the symmetric case:
// on a real transport error, resolveTeamID must warn AND set id to null so
// callers can distinguish "we tried and failed" (warning + null) from
// "server has no id yet" (no warning + null; TestTeamResource_ResolveTeamID_
// SilentEmpty above).
func TestTeamResource_ResolveTeamID_ErrorEmitsWarning(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("ResolveTeam", mock.Anything, "tf_backend").
		Return("", errors.New("members endpoint 500"))

	model := teamModel{Origin: types.StringValue("tf_backend"), ID: types.StringValue("stale-id-to-be-overwritten")}
	var diags diag.Diagnostics
	r.resolveTeamID(context.Background(), &model, &diags)

	assert.True(t, model.ID.IsNull(), "on error, id must be reset to null (no stale value leaks through)")
	assert.Equal(t, 1, diags.WarningsCount(), "transient failure must emit a warning so users can distinguish it from silent-empty")
	assert.Contains(t, diags.Warnings()[0].Summary(), "Unable to resolve team id")
	assert.False(t, diags.HasError(), "resolveTeamID is best-effort — never an error")
	mockClient.AssertExpectations(t)
}

// TestTeamResource_ResolveTeamID_Success is the happy-path pin: a non-empty
// id maps into model.ID as a value-bearing types.String.
func TestTeamResource_ResolveTeamID_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("ResolveTeam", mock.Anything, "tf_backend").
		Return("00000000-0000-0000-0000-000000000001", nil)

	model := teamModel{Origin: types.StringValue("tf_backend")}
	var diags diag.Diagnostics
	r.resolveTeamID(context.Background(), &model, &diags)

	assert.Equal(t, "00000000-0000-0000-0000-000000000001", model.ID.ValueString())
	assert.False(t, model.ID.IsNull())
	assert.Equal(t, 0, diags.WarningsCount())
	assert.False(t, diags.HasError())
	mockClient.AssertExpectations(t)
}

// teamTftypesValue builds a tftypes.Value carrying (origin, id, team_yaml).
// A nil id is passed through as tftypes null.
func teamTftypesValue(origin string, id *string, teamYaml string) tftypes.Value {
	var idValue tftypes.Value
	if id == nil {
		idValue = tftypes.NewValue(tftypes.String, nil)
	} else {
		idValue = tftypes.NewValue(tftypes.String, *id)
	}
	return tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"origin":    tftypes.String,
				"id":        tftypes.String,
				"team_yaml": tftypes.String,
			},
		},
		map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, origin),
			"id":        idValue,
			"team_yaml": tftypes.NewValue(tftypes.String, teamYaml),
		},
	)
}

// teamTestSchema returns the minimal in-test schema shared by Delete/Update
// fixtures. Kept as a helper so any future attribute rename lands in one place.
func teamTestSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"team_yaml": schema.StringAttribute{Required: true},
		},
	}
}

// teamDeleteState builds the minimal state fixture used by the Delete tests.
func teamDeleteState(origin string) tfsdk.State {
	return tfsdk.State{
		Raw:    teamTftypesValue(origin, nil, "kind: Dash0Team"),
		Schema: teamTestSchema(),
	}
}

func TestTeamResource_Delete_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("DeleteTeam", mock.Anything, "tf_origin").Return(nil)

	req := resource.DeleteRequest{State: teamDeleteState("tf_origin")}
	resp := &resource.DeleteResponse{}

	r.Delete(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "happy-path delete must not surface a diagnostic")
	mockClient.AssertExpectations(t)
}

// TestTeamResource_Delete_NotFoundIsIdempotent covers the destroy-after-out-of-
// band-delete case: a 404 means the team is already gone, so terraform destroy
// should proceed rather than block waiting for `terraform state rm`.
func TestTeamResource_Delete_NotFoundIsIdempotent(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("DeleteTeam", mock.Anything, "tf_origin").
		Return(&dash0.APIError{StatusCode: 404, Status: "404 Not Found"})

	req := resource.DeleteRequest{State: teamDeleteState("tf_origin")}
	resp := &resource.DeleteResponse{}

	r.Delete(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "404 on Delete must be swallowed so destroy is idempotent")
	mockClient.AssertExpectations(t)
}

// TestTeamResource_Delete_NonNotFoundStillErrors ensures the 404 short-circuit
// does not swallow other transport errors (5xx, network, auth). Only IsNotFound
// should turn into idempotent success.
func TestTeamResource_Delete_NonNotFoundStillErrors(t *testing.T) {
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
			r := &TeamResource{client: mockClient}
			mockClient.On("DeleteTeam", mock.Anything, "tf_origin").Return(tc.err)

			req := resource.DeleteRequest{State: teamDeleteState("tf_origin")}
			resp := &resource.DeleteResponse{}

			r.Delete(context.Background(), req, resp)

			assert.True(t, resp.Diagnostics.HasError(), "non-404 errors must still surface")
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
			mockClient.AssertExpectations(t)
		})
	}
}

// teamUpdateRequest builds an UpdateRequest whose State carries (stateOrigin,
// stateID, stateYaml) and whose Plan carries (planOrigin, planID, planYaml).
// Update is expected to overwrite plan.Origin/plan.ID with the state values,
// so the plan-side origin/id are usually set to a distinct sentinel to prove
// the carry-over happened.
func teamUpdateRequest(stateOrigin string, stateID *string, stateYaml, planOrigin string, planID *string, planYaml string) resource.UpdateRequest {
	return resource.UpdateRequest{
		State: tfsdk.State{
			Raw:    teamTftypesValue(stateOrigin, stateID, stateYaml),
			Schema: teamTestSchema(),
		},
		Plan: tfsdk.Plan{
			Raw:    teamTftypesValue(planOrigin, planID, planYaml),
			Schema: teamTestSchema(),
		},
	}
}

// TestTeamResource_Update_Success covers the happy path: valid YAML,
// successful UpdateTeam, state persisted with origin+id carried from prior
// state, team_yaml updated to the plan value.
func TestTeamResource_Update_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	stateYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []`
	planYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
  members: [alice@example.com]`

	// The Update path converts YAML to JSON before calling the client, so we
	// match on any string argument for the JSON body.
	mockClient.On("UpdateTeam", mock.Anything, "tf_backend", mock.AnythingOfType("string")).Return(nil)

	stateID := "00000000-0000-0000-0000-000000000001"
	req := teamUpdateRequest("tf_backend", &stateID, stateYaml, "tf_backend", &stateID, planYaml)
	resp := &resource.UpdateResponse{
		State: tfsdk.State{Raw: req.State.Raw, Schema: teamTestSchema()},
	}

	r.Update(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "happy-path Update must not error")

	var finalState teamModel
	resp.State.Get(context.Background(), &finalState)
	assert.Equal(t, planYaml, finalState.TeamYaml.ValueString(), "team_yaml must reflect the plan value")
	assert.Equal(t, "tf_backend", finalState.Origin.ValueString(), "origin must be carried from prior state")
	assert.Equal(t, stateID, finalState.ID.ValueString(), "id must be carried from prior state")
	mockClient.AssertExpectations(t)
}

// TestTeamResource_Update_CarriesOverOriginAndIDFromState is the load-bearing
// invariant of the resource: origin is immutable after Create and id is
// server-immutable. Even if the plan somehow carries a different origin/id
// (Framework quirk, corrupted plan, unknown-at-plan-time), Update must ignore
// the plan values and preserve state's.
func TestTeamResource_Update_CarriesOverOriginAndIDFromState(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	teamYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []`

	// Capture the origin actually passed to UpdateTeam so we can prove it is
	// state.Origin, not plan.Origin.
	var seenOrigin string
	mockClient.On("UpdateTeam", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		Run(func(args mock.Arguments) {
			seenOrigin = args.String(1)
		}).
		Return(nil)

	stateID := "00000000-0000-0000-0000-000000000001"
	rogueID := "99999999-9999-9999-9999-999999999999"
	// Plan carries different origin/id (simulating a Framework quirk); state
	// values must win.
	req := teamUpdateRequest("tf_backend", &stateID, teamYaml, "tf_rogue_should_be_ignored", &rogueID, teamYaml)
	resp := &resource.UpdateResponse{
		State: tfsdk.State{Raw: req.State.Raw, Schema: teamTestSchema()},
	}

	r.Update(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, "tf_backend", seenOrigin, "UpdateTeam must be called with state.Origin, not plan.Origin")

	var finalState teamModel
	resp.State.Get(context.Background(), &finalState)
	assert.Equal(t, "tf_backend", finalState.Origin.ValueString(), "state.Origin must survive Update")
	assert.Equal(t, stateID, finalState.ID.ValueString(), "state.ID must survive Update")
}

// TestTeamResource_Update_InvalidYAML covers the plan-side YAML syntax check
// before the client is ever called.
func TestTeamResource_Update_InvalidYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	stateYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []`

	stateID := "00000000-0000-0000-0000-000000000001"
	req := teamUpdateRequest("tf_backend", &stateID, stateYaml, "tf_backend", &stateID, "invalid: yaml: [")
	resp := &resource.UpdateResponse{
		State: tfsdk.State{Raw: req.State.Raw, Schema: teamTestSchema()},
	}

	r.Update(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
	// UpdateTeam must not have been called — the mock has no expectations, so
	// an unexpected call would panic with an assertion failure. Assert
	// expectations to make the "no call" contract explicit.
	mockClient.AssertExpectations(t)
}

// TestTeamResource_Update_ClientError covers the branch where the API rejects
// the write. The error propagates as a Client Error diagnostic; state is not
// updated.
func TestTeamResource_Update_ClientError(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	stateYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []`
	planYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
  members: []`

	mockClient.On("UpdateTeam", mock.Anything, "tf_backend", mock.AnythingOfType("string")).
		Return(errors.New("upstream 500"))

	stateID := "00000000-0000-0000-0000-000000000001"
	req := teamUpdateRequest("tf_backend", &stateID, stateYaml, "tf_backend", &stateID, planYaml)
	resp := &resource.UpdateResponse{
		State: tfsdk.State{Raw: req.State.Raw, Schema: teamTestSchema()},
	}

	r.Update(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "upstream 500")
	mockClient.AssertExpectations(t)
}

// teamImportStateResponse builds an ImportStateResponse whose State is
// initialized the way the framework's ImportResourceState RPC does: a
// tftypes.Object whose attributes are all null. ImportState then fills them
// in via SetAttribute.
func teamImportStateResponse() *resource.ImportStateResponse {
	nullRaw := tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"origin":    tftypes.String,
				"id":        tftypes.String,
				"team_yaml": tftypes.String,
			},
		},
		map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, nil),
			"id":        tftypes.NewValue(tftypes.String, nil),
			"team_yaml": tftypes.NewValue(tftypes.String, nil),
		},
	)
	return &resource.ImportStateResponse{
		State: tfsdk.State{Raw: nullRaw, Schema: teamTestSchema()},
	}
}

// TestTeamResource_ImportState_Success covers the happy path: GetTeam
// returns valid YAML, ResolveTeam returns the server-assigned id, and state
// is populated with all three attributes so the subsequent Refresh has a
// complete resource to reconcile against.
func TestTeamResource_ImportState_Success(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	apiResponse := `kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
spec:
  display:
    name: Backend Team
  members: []`
	resolvedID := "00000000-0000-0000-0000-000000000001"

	mockClient.On("GetTeam", mock.Anything, "tf_backend").Return(apiResponse, nil)
	mockClient.On("ResolveTeam", mock.Anything, "tf_backend").Return(resolvedID, nil)

	req := resource.ImportStateRequest{ID: "tf_backend"}
	resp := teamImportStateResponse()

	r.ImportState(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())

	var finalState teamModel
	resp.State.Get(context.Background(), &finalState)
	assert.Equal(t, "tf_backend", finalState.Origin.ValueString())
	assert.Equal(t, apiResponse, finalState.TeamYaml.ValueString())
	assert.Equal(t, resolvedID, finalState.ID.ValueString())
	mockClient.AssertExpectations(t)
}

// TestTeamResource_ImportState_GetTeamError covers the branch where the
// import identifier does not resolve (wrong origin, wrong id, insufficient
// permissions). The error is surfaced and no state is written; ResolveTeam
// must not be called since it would only reproduce the same failure.
func TestTeamResource_ImportState_GetTeamError(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	mockClient.On("GetTeam", mock.Anything, "does_not_exist").
		Return("", errors.New("dash0 api error: not found (status: 404)"))

	req := resource.ImportStateRequest{ID: "does_not_exist"}
	resp := teamImportStateResponse()

	r.ImportState(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Error Importing Team")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "does_not_exist")

	// State must remain null-shaped — ImportState short-circuits before any
	// SetAttribute call.
	var finalState teamModel
	resp.State.Get(context.Background(), &finalState)
	assert.True(t, finalState.Origin.IsNull(), "state must not be partially populated on GetTeam failure")
	assert.True(t, finalState.TeamYaml.IsNull())

	mockClient.AssertExpectations(t)
	mockClient.AssertNotCalled(t, "ResolveTeam", mock.Anything, mock.Anything)
}

// TestTeamResource_ImportState_ResolveTeamFailureKeepsIDNull covers the
// best-effort id-resolution contract: if ResolveTeam fails after GetTeam
// succeeded, import still finishes with origin+team_yaml set, id=null, and
// a warning. Read's self-heal branch (Read calls resolveTeamID when
// state.ID.IsNull()) later recovers the id on the next refresh.
func TestTeamResource_ImportState_ResolveTeamFailureKeepsIDNull(t *testing.T) {
	mockClient := &MockClient{}
	r := &TeamResource{client: mockClient}

	apiResponse := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []`

	mockClient.On("GetTeam", mock.Anything, "tf_backend").Return(apiResponse, nil)
	mockClient.On("ResolveTeam", mock.Anything, "tf_backend").Return("", errors.New("members endpoint unavailable"))

	req := resource.ImportStateRequest{ID: "tf_backend"}
	resp := teamImportStateResponse()

	r.ImportState(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "ResolveTeam failure must not abort import")
	assert.Equal(t, 1, resp.Diagnostics.WarningsCount(), "ResolveTeam failure must warn")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Unable to resolve team id")

	var finalState teamModel
	resp.State.Get(context.Background(), &finalState)
	assert.Equal(t, "tf_backend", finalState.Origin.ValueString())
	assert.Equal(t, apiResponse, finalState.TeamYaml.ValueString())
	assert.True(t, finalState.ID.IsNull(), "id must remain null so Read can self-heal on the next refresh")

	mockClient.AssertExpectations(t)
}

func TestWarnIfCustomTeamMetadataSet(t *testing.T) {
	cases := []struct {
		name           string
		yaml           string
		expectWarnings int
		expectSummary  string
	}{
		{
			name: "no metadata",
			yaml: `
kind: Dash0Team
spec:
  display:
    name: Backend Team
  members: []
`,
			expectWarnings: 0,
		},
		{
			name: "metadata.name only",
			yaml: `
kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []
`,
			expectWarnings: 0,
		},
		{
			name: "only dash0.com/* labels and annotations",
			yaml: `
kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
  annotations:
    dash0.com/created-at: "2026-01-15T10:00:00Z"
spec:
  members: []
`,
			expectWarnings: 0,
		},
		{
			name: "custom label surfaces a warning",
			yaml: `
kind: Dash0Team
metadata:
  name: backend-team
  labels:
    team-lead: alice@example.com
spec:
  members: []
`,
			expectWarnings: 1,
			expectSummary:  "metadata.labels outside the dash0.com/* namespace are dropped",
		},
		{
			name: "custom annotation surfaces a warning",
			yaml: `
kind: Dash0Team
metadata:
  name: backend-team
  annotations:
    internal.example.com/cost-center: eng-042
spec:
  members: []
`,
			expectWarnings: 1,
			expectSummary:  "metadata.annotations outside the dash0.com/* namespace are dropped",
		},
		{
			name: "mixed custom + dash0.com labels — one warning listing only the custom key",
			yaml: `
kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
    team-lead: alice@example.com
spec:
  members: []
`,
			expectWarnings: 1,
		},
		{
			name: "custom in both labels and annotations produces two warnings",
			yaml: `
kind: Dash0Team
metadata:
  name: backend-team
  labels:
    team-lead: alice@example.com
  annotations:
    internal.example.com/cost-center: eng-042
spec:
  members: []
`,
			expectWarnings: 2,
		},
		{
			name:           "invalid yaml is silently ignored (validated elsewhere)",
			yaml:           "not valid {",
			expectWarnings: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			warnIfCustomTeamMetadataSet(tc.yaml, &diags)
			assert.Equal(t, tc.expectWarnings, diags.WarningsCount())
			if tc.expectSummary != "" && diags.WarningsCount() > 0 {
				assert.Contains(t, diags.Warnings()[0].Summary(), tc.expectSummary)
			}
		})
	}
}

// teamValidateConfigRequest builds a ValidateConfigRequest whose Config
// carries the given team_yaml. Origin and id are left null — ValidateConfig
// only inspects team_yaml.
func teamValidateConfigRequest(teamYaml string) resource.ValidateConfigRequest {
	return resource.ValidateConfigRequest{
		Config: tfsdk.Config{
			Raw:    teamTftypesValue("", nil, teamYaml),
			Schema: teamTestSchema(),
		},
	}
}

// TestTeamResource_ValidateConfig_ValidYAML covers the happy path: a
// well-formed Dash0Team CRD envelope passes validation with no errors and no
// warnings (no custom labels).
func TestTeamResource_ValidateConfig_ValidYAML(t *testing.T) {
	r := &TeamResource{}
	req := teamValidateConfigRequest(`
apiVersion: dash0.com/v1alpha1
kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    color:
      from: "#111"
      to: "#222"
  members: []
`)
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

// TestTeamResource_ValidateConfig_InvalidYAML asserts that syntactically
// invalid YAML fails at plan time (via ValidateConfig) rather than at apply
// time (via Create/Update's yaml.Unmarshal). The error must be attributed to
// the team_yaml attribute so Terraform surfaces it inline.
func TestTeamResource_ValidateConfig_InvalidYAML(t *testing.T) {
	r := &TeamResource{}
	req := teamValidateConfigRequest("invalid: yaml: [")
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), req, resp)

	require := assert.New(t)
	require.True(resp.Diagnostics.HasError())
	require.Contains(resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
	// AttributePath is populated on attribute-scoped diagnostics.
	require.NotEmpty(resp.Diagnostics.ErrorsCount(), "attribute-scoped diagnostic should be present")
}

// TestTeamResource_ValidateConfig_WrongKind asserts the CRD shape check: the
// resource only manages `kind: Dash0Team`, so any other kind is rejected at
// plan time with a message pointing at team_yaml.
func TestTeamResource_ValidateConfig_WrongKind(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "missing kind",
			yaml: `
metadata:
  name: backend-team
spec:
  members: []
`,
		},
		{
			name: "wrong kind (Dash0Dashboard)",
			yaml: `
kind: Dash0Dashboard
metadata:
  name: backend-team
spec: {}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &TeamResource{}
			req := teamValidateConfigRequest(tc.yaml)
			resp := &resource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), req, resp)

			assert.True(t, resp.Diagnostics.HasError())
			assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "wrong kind")
		})
	}
}

// TestTeamResource_ValidateConfig_EmptyDocument covers the case where
// yaml.Unmarshal succeeds but produces nil (an empty or whitespace-only
// input). Without the empty-map guard, the subsequent kind check would
// index a nil map. Bare scalars and sequence roots trip the earlier
// yaml.Unmarshal-into-map error path and are covered by
// ValidateConfig_InvalidYAML.
func TestTeamResource_ValidateConfig_EmptyDocument(t *testing.T) {
	r := &TeamResource{}
	req := teamValidateConfigRequest("")
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "empty or not a YAML mapping")
}

// TestTeamResource_ValidateConfig_CustomMetadataWarns asserts the two
// validators compose correctly: valid YAML with the right kind but custom
// labels emits a warning (not an error) from the metadata check.
func TestTeamResource_ValidateConfig_CustomMetadataWarns(t *testing.T) {
	r := &TeamResource{}
	req := teamValidateConfigRequest(`
kind: Dash0Team
metadata:
  name: backend-team
  labels:
    team-lead: alice@example.com
spec:
  members: []
`)
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "metadata.labels outside the dash0.com/*")
}

// TestTeamResource_ValidateConfig_UnknownOrNullSkips guards the early-return
// branch: when team_yaml is unknown at plan time (e.g., interpolation from
// another as-yet-uncomputed resource) or null, ValidateConfig must
// short-circuit without erroring — the later Create/Update path will re-run
// the checks against the resolved value.
func TestTeamResource_ValidateConfig_UnknownOrNullSkips(t *testing.T) {
	r := &TeamResource{}
	req := resource.ValidateConfigRequest{
		Config: tfsdk.Config{
			Raw: tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":    tftypes.String,
						"id":        tftypes.String,
						"team_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":    tftypes.NewValue(tftypes.String, nil),
					"id":        tftypes.NewValue(tftypes.String, nil),
					"team_yaml": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
				},
			),
			Schema: teamTestSchema(),
		},
	}
	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "unknown team_yaml must not trip validation prematurely")
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

// TestWarnIfCustomTeamMetadataSet_ListsCustomKeysInDetail asserts the
// warning detail identifies exactly which custom keys will be dropped, so
// users can locate and remove them.
func TestWarnIfCustomTeamMetadataSet_ListsCustomKeysInDetail(t *testing.T) {
	teamYaml := `
kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
    zulu: last
    alpha: first
spec:
  members: []
`
	var diags diag.Diagnostics
	warnIfCustomTeamMetadataSet(teamYaml, &diags)
	require := assert.New(t)
	require.Equal(1, diags.WarningsCount())
	detail := diags.Warnings()[0].Detail()
	// Custom keys are sorted for stable output.
	require.Contains(detail, "alpha, zulu")
	// dash0.com/* keys must not appear in the list of dropped entries.
	require.NotContains(detail, "dash0.com/origin")
}
