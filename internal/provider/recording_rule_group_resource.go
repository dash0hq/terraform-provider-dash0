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
	_ resource.Resource                = &RecordingRuleGroupResource{}
	_ resource.ResourceWithConfigure   = &RecordingRuleGroupResource{}
	_ resource.ResourceWithImportState = &RecordingRuleGroupResource{}
)

// NewRecordingRuleGroupResource is a helper function to simplify the provider implementation.
func NewRecordingRuleGroupResource() resource.Resource {
	return &RecordingRuleGroupResource{}
}

// RecordingRuleGroupResource is the resource implementation.
type RecordingRuleGroupResource struct {
	client client.Client
}

// Configure adds the provider configured client to the resource.
func (r *RecordingRuleGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RecordingRuleGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recording_rule_group"
}

func (r *RecordingRuleGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Recording Rule Group.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "Identifier of the recording rule group.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset for which the recording rule group is created.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"recording_rule_group_yaml": schema.StringAttribute{
				Description: "The recording rule group definition in YAML format (Dash0 CRD format).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

func (r *RecordingRuleGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m model.RecordingRuleGroup
	diags := req.Plan.Get(ctx, &m)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	m.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var yamlContent interface{}
	err := yaml.Unmarshal([]byte(m.RecordingRuleGroupYaml.ValueString()), &yamlContent)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Recording rule group definition is not valid YAML: %s", err),
		)
		return
	}

	err = r.client.CreateRecordingRuleGroup(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create recording rule group, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a recording rule group resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, m)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state model.RecordingRuleGroup
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.GetRecordingRuleGroup(ctx, state.Dataset.ValueString(), state.Origin.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read recording rule group, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a recording rule group resource")

	// Compare the current state with the retrieved recording rule group
	// Only update state if there's a significant change (ignoring certain fields)
	if state.RecordingRuleGroupYaml.ValueString() != "" {
		stateYAML := state.RecordingRuleGroupYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, group.RecordingRuleGroupYaml.ValueString(), additionalIgnored...)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Recording Rule Group Comparison Error",
				fmt.Sprintf("Error comparing recording rule groups: %s. Using API response as source of truth.", err),
			)
			state.RecordingRuleGroupYaml = group.RecordingRuleGroupYaml
		} else if !equivalent {
			tflog.Debug(ctx, "recording rule group has changed, updating state")
			state.RecordingRuleGroupYaml = group.RecordingRuleGroupYaml
		} else {
			tflog.Debug(ctx, "recording rule group is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.RecordingRuleGroupYaml = group.RecordingRuleGroupYaml
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state model.RecordingRuleGroup
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan model.RecordingRuleGroup
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var yamlContent interface{}
	err := yaml.Unmarshal([]byte(plan.RecordingRuleGroupYaml.ValueString()), &yamlContent)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Recording rule group definition is not valid YAML: %s", err),
		)
		return
	}

	// Update the existing recording rule group (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	err = r.client.UpdateRecordingRuleGroup(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update recording rule group, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a recording rule group resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Get current state
	var state model.RecordingRuleGroup
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteRecordingRuleGroup(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete recording rule group, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a recording rule group resource")
}

// ImportState function is required for resources that support import
func (r *RecordingRuleGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	// Retrieve the recording rule group using the client
	group, err := r.client.GetRecordingRuleGroup(ctx, dataset, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Recording Rule Group",
			fmt.Sprintf("Could not get recording rule group with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Set the resource state with the retrieved recording rule group
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), group.Origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), group.Dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("recording_rule_group_yaml"), group.RecordingRuleGroupYaml)...)
}
