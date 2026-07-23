package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testTeamClient is a minimal client.Client stub that returns canned
// responses for GetTeam and ResolveTeam. It embeds client.Client so unused
// methods panic if accidentally called.
type testTeamClient struct {
	client.Client
	getResponse     string
	getError        error
	resolveID       string
	resolveError    error
	resolveCallSeen bool
}

func (c *testTeamClient) GetTeam(_ context.Context, _ string) (string, error) {
	return c.getResponse, c.getError
}

func (c *testTeamClient) ResolveTeam(_ context.Context, _ string) (string, error) {
	c.resolveCallSeen = true
	return c.resolveID, c.resolveError
}

// TestTeamResource_ReadWithDiffs exercises the resource's Read normalization
// so drift is detected only on fields the user actually authored. The
// canned API response uses emails and preserves dash0.com/origin — matching
// what the client wrapper (client.GetTeam) produces after
// dash0.ResolveTeamMembersToEmails + dash0.StripTeamServerFields have run.
// Server-managed labels/annotations that the resource strips itself
// (teamAlwaysIgnoredFields) are also covered.
func TestTeamResource_ReadWithDiffs(t *testing.T) {
	testOrigin := "tf_backend"

	// User-authored YAML in state. Members are emails; metadata is minimal.
	originalYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - alice@example.com
    - bob@example.com`

	tests := []struct {
		name              string
		apiResponseYaml   string
		expectYamlUpdated bool
		expectWarning     bool
		expectError       bool
	}{
		{
			name: "metadata changes only - no significant diff",
			// The client wrapper already stripped dash0.com/id, source,
			// created-at, updated-at; dash0.com/origin remains and is
			// covered by teamAlwaysIgnoredFields.
			apiResponseYaml: `kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
spec:
  display:
    name: Backend Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - alice@example.com
    - bob@example.com`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
		{
			name: "significant content change - description drift",
			apiResponseYaml: `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    description: A different description entirely.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - alice@example.com
    - bob@example.com`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
		{
			name: "membership drift - server removed a member",
			apiResponseYaml: `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - alice@example.com`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
		{
			name: "metadata.name change - drift",
			apiResponseYaml: `kind: Dash0Team
metadata:
  name: renamed-backend-team
spec:
  display:
    name: Backend Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - alice@example.com
    - bob@example.com`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
		{
			// Regression: previously the code overwrote state.TeamYaml with
			// the unparseable API response and only warned, which permanently
			// poisoned state — the next refresh compared against garbage,
			// diffed forever, and could only be recovered by `terraform state
			// rm`. Now we preserve the prior state and surface an error so a
			// subsequent successful refresh can heal state naturally.
			name:              "invalid YAML response - preserve state and error",
			apiResponseYaml:   "not valid yaml {",
			expectYamlUpdated: false,
			expectWarning:     false,
			expectError:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testSchema := schema.Schema{
				Attributes: map[string]schema.Attribute{
					"origin": schema.StringAttribute{
						Computed: true,
					},
					"id": schema.StringAttribute{
						Computed: true,
					},
					"team_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			}

			testClient := &testTeamClient{getResponse: tc.apiResponseYaml}
			r := &TeamResource{client: testClient}

			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":    tftypes.String,
						"id":        tftypes.String,
						"team_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":    tftypes.NewValue(tftypes.String, testOrigin),
					"id":        tftypes.NewValue(tftypes.String, nil),
					"team_yaml": tftypes.NewValue(tftypes.String, originalYaml),
				},
			)

			state := tfsdk.State{Raw: raw, Schema: testSchema}
			req := resource.ReadRequest{State: state}
			resp := resource.ReadResponse{State: state}

			r.Read(context.Background(), req, &resp)

			var resultState teamModel
			resp.State.Get(context.Background(), &resultState)

			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponseYaml, resultState.TeamYaml.ValueString())
			} else {
				assert.Equal(t, originalYaml, resultState.TeamYaml.ValueString(),
					"prior state.team_yaml must be preserved when the API response is unparseable")
			}

			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
			assert.Equal(t, tc.expectError, resp.Diagnostics.HasError())
		})
	}
}

// TestTeamResource_ReadPreservesOriginLabel verifies that when the API
// response contains labels that the resource strips (server-managed
// metadata), the state comparison still matches the user's minimal YAML —
// i.e. the presence of dash0.com/origin in the response does not manifest as
// perpetual drift. This is regression coverage for the
// teamAlwaysIgnoredFields set.
func TestTeamResource_ReadPreservesOriginLabel(t *testing.T) {
	testOrigin := "tf_backend"

	stateYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    color:
      from: "#111"
      to: "#222"
  members:
    - alice@example.com`

	apiResponseYaml := `kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
spec:
  display:
    name: Backend Team
    color:
      from: "#111"
      to: "#222"
  members:
    - alice@example.com`

	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"team_yaml": schema.StringAttribute{Required: true},
		},
	}
	testClient := &testTeamClient{getResponse: apiResponseYaml}
	r := &TeamResource{client: testClient}

	raw := tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"origin":    tftypes.String,
				"id":        tftypes.String,
				"team_yaml": tftypes.String,
			},
		},
		map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, testOrigin),
			"id":        tftypes.NewValue(tftypes.String, nil),
			"team_yaml": tftypes.NewValue(tftypes.String, stateYaml),
		},
	)

	state := tfsdk.State{Raw: raw, Schema: testSchema}
	req := resource.ReadRequest{State: state}
	resp := resource.ReadResponse{State: state}

	r.Read(context.Background(), req, &resp)

	var resultState teamModel
	resp.State.Get(context.Background(), &resultState)

	assert.Equal(t, stateYaml, resultState.TeamYaml.ValueString(),
		"server-side dash0.com/origin must not trigger a state update")
	assert.Equal(t, 0, resp.Diagnostics.WarningsCount())
}

// TestTeamResource_ReadNotFoundClearsState covers the out-of-band-delete
// contract: when GetTeam returns a 404 (team removed via CLI, UI, or another
// workspace), Read must clear state so the next plan re-creates the resource,
// not surface a hard error that forces `terraform state rm`.
func TestTeamResource_ReadNotFoundClearsState(t *testing.T) {
	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"team_yaml": schema.StringAttribute{Required: true},
		},
	}
	testClient := &testTeamClient{getError: &dash0.APIError{StatusCode: 404, Status: "404 Not Found"}}
	r := &TeamResource{client: testClient}

	raw := tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"origin":    tftypes.String,
				"id":        tftypes.String,
				"team_yaml": tftypes.String,
			},
		},
		map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, "tf_backend"),
			"id":        tftypes.NewValue(tftypes.String, nil),
			"team_yaml": tftypes.NewValue(tftypes.String, "kind: Dash0Team"),
		},
	)

	state := tfsdk.State{Raw: raw, Schema: testSchema}
	req := resource.ReadRequest{State: state}
	resp := resource.ReadResponse{State: state}

	r.Read(context.Background(), req, &resp)

	assert.False(t, resp.Diagnostics.HasError(), "404 must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared when the team no longer exists")
}

// TestTeamResource_ReadNonNotFoundStillErrors ensures the 404 short-circuit
// does not swallow other transport errors (5xx, network failures, auth
// errors). Only IsNotFound should route to RemoveResource.
func TestTeamResource_ReadNonNotFoundStillErrors(t *testing.T) {
	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"team_yaml": schema.StringAttribute{Required: true},
		},
	}
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
			testClient := &testTeamClient{getError: tc.err}
			r := &TeamResource{client: testClient}

			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":    tftypes.String,
						"id":        tftypes.String,
						"team_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":    tftypes.NewValue(tftypes.String, "tf_backend"),
					"id":        tftypes.NewValue(tftypes.String, nil),
					"team_yaml": tftypes.NewValue(tftypes.String, "kind: Dash0Team"),
				},
			)

			state := tfsdk.State{Raw: raw, Schema: testSchema}
			req := resource.ReadRequest{State: state}
			resp := resource.ReadResponse{State: state}

			r.Read(context.Background(), req, &resp)

			assert.True(t, resp.Diagnostics.HasError(), "non-404 errors must still surface")
			assert.False(t, resp.State.Raw.IsNull(), "state must be preserved on transient errors")
		})
	}
}

// TestTeamResource_ReadSelfHealsNullID covers the self-heal contract: when
// state.ID is null (typically because Create's best-effort resolveTeamID
// failed transiently), a subsequent Read must re-resolve so downstream
// references to dash0_team.foo.id stop rendering as null once the underlying
// issue clears.
func TestTeamResource_ReadSelfHealsNullID(t *testing.T) {
	stateYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
  members: []`

	apiResponseYaml := `kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
spec:
  display:
    name: Backend Team
  members: []`

	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"team_yaml": schema.StringAttribute{Required: true},
		},
	}
	testClient := &testTeamClient{
		getResponse: apiResponseYaml,
		resolveID:   "00000000-0000-0000-0000-000000000001",
	}
	r := &TeamResource{client: testClient}

	raw := tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"origin":    tftypes.String,
				"id":        tftypes.String,
				"team_yaml": tftypes.String,
			},
		},
		map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, "tf_backend"),
			"id":        tftypes.NewValue(tftypes.String, nil), // stuck-null from a prior transient failure
			"team_yaml": tftypes.NewValue(tftypes.String, stateYaml),
		},
	)

	state := tfsdk.State{Raw: raw, Schema: testSchema}
	req := resource.ReadRequest{State: state}
	resp := resource.ReadResponse{State: state}

	r.Read(context.Background(), req, &resp)

	assert.True(t, testClient.resolveCallSeen, "Read must re-resolve when state.id is null")

	var resultState teamModel
	resp.State.Get(context.Background(), &resultState)
	assert.Equal(t, "00000000-0000-0000-0000-000000000001", resultState.ID.ValueString(),
		"resolved id must be written to state so downstream refs stop rendering as null")
}

// TestTeamResource_ReadSkipsResolveWhenIDAlreadyPresent asserts the
// self-heal branch does not fire wasteful re-resolutions once the id is
// known — the team id is immutable server-side, so re-resolving on every
// refresh would be pure overhead.
func TestTeamResource_ReadSkipsResolveWhenIDAlreadyPresent(t *testing.T) {
	stateYaml := `kind: Dash0Team
metadata:
  name: backend-team
spec:
  members: []`

	apiResponseYaml := `kind: Dash0Team
metadata:
  name: backend-team
  labels:
    dash0.com/origin: tf_backend
spec:
  members: []`

	testSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"team_yaml": schema.StringAttribute{Required: true},
		},
	}
	testClient := &testTeamClient{getResponse: apiResponseYaml}
	r := &TeamResource{client: testClient}

	raw := tftypes.NewValue(
		tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"origin":    tftypes.String,
				"id":        tftypes.String,
				"team_yaml": tftypes.String,
			},
		},
		map[string]tftypes.Value{
			"origin":    tftypes.NewValue(tftypes.String, "tf_backend"),
			"id":        tftypes.NewValue(tftypes.String, "00000000-0000-0000-0000-000000000001"),
			"team_yaml": tftypes.NewValue(tftypes.String, stateYaml),
		},
	)

	state := tfsdk.State{Raw: raw, Schema: testSchema}
	req := resource.ReadRequest{State: state}
	resp := resource.ReadResponse{State: state}

	r.Read(context.Background(), req, &resp)

	assert.False(t, testClient.resolveCallSeen, "Read must not re-resolve when state.id is already populated")

	var resultState teamModel
	resp.State.Get(context.Background(), &resultState)
	assert.Equal(t, "00000000-0000-0000-0000-000000000001", resultState.ID.ValueString(),
		"existing id must be preserved untouched")
}
