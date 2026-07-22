package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testTeamClient is a minimal client.Client stub that returns a canned
// response for GetTeam. It embeds client.Client so unused methods panic if
// accidentally called.
type testTeamClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testTeamClient) GetTeam(_ context.Context, _ string) (string, error) {
	return c.getResponse, c.getError
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
			name:              "invalid YAML response - update and warn",
			apiResponseYaml:   "not valid yaml {",
			expectYamlUpdated: true,
			expectWarning:     true,
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
				assert.Equal(t, originalYaml, resultState.TeamYaml.ValueString())
			}

			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
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
