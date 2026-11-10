package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// testTeamListClient returns a canned team list. It embeds client.Client so
// any other method panics if called.
type testTeamListClient struct {
	client.Client
	teams []*dash0.TeamsListItem
	err   error
}

func (c *testTeamListClient) ListTeams(_ context.Context) ([]*dash0.TeamsListItem, error) {
	return c.teams, c.err
}

// teamDataSourceConfig builds a config with the given name and id, where nil
// means null and tftypes.UnknownValue means unknown.
func teamDataSourceConfig(t *testing.T, ds *TeamDataSource, name, id interface{}) tfsdk.Config {
	t.Helper()
	schemaResp := &datasource.SchemaResponse{}
	ds.Schema(context.Background(), datasource.SchemaRequest{}, schemaResp)
	require.False(t, schemaResp.Diagnostics.HasError())

	objType := schemaResp.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	return tfsdk.Config{
		Schema: schemaResp.Schema,
		Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
			"name":         tftypes.NewValue(tftypes.String, name),
			"id":           tftypes.NewValue(tftypes.String, id),
			"origin":       tftypes.NewValue(tftypes.String, nil),
			"member_count": tftypes.NewValue(tftypes.Number, nil),
		}),
	}
}

func readTeamDataSource(t *testing.T, c client.Client, name, id interface{}) (*datasource.ReadResponse, teamDataSourceModel) {
	t.Helper()
	ds := &TeamDataSource{client: c}
	config := teamDataSourceConfig(t, ds, name, id)
	resp := &datasource.ReadResponse{
		State: tfsdk.State{Schema: config.Schema, Raw: tftypes.NewValue(config.Raw.Type(), nil)},
	}
	ds.Read(context.Background(), datasource.ReadRequest{Config: config}, resp)

	var state teamDataSourceModel
	if !resp.Diagnostics.HasError() {
		require.False(t, resp.State.Get(context.Background(), &state).HasError())
	}
	return resp, state
}

func TestTeamDataSource_Read(t *testing.T) {
	origin := "saml://conn_1/group_1"
	// Names differ only by case or suffix, so a loose match picks the wrong team.
	teams := []*dash0.TeamsListItem{
		{Id: "team_backend_lower", Name: "backend"},
		{Id: "team_backend", Name: "Backend", Origin: &origin, TotalMemberCount: 7},
		{Id: "team_backend_ops", Name: "Backend Ops", TotalMemberCount: 2},
		{Id: "team_dup_1", Name: "Platform"},
		{Id: "team_dup_2", Name: "Platform"},
	}

	t.Run("by name returns the exact match", func(t *testing.T) {
		resp, state := readTeamDataSource(t, &testTeamListClient{teams: teams}, "Backend", nil)
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.Equal(t, "team_backend", state.ID.ValueString())
		assert.Equal(t, "Backend", state.Name.ValueString())
		assert.Equal(t, origin, state.Origin.ValueString())
		assert.Equal(t, int64(7), state.MemberCount.ValueInt64())
	})

	t.Run("by id returns the team and its name", func(t *testing.T) {
		resp, state := readTeamDataSource(t, &testTeamListClient{teams: teams}, nil, "team_backend_ops")
		require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.Equal(t, "team_backend_ops", state.ID.ValueString())
		assert.Equal(t, "Backend Ops", state.Name.ValueString())
		assert.True(t, state.Origin.IsNull(), "a UI team has no origin")
		assert.Equal(t, int64(2), state.MemberCount.ValueInt64())
	})

	t.Run("no team with the name", func(t *testing.T) {
		resp, _ := readTeamDataSource(t, &testTeamListClient{teams: teams}, "BACKEND", nil)
		require.True(t, resp.Diagnostics.HasError())
		assert.Equal(t, "Dash0 Team Not Found", resp.Diagnostics.Errors()[0].Summary())
	})

	t.Run("no team with the id", func(t *testing.T) {
		resp, _ := readTeamDataSource(t, &testTeamListClient{teams: teams}, nil, "team_missing")
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), `"team_missing"`)
	})

	t.Run("duplicate names fail and list the ids", func(t *testing.T) {
		resp, _ := readTeamDataSource(t, &testTeamListClient{teams: teams}, "Platform", nil)
		require.True(t, resp.Diagnostics.HasError())
		detail := resp.Diagnostics.Errors()[0].Detail()
		assert.Contains(t, detail, "team_dup_1")
		assert.Contains(t, detail, "team_dup_2")
	})

	t.Run("API error", func(t *testing.T) {
		resp, _ := readTeamDataSource(t, &testTeamListClient{err: errors.New("429 too many requests")}, "Backend", nil)
		require.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "429 too many requests")
	})
}

func TestTeamDataSource_ValidateConfig(t *testing.T) {
	tests := []struct {
		name      string
		teamName  interface{}
		id        interface{}
		expectErr bool
	}{
		{name: "name only", teamName: "Backend", id: nil},
		{name: "id only", teamName: nil, id: "team_backend"},
		{name: "unknown name", teamName: tftypes.UnknownValue, id: nil},
		{name: "neither", teamName: nil, id: nil, expectErr: true},
		{name: "both", teamName: "Backend", id: "team_backend", expectErr: true},
		// An unknown value can resolve to null, so these must not fail yet.
		{name: "unknown name and known id", teamName: tftypes.UnknownValue, id: "team_backend"},
		{name: "unknown name and unknown id", teamName: tftypes.UnknownValue, id: tftypes.UnknownValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &TeamDataSource{}
			resp := &datasource.ValidateConfigResponse{}
			ds.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{
				Config: teamDataSourceConfig(t, ds, tt.teamName, tt.id),
			}, resp)
			assert.Equal(t, tt.expectErr, resp.Diagnostics.HasError(), resp.Diagnostics)
		})
	}
}
