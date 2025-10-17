package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"

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
	_ resource.Resource                = &SyntheticCheckResource{}
	_ resource.ResourceWithConfigure   = &SyntheticCheckResource{}
	_ resource.ResourceWithImportState = &SyntheticCheckResource{}
)

// NewSyntheticCheckResource is a helper function to simplify the provider implementation.
func NewSyntheticCheckResource() resource.Resource {
	return &SyntheticCheckResource{}
}

// SyntheticCheckResource is the resource implementation.
type SyntheticCheckResource struct {
	client client.Client
}

// Configure adds the provider configured client to the resource.
func (r *SyntheticCheckResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SyntheticCheckResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_synthetic_check"
}

func (r *SyntheticCheckResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Synthetic Check.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "Identifier of the synthetic check.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset for which the synthetic check is created.",
				Required:    true,
			},
			"synthetic_check_yaml": schema.StringAttribute{
				Description: "The synthetic check definition in YAML format.",
				Required:    true,
			},
		},
	}
}

func (r *SyntheticCheckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model.SyntheticCheck
	diags := req.Plan.Get(ctx, &m)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	m.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var checkYaml interface{}
	err := yaml.Unmarshal([]byte(m.SyntheticCheckYaml.ValueString()), &checkYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Synthetic check definition is not valid YAML: %s", err),
		)
		return
	}

	err = r.client.CreateSyntheticCheck(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create synthetic check, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a synthetic check resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, m)
	resp.Diagnostics.Append(diags...)
}

func (r *SyntheticCheckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state model.SyntheticCheck
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	check, err := r.client.GetSyntheticCheck(ctx, state.Dataset.ValueString(), state.Origin.ValueString())
	if err != nil {
		// Handle 404 case by returning an empty state
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read synthetic check, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a synthetic check resource")

	// Compare the current state with the retrieved synthetic check
	// Only update state if there's a significant change (ignoring certain fields)
	if state.SyntheticCheckYaml.ValueString() != "" {
		equivalent, err := converter.ResourceYAMLEquivalent(state.SyntheticCheckYaml.ValueString(), check.SyntheticCheckYaml.ValueString())
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Synthetic Check Comparison Error",
				fmt.Sprintf("Error comparing synthetic checks: %s. Using API response as source of truth.", err),
			)
			// Fall back to updating with API response on error
			state.SyntheticCheckYaml = check.SyntheticCheckYaml
		} else if !equivalent {
			// Only update if synthetic checks are not equivalent
			tflog.Debug(ctx, "Synthetic check has changed, updating state")
			state.SyntheticCheckYaml = check.SyntheticCheckYaml
		} else {
			tflog.Debug(ctx, "Synthetic check is equivalent, ignoring changes in metadata fields")
			// Keep the current state since the synthetic checks are equivalent
		}
	} else {
		// If there's no current synthetic check YAML, use the one from the API
		state.SyntheticCheckYaml = check.SyntheticCheckYaml
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *SyntheticCheckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state model.SyntheticCheck
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan model.SyntheticCheck
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var checkYaml interface{}
	err := yaml.Unmarshal([]byte(plan.SyntheticCheckYaml.ValueString()), &checkYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Synthetic check definition is not valid YAML: %s", err),
		)
		return
	}

	// Check if dataset has changed
	datasetChanged := state.Dataset.ValueString() != plan.Dataset.ValueString()

	if datasetChanged {
		// Delete from old dataset
		err = r.client.DeleteSyntheticCheck(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete synthetic check from old dataset, got error: %s", err))
			return
		}
		// Create in new dataset
		plan.Origin = state.Origin
		err = r.client.CreateSyntheticCheck(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create synthetic check in new dataset, got error: %s", err))
			return
		}
	} else {
		// Update the existing synthetic check
		plan.Origin = state.Origin
		err = r.client.UpdateSyntheticCheck(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update synthetic check, got error: %s", err))
			return
		}
	}

	tflog.Trace(ctx, "updated a synthetic check resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *SyntheticCheckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Get current state
	var state model.SyntheticCheck
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSyntheticCheck(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete synthetic check, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a synthetic check resource")
}

// ImportState function is required for resources that support import
func (r *SyntheticCheckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	// Retrieve the synthetic check using the client
	check, err := r.client.GetSyntheticCheck(ctx, dataset, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Synthetic Check",
			fmt.Sprintf("Could not get synthetic check with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Set the resource state with the retrieved synthetic check
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), check.Origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), check.Dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("synthetic_check_yaml"), check.SyntheticCheckYaml)...)
}
