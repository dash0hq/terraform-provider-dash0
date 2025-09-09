package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"

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

// Configure adds the provider configured client to the resource.
func (r *DashboardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected dash0ClientInterface, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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
		Description: "Manages a Dash0 Dashboard (in Perses format).",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "Identifier of the dashboard.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset for which the dashboard is created.",
				Required:    true,
			},
			"dashboard_yaml": schema.StringAttribute{
				Description: "The dashboard definition in YAML format (Perses Dashboard format).",
				Required:    true,
			},
		},
	}
}

func (r *DashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model model.Dashboard
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

	err = r.client.CreateDashboard(ctx, model)
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
	var state model.Dashboard
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboard, err := r.client.GetDashboard(ctx, state.Dataset.ValueString(), state.Origin.ValueString())
	if err != nil {
		// Handle 404 case by returning an empty state
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read dashboard, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a dashboard resource")

	// Compare the current state with the retrieved dashboard
	// Only update state if there's a significant change (ignoring certain fields)
	if state.DashboardYaml.ValueString() != "" {
		equivalent, err := converter.ResourceYAMLEquivalent(state.DashboardYaml.ValueString(), dashboard.DashboardYaml.ValueString())
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Dashboard Comparison Error",
				fmt.Sprintf("Error comparing dashboards: %s. Using API response as source of truth.", err),
			)
			// Fall back to updating with API response on error
			state.DashboardYaml = dashboard.DashboardYaml
		} else if !equivalent {
			// Only update if dashboards are not equivalent
			tflog.Debug(ctx, "Dashboard has changed, updating state")
			state.DashboardYaml = dashboard.DashboardYaml
		} else {
			tflog.Debug(ctx, "Dashboard is equivalent, ignoring changes in metadata fields")
			// Keep the current state since the dashboards are equivalent
		}
	} else {
		// If there's no current dashboard YAML, use the one from the API
		state.DashboardYaml = dashboard.DashboardYaml
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *DashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state model.Dashboard
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan model.Dashboard
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

	// Check if dataset has changed
	datasetChanged := state.Dataset.ValueString() != plan.Dataset.ValueString()

	if datasetChanged {
		tflog.Info(ctx, fmt.Sprintf("Dataset changed from %s to %s, recreating dashboard",
			state.Dataset.ValueString(), plan.Dataset.ValueString()))

		// Delete the existing dashboard
		err := r.client.DeleteDashboard(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to delete old dashboard when changing dataset, got error: %s", err))
			return
		}

		// Create a new dashboard in the new dataset
		err = r.client.CreateDashboard(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to create dashboard in new dataset, got error: %s", err))
			return
		}

		tflog.Trace(ctx, "recreated dashboard resource in new dataset")
	} else {
		// Standard update (same dataset)
		err := r.client.UpdateDashboard(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to update dashboard, got error: %s", err))
			return
		}

		tflog.Trace(ctx, "updated dashboard resource")
	}

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *DashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model.Dashboard
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
	// Expect the import ID in the format "origin,dataset"
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

	// Retrieve the dashboard using the client
	dashboard, err := r.client.GetDashboard(ctx, dataset, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Dashboard",
			fmt.Sprintf("Could not get dashboard with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Set the state with values from the imported dashboard
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_yaml"), dashboard.DashboardYaml)...)
}
