package provider

import (
	"context"
	"fmt"
	"github.com/google/uuid"

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
	_ resource.Resource              = &dashboardResource{}
	_ resource.ResourceWithConfigure = &dashboardResource{}
)

// NewDashboardResource is a helper function to simplify the provider implementation.
func NewDashboardResource() resource.Resource {
	return &dashboardResource{}
}

// dashboardResource is the resource implementation.
type dashboardResource struct {
	client *dash0Client
}

type dashboardResourceModel struct {
	Origin                  types.String `tfsdk:"origin"`
	Dataset                 types.String `tfsdk:"dataset"`
	DashboardDefinitionYaml types.String `tfsdk:"dashboard_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *dashboardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*dash0Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *dash0Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *dashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

func (r *dashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
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

func (r *dashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model dashboardResourceModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var dashboardYaml interface{}
	err := yaml.Unmarshal([]byte(model.DashboardDefinitionYaml.ValueString()), &dashboardYaml)
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

func (r *dashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state dashboardResourceModel
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

	// Set refreshed state
	state.DashboardDefinitionYaml = dashboard.DashboardDefinitionYaml

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *dashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state dashboardResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan dashboardResourceModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var dashboardYaml interface{}
	err := yaml.Unmarshal([]byte(plan.DashboardDefinitionYaml.ValueString()), &dashboardYaml)
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

func (r *dashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dashboardResourceModel
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
