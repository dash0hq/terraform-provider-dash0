package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &dashboardResource{}
	_ resource.ResourceWithConfigure   = &dashboardResource{}
	_ resource.ResourceWithImportState = &dashboardResource{}
)

// NewDashboardResource is a helper function to simplify the provider implementation.
func NewDashboardResource() resource.Resource {
	return &dashboardResource{}
}

// dashboardResource is the resource implementation.
type dashboardResource struct {
	client *dash0Client
}

// dashboardResourceModel maps the resource schema data.
type dashboardResourceModel struct {
	ID                      types.String `tfsdk:"id"`
	Dataset                 types.String `tfsdk:"dataset"`
	DashboardDefinitionYaml types.String `tfsdk:"dashboard_definition_yaml"`
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

// Metadata returns the resource type name.
func (r *dashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

// Schema defines the schema for the resource.
func (r *dashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Dashboard (in Perses format).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identifier of the dashboard.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset for which the dashboard is created.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("default"),
			},
			"dashboard_definition_yaml": schema.StringAttribute{
				Description: "The dashboard definition in YAML format (Perses Dashboard format).",
				Required:    true,
			},
		},
	}
}

// Create creates the resource and sets the initial Terraform state.
func (r *dashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan dashboardResourceModel
	diags := req.Plan.Get(ctx, &plan)
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

	// ID will be set by CreateDashboard from API response

	// API call to create the dashboard
	id, err := r.client.CreateDashboard(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create dashboard, got error: %s", err))
		return
	}
	
	// Set the ID returned from the API
	plan.ID = types.StringValue(id)

	tflog.Trace(ctx, "created a dashboard resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Read refreshes the Terraform state with the latest data.
func (r *dashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state dashboardResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// API call to get the dashboard details
	dashboard, err := r.client.GetDashboard(ctx, state.ID.ValueString())
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

// Update updates the resource and sets the updated Terraform state on success.
func (r *dashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan dashboardResourceModel
	diags := req.Plan.Get(ctx, &plan)
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

	// API call to update the dashboard
	id, err := r.client.UpdateDashboard(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update dashboard, got error: %s", err))
		return
	}
	
	// Set the ID returned from the API
	plan.ID = types.StringValue(id)

	tflog.Trace(ctx, "updated a dashboard resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *dashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state dashboardResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// API call to delete the dashboard
	_, err := r.client.DeleteDashboard(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete dashboard, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a dashboard resource")
}

// ImportState imports an existing resource into Terraform state.
func (r *dashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
