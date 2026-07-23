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

// teamDeleteState builds the minimal state fixture used by the Delete tests.
func teamDeleteState(origin string) tfsdk.State {
	return tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":    tftypes.String,
					"id":        tftypes.String,
					"team_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, origin),
				"id":        tftypes.NewValue(tftypes.String, nil),
				"team_yaml": tftypes.NewValue(tftypes.String, "kind: Dash0Team"),
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
