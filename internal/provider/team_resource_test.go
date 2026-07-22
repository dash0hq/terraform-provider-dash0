package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
