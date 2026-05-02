package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	customplanmodifier "github.com/dash0hq/terraform-provider-dash0/internal/provider/planmodifier"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &DashboardResource{}
	_ resource.ResourceWithConfigure   = &DashboardResource{}
	_ resource.ResourceWithImportState = &DashboardResource{}
)

// NewDashboardResource is a helper function to simplify the provider implementation.
func NewDashboardResource() resource.Resource {
	return &DashboardResource{}
}

// DashboardResource is the resource implementation.
type DashboardResource struct {
	client client.Client
}

// dashboardModel is the Terraform state model for a dashboard resource.
type dashboardModel struct {
	Origin        types.String `tfsdk:"origin"`
	Dataset       types.String `tfsdk:"dataset"`
	DashboardYaml types.String `tfsdk:"dashboard_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *DashboardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *DashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

func (r *DashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Manages a Dash0 Dashboard. Dashboards provide visualizations of your telemetry data such as metrics, logs, and traces. See [About Dashboards](https://dash0.com/docs/dash0/dashboards/about-dashboards) for more details. The dashboard definition uses the [Perses Dashboard format](https://dash0.com/docs/dash0/dashboards/reference-dashboard-source-format).`,
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the dashboard, automatically generated on creation. Used to reference the dashboard for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the dashboard belongs to. Datasets are used to separate observability data within a Dash0 organization. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"dashboard_yaml": schema.StringAttribute{
				Description: "The dashboard definition in YAML format, following the [Perses Dashboard specification](https://dash0.com/docs/dash0/dashboards/reference-dashboard-source-format). The following `metadata.annotations` are supported: `dash0.com/sharing` (sharing settings) and `dash0.com/folder-path` (folder location). Changes to these annotations trigger a resource update; all other metadata annotations are managed by the server and ignored during drift detection.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(converter.AnnotationSharing, converter.AnnotationFolderPath),
				},
			},
		},
	}
}

func (r *DashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model dashboardModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var dashboardYaml interface{}
	err := yaml.Unmarshal([]byte(model.DashboardYaml.ValueString()), &dashboardYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Dashboard definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.DashboardYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert dashboard YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateDashboard(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create dashboard, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a dashboard resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *DashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state dashboardModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetDashboard(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read dashboard, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a dashboard resource")

	// Compare the current state with the retrieved dashboard
	if state.DashboardYaml.ValueString() != "" {
		stateYAML := state.DashboardYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, []string{converter.AnnotationSharing, converter.AnnotationFolderPath})
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Dashboard Comparison Error",
				fmt.Sprintf("Error comparing dashboards: %s. Using API response as source of truth.", err),
			)
			state.DashboardYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Dashboard has changed, updating state")
			state.DashboardYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Dashboard is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.DashboardYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *DashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state dashboardModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan dashboardModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var dashboardYaml interface{}
	err := yaml.Unmarshal([]byte(plan.DashboardYaml.ValueString()), &dashboardYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Dashboard definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.DashboardYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert dashboard YAML to JSON: %s", err))
		return
	}

	// Update the existing dashboard (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	err = r.client.UpdateDashboard(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update dashboard, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a dashboard resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dashboardModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteDashboard(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete dashboard, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a dashboard resource")
}

// ImportState function is required for resources that support import
func (r *DashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 2 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in the format 'dataset,origin'. Got: %s", req.ID),
		)
		return
	}

	dataset := idParts[0]
	origin := idParts[1]

	apiResponseJSON, err := r.client.GetDashboard(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Dashboard",
			fmt.Sprintf("Could not get dashboard with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_yaml"), apiResponseJSON)...)
}
