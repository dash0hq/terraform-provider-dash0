package provider

import (
	"context"
	"fmt"
	"strings"

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
	_ resource.Resource                = &viewResource{}
	_ resource.ResourceWithConfigure   = &viewResource{}
	_ resource.ResourceWithImportState = &viewResource{}
)

// NewViewResource is a helper function to simplify the provider implementation.
func NewViewResource() resource.Resource {
	return &viewResource{}
}

// viewResource is the resource implementation.
type viewResource struct {
	client dash0ClientInterface
}

type viewResourceModel struct {
	Origin   types.String `tfsdk:"origin"`
	Dataset  types.String `tfsdk:"dataset"`
	ViewYaml types.String `tfsdk:"view_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *viewResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(dash0ClientInterface)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected dash0ClientInterface, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *viewResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_view"
}

func (r *viewResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 View.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "Identifier of the view.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset for which the view is created.",
				Required:    true,
			},
			"view_yaml": schema.StringAttribute{
				Description: "The view definition in YAML format.",
				Required:    true,
			},
		},
	}
}

func (r *viewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model viewResourceModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var viewYaml interface{}
	err := yaml.Unmarshal([]byte(model.ViewYaml.ValueString()), &viewYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("view definition is not valid YAML: %s", err),
		)
		return
	}

	err = r.client.CreateView(ctx, model)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create view, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a view resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *viewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state viewResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	check, err := r.client.GetView(ctx, state.Dataset.ValueString(), state.Origin.ValueString())
	if err != nil {
		// Handle 404 case by returning an empty state
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read view, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a view resource")

	// Compare the current state with the retrieved view
	// Only update state if there's a significant change (ignoring certain fields)
	if state.ViewYaml.ValueString() != "" {
		equivalent, err := ResourceYAMLEquivalent(state.ViewYaml.ValueString(), check.ViewYaml.ValueString())
		if err != nil {
			resp.Diagnostics.AddWarning(
				"View Comparison Error",
				fmt.Sprintf("Error comparing views: %s. Using API response as source of truth.", err),
			)
			// Fall back to updating with API response on error
			state.ViewYaml = check.ViewYaml
		} else if !equivalent {
			// Only update if view are not equivalent
			tflog.Debug(ctx, "view has changed, updating state")
			state.ViewYaml = check.ViewYaml
		} else {
			tflog.Debug(ctx, "view is equivalent, ignoring changes in metadata fields")
			// Keep the current state since the views are equivalent
		}
	} else {
		// If there's no current views YAML, use the one from the API
		state.ViewYaml = check.ViewYaml
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *viewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// get current state
	var state viewResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan viewResourceModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var viewYaml interface{}
	err := yaml.Unmarshal([]byte(plan.ViewYaml.ValueString()), &viewYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("View definition is not valid YAML: %s", err),
		)
		return
	}

	// Check if dataset has changed
	datasetChanged := state.Dataset.ValueString() != plan.Dataset.ValueString()

	if datasetChanged {
		// Delete from old dataset
		err = r.client.DeleteView(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete view from old dataset, got error: %s", err))
			return
		}
		// Create in new dataset
		plan.Origin = state.Origin
		err = r.client.CreateView(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create view in new dataset, got error: %s", err))
			return
		}
	} else {
		// Update the existing view
		plan.Origin = state.Origin
		err = r.client.UpdateView(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update view, got error: %s", err))
			return
		}
	}

	tflog.Trace(ctx, "updated a view resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *viewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Get current state
	var state viewResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteView(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete view, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a view resource")
}

// ImportState function is required for resources that support import
func (r *viewResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Expect the import ID in the format "dataset,origin"
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

	// Retrieve the view using the client
	check, err := r.client.GetView(ctx, dataset, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing view",
			fmt.Sprintf("Could not get view with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Set the resource state with the retrieved view
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), check.Origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), check.Dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("view_yaml"), check.ViewYaml)...)
}
