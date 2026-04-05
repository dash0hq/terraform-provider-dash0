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

// recordingRuleGroupModel is the Terraform state model for a recording rule group resource.
type recordingRuleGroupModel struct {
	Origin                 types.String `tfsdk:"origin"`
	Dataset                types.String `tfsdk:"dataset"`
	RecordingRuleGroupYaml types.String `tfsdk:"recording_rule_group_yaml"`
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
			fmt.Sprintf("Expected client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
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
		Description: `Manages a Dash0 Recording Rule Group. Recording rules precompute frequently used or computationally expensive PromQL expressions and save the results as new time series. This is useful for dashboards and alerts that query the same expression repeatedly. The definition uses the [Prometheus recording rule format](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/).`,
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the recording rule group, automatically generated on creation. Used to reference the recording rule group for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the recording rule group belongs to. Datasets are used to separate observability data within a Dash0 organization. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"recording_rule_group_yaml": schema.StringAttribute{
				Description: "The recording rule group definition in YAML format, following the [Prometheus recording rule specification](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

func (r *RecordingRuleGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model recordingRuleGroupModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var yamlContent interface{}
	err := yaml.Unmarshal([]byte(model.RecordingRuleGroupYaml.ValueString()), &yamlContent)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Recording rule group definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.RecordingRuleGroupYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert recording rule group YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateRecordingRuleGroup(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create recording rule group, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a recording rule group resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state recordingRuleGroupModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetRecordingRuleGroup(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read recording rule group, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a recording rule group resource")

	// Compare the current state with the retrieved recording rule group
	if state.RecordingRuleGroupYaml.ValueString() != "" {
		stateYAML := state.RecordingRuleGroupYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored...)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Recording Rule Group Comparison Error",
				fmt.Sprintf("Error comparing recording rule groups: %s. Using API response as source of truth.", err),
			)
			state.RecordingRuleGroupYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Recording rule group has changed, updating state")
			state.RecordingRuleGroupYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Recording rule group is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.RecordingRuleGroupYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state recordingRuleGroupModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan recordingRuleGroupModel
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

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.RecordingRuleGroupYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert recording rule group YAML to JSON: %s", err))
		return
	}

	// Update the existing recording rule group (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	err = r.client.UpdateRecordingRuleGroup(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
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
	var state recordingRuleGroupModel
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

	apiResponseJSON, err := r.client.GetRecordingRuleGroup(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Recording Rule Group",
			fmt.Sprintf("Could not get recording rule group with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("recording_rule_group_yaml"), apiResponseJSON)...)
}
