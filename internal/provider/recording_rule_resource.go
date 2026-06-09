package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &RecordingRuleResource{}
	_ resource.ResourceWithConfigure   = &RecordingRuleResource{}
	_ resource.ResourceWithImportState = &RecordingRuleResource{}
)

// NewRecordingRuleResource is a helper function to simplify the provider implementation.
func NewRecordingRuleResource() resource.Resource {
	return &RecordingRuleResource{}
}

// RecordingRuleResource is the resource implementation.
type RecordingRuleResource struct {
	client client.Client
}

// recordingRuleModel is the Terraform state model for a recording rule resource.
type recordingRuleModel struct {
	Origin            types.String `tfsdk:"origin"`
	ID                types.String `tfsdk:"id"`
	Dataset           types.String `tfsdk:"dataset"`
	RecordingRuleYaml types.String `tfsdk:"recording_rule_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *RecordingRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RecordingRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_recording_rule"
}

func (r *RecordingRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Manages a Dash0 Recording Rule. Recording rules pre-compute frequently needed or computationally expensive PromQL expressions and save the results as new time series. See [Manage Check Rules as Code](https://dash0.com/docs/dash0/monitoring/alerting/manage-check-rules-as-code) for more details — recording rules share the same Prometheus rule format and management surface as alert check rules. The recording rule definition uses the [Prometheus Rule format](https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1.PrometheusRule).

More information on how Prometheus rules are mapped to Dash0 recording rules can be found in the [Dash0 Operator documentation](https://dash0.com/docs/dash0/monitoring/kubernetes/about-kubernetes#managing-dash0-recording-rules).`,

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the recording rule, automatically generated on creation. Used to reference the recording rule for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned identifier of the recording rule group, resolved by the provider after creation. The value has the form `recording_rule_group_<ulid>` (a ULID, not a UUID) because recording rules live inside groups and the API addresses the whole group. Recording rules are not addressable in the Dash0 web app, so no `url` is exposed.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the recording rule belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"recording_rule_yaml": schema.StringAttribute{
				Description: "The recording rule definition in YAML format, following the [Prometheus recording rule specification](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

// resolveRecordingRule populates the recording rule's server-assigned id on
// the model by looking it up via the list endpoint. The id is best-effort
// metadata: failures are surfaced as warnings and leave the attribute null
// rather than failing the operation.
func (r *RecordingRuleResource) resolveRecordingRule(ctx context.Context, model *recordingRuleModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveRecordingRule(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve recording rule metadata",
			fmt.Sprintf("The recording rule was saved successfully, but its id could not be determined: %s", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

func (r *RecordingRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model recordingRuleModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var recordingRuleYaml interface{}
	err := yaml.Unmarshal([]byte(model.RecordingRuleYaml.ValueString()), &recordingRuleYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Recording rule definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.RecordingRuleYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert recording rule YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateRecordingRule(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create recording rule, got error: %s", err))
		return
	}

	// Resolve the id for the newly created recording rule (best-effort).
	r.resolveRecordingRule(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a recording rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state recordingRuleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetRecordingRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read recording rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a recording rule resource")

	// Compare the current state with the retrieved recording rule
	if state.RecordingRuleYaml.ValueString() != "" {
		stateYAML := state.RecordingRuleYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, nil)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Recording Rule Comparison Error",
				fmt.Sprintf("Error comparing recording rules: %s. Using API response as source of truth.", err),
			)
			state.RecordingRuleYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Recording rule has changed, updating state")
			state.RecordingRuleYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Recording rule is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.RecordingRuleYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state recordingRuleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan recordingRuleModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var recordingRuleYaml interface{}
	err := yaml.Unmarshal([]byte(plan.RecordingRuleYaml.ValueString()), &recordingRuleYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Recording rule definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.RecordingRuleYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert recording rule YAML to JSON: %s", err))
		return
	}

	// Update the existing recording rule (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The recording rule's identifier is immutable, so the id never changes on
	// update; carry it from state instead of re-resolving it via the API.
	plan.ID = state.ID
	err = r.client.UpdateRecordingRule(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update recording rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a recording rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *RecordingRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state recordingRuleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteRecordingRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete recording rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a recording rule resource")
}

// ImportState function is required for resources that support import
func (r *RecordingRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetRecordingRule(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Recording Rule",
			fmt.Sprintf("Could not get recording rule with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("recording_rule_yaml"), apiResponseJSON)...)

	// Resolve the id (best-effort).
	model := recordingRuleModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveRecordingRule(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
