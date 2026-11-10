package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource                   = &TeamDataSource{}
	_ datasource.DataSourceWithConfigure      = &TeamDataSource{}
	_ datasource.DataSourceWithValidateConfig = &TeamDataSource{}
)

// NewTeamDataSource is a helper function to simplify the provider implementation.
func NewTeamDataSource() datasource.DataSource {
	return &TeamDataSource{}
}

// TeamDataSource looks up one team by display name or id. It reads the team
// list on every plan, so teams created outside Terraform, such as teams synced
// from an identity provider, can be referenced without copying their id.
type TeamDataSource struct {
	client client.Client
}

// teamDataSourceModel is the Terraform state model for the team data source.
type teamDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Origin      types.String `tfsdk:"origin"`
	MemberCount types.Int64  `tfsdk:"member_count"`
}

// Configure adds the provider configured client to the data source.
func (d *TeamDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = c
}

func (d *TeamDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

func (d *TeamDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a Dash0 team by its display name or id. " +
			"Use it to reference a team that Terraform does not manage, such as a team synced from your identity provider, " +
			"for example as the `teamId` of a grant in `spec.permissions` of a `dash0_dataset`.\n\n" +
			"Set exactly one of `name` or `id`. " +
			"Dash0 allows two teams to have the same display name. When more than one team has the name, the lookup fails and lists their ids, so you can look the team up by `id` instead.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Description: "The display name of the team, as shown in Dash0. The match is exact and case-sensitive.",
				Optional:    true,
				Computed:    true,
			},
			"id": schema.StringAttribute{
				Description: "The id of the team, for example `team_01h455vb4pex5vsknk084sn02q`. " +
					"Set it to look up the team by id instead of by name.",
				Optional: true,
				Computed: true,
			},
			"origin": schema.StringAttribute{
				Description: "The external identifier of the team, set by the tool that created it. Null for teams created in the Dash0 UI.",
				Computed:    true,
			},
			"member_count": schema.Int64Attribute{
				Description: "The number of members in the team.",
				Computed:    true,
			},
		},
	}
}

// ValidateConfig requires exactly one of name and id. It skips the check while
// either value is unknown, such as a module variable during validate, because
// an unknown value can still resolve to null. Terraform validates the config
// again once the values are known.
func (d *TeamDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var config teamDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Name.IsUnknown() || config.ID.IsUnknown() {
		return
	}

	nameSet := !config.Name.IsNull()
	idSet := !config.ID.IsNull()
	switch {
	case nameSet && idSet:
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Conflicting Team Lookup Attributes",
			"Set either name or id, not both.",
		)
	case !nameSet && !idSet:
		resp.Diagnostics.AddError(
			"Missing Team Lookup Attribute",
			"Set name or id to choose the team to look up.",
		)
	}
}

func (d *TeamDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config teamDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	teams, err := d.client.ListTeams(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Dash0 Teams",
			fmt.Sprintf("Could not list teams: %s", err),
		)
		return
	}

	byID := !config.ID.IsNull()
	var matches []*dash0.TeamsListItem
	for _, team := range teams {
		if (byID && team.Id == config.ID.ValueString()) || (!byID && team.Name == config.Name.ValueString()) {
			matches = append(matches, team)
		}
	}

	switch {
	case len(matches) == 0 && byID:
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Dash0 Team Not Found",
			fmt.Sprintf("No team has the id %q.", config.ID.ValueString()),
		)
		return
	case len(matches) == 0:
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Dash0 Team Not Found",
			fmt.Sprintf("No team has the display name %q. The match is case-sensitive. "+
				"A team synced from an identity provider exists only after the sync has run.", config.Name.ValueString()),
		)
		return
	case len(matches) > 1:
		ids := make([]string, len(matches))
		for i, team := range matches {
			ids[i] = team.Id
		}
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Multiple Dash0 Teams Found",
			fmt.Sprintf("%d teams have the display name %q: %s. Look up the team by id instead.",
				len(matches), config.Name.ValueString(), strings.Join(ids, ", ")),
		)
		return
	}

	team := matches[0]
	state := teamDataSourceModel{
		ID:          types.StringValue(team.Id),
		Name:        types.StringValue(team.Name),
		Origin:      types.StringPointerValue(team.Origin),
		MemberCount: types.Int64Value(int64(team.TotalMemberCount)),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
