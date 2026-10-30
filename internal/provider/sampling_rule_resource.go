package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	dash0 "github.com/dash0hq/dash0-api-client-go"
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
	_ resource.Resource                   = &SamplingRuleResource{}
	_ resource.ResourceWithConfigure      = &SamplingRuleResource{}
	_ resource.ResourceWithImportState    = &SamplingRuleResource{}
	_ resource.ResourceWithValidateConfig = &SamplingRuleResource{}
)

// NewSamplingRuleResource is a helper function to simplify the provider implementation.
func NewSamplingRuleResource() resource.Resource {
	return &SamplingRuleResource{}
}

// SamplingRuleResource is the resource implementation.
type SamplingRuleResource struct {
	client client.Client
	// defaultDataset is the provider-level default dataset, inherited by this
	// resource's `dataset` attribute when it is omitted from configuration.
	defaultDataset string
}

// samplingRuleModel is the Terraform state model for a sampling rule resource.
// There is no `url` attribute: sampling rules have no deeplink asset type, so
// there is no web app page to link to.
type samplingRuleModel struct {
	Origin           types.String `tfsdk:"origin"`
	ID               types.String `tfsdk:"id"`
	Dataset          types.String `tfsdk:"dataset"`
	SamplingRuleYaml types.String `tfsdk:"sampling_rule_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *SamplingRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(resourceProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected provider.resourceProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = data.client
	r.defaultDataset = data.defaultDataset
}

func (r *SamplingRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sampling_rule"
}

func (r *SamplingRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Sampling Rule. Sampling rules decide which traces Dash0 keeps: each rule " +
			"keeps the traces that match its conditions, such as traces with an error span, traces that match an " +
			"OTTL expression, or a probabilistic share of all traces. See " +
			"[Sample Traces](https://dash0.com/docs/dash0/telemetry/signal-control/sample-traces) for the " +
			"condition kinds and their limits.\n\n" +
			"~> **Note:** Creating a sampling rule requires an auth token with the admin role.\n\n" +
			"~> **Note:** `spec.enabled` is required. The provider always sends a value for it, so an " +
			"omitted field would silently create a disabled rule and later switch off one enabled elsewhere.\n\n" +
			"Changing `dataset` destroys the rule and re-creates it under a new origin.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the sampling rule, automatically generated on creation. Used to reference the rule for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned identifier of the sampling rule, resolved by the provider after creation.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the sampling rule belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. If omitted, the provider-level `dataset` default is used (see the provider's `dataset` attribute). Changing this value forces the resource to be recreated under a new origin.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"sampling_rule_yaml": schema.StringAttribute{
				Description: "The sampling rule definition in YAML format. The YAML must declare `kind: Dash0Sampling`, an explicit `spec.enabled`, and `spec.conditions`, a tree of `probabilistic`, `ottl`, `error` and `and` conditions. " +
					"`spec.display.name` and `spec.rateLimit` are optional. " +
					"`metadata.labels` are ignored during drift detection, so the provider warns when `metadata.labels.custom` is set.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

// ValidateConfig runs plan-time validation for sampling_rule_yaml so users see
// problems on `terraform plan` rather than on the subsequent apply.
//
// Only `kind` is checked, not `apiVersion`: SamplingDefinition has no
// apiVersion field, so there is no version to dispatch on.
func (r *SamplingRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model samplingRuleModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.SamplingRuleYaml.IsNull() || model.SamplingRuleYaml.IsUnknown() {
		return
	}

	parsed := parseAndValidateSamplingRuleYAML(model.SamplingRuleYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	warnIfCustomLabelsSet(parsed, "sampling_rule_yaml", &resp.Diagnostics)
}

// parseAndValidateSamplingRuleYAML parses the user's document and enforces
// every invariant the provider depends on, appending errors to diags. It
// returns nil when any of them fails.
//
// This is shared by ValidateConfig, Create, and Update rather than living in
// ValidateConfig alone. ValidateConfig cannot see a value that is unknown at
// plan time — the ordinary case when the YAML interpolates another resource's
// computed attribute — and returns early. Create and Update always have a
// known value, so they are the backstop that an unknown cannot slip past.
func parseAndValidateSamplingRuleYAML(document string, diags *diag.Diagnostics) map[string]interface{} {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		diags.AddAttributeError(
			path.Root("sampling_rule_yaml"),
			"Invalid YAML in sampling_rule_yaml",
			fmt.Sprintf("sampling_rule_yaml is not valid YAML: %s", err),
		)
		return nil
	}

	// An empty, whitespace-only, or explicitly null document unmarshals into a
	// nil map without error, so guard before the shape check.
	if parsed == nil {
		diags.AddAttributeError(
			path.Root("sampling_rule_yaml"),
			"sampling_rule_yaml is empty or not a YAML mapping",
			"sampling_rule_yaml must be a YAML mapping following the Dash0Sampling envelope (kind, metadata, spec).",
		)
		return nil
	}

	// The Dash0 operator's Dash0SamplingRule custom resource has a different
	// shape, so a document copied from a cluster is rejected here by name.
	if kind, _ := parsed["kind"].(string); kind != string(dash0.Dash0Sampling) {
		diags.AddAttributeError(
			path.Root("sampling_rule_yaml"),
			"sampling_rule_yaml is missing or has the wrong kind",
			fmt.Sprintf("sampling_rule_yaml must declare `kind: Dash0Sampling`; got %q. The dash0_sampling_rule "+
				"resource only manages the Dash0Sampling kind of the Dash0 API.", kind),
		)
		return nil
	}

	// SamplingSpec.Enabled is a non-pointer bool with no omitempty.
	errorIfEnabledAbsent(parsed, "sampling_rule_yaml", "sampling rule", diags)
	if diags.HasError() {
		return nil
	}
	return parsed
}

// resolveSamplingRule populates the rule's server-assigned id on the model by
// looking it up via the list endpoint. The id is best-effort metadata:
// failures are surfaced as warnings and leave the attribute null rather than
// failing the operation.
func (r *SamplingRuleResource) resolveSamplingRule(ctx context.Context, model *samplingRuleModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveSamplingRule(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve sampling rule metadata",
			fmt.Sprintf("The sampling rule's id could not be determined: %s. It is left null; the sampling rule itself is unaffected.", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

func (r *SamplingRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model samplingRuleModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())
	if model.Dataset.IsNull() || model.Dataset.IsUnknown() {
		model.Dataset = types.StringValue(r.defaultDataset)
	}

	// Re-validate here, not just in ValidateConfig: the value is always known by
	// Create, so this is the point an unknown-at-plan-time document cannot slip
	// past. See parseAndValidateSamplingRuleYAML.
	parsed := parseAndValidateSamplingRuleYAML(model.SamplingRuleYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	warnIfCustomLabelsSet(parsed, "sampling_rule_yaml", &resp.Diagnostics)

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.SamplingRuleYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert sampling rule YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateSamplingRule(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create sampling rule, got error: %s", err))
		return
	}

	// Resolve the id for the newly created rule (best-effort).
	r.resolveSamplingRule(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a sampling rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *SamplingRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state samplingRuleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetSamplingRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if removedOutOfBandInDataset(ctx, resp, r.client, state.Dataset.ValueString(), "Sampling rule", state.Origin.ValueString(), apiResponseJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read sampling rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a sampling rule resource")

	// Compare the current state with the retrieved sampling rule
	if state.SamplingRuleYaml.ValueString() != "" {
		stateYAML := state.SamplingRuleYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, nil)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Sampling Rule Comparison Error",
				fmt.Sprintf("Error comparing sampling rules: %s. Using API response as source of truth.", err),
			)
			state.SamplingRuleYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Sampling rule has changed, updating state")
			state.SamplingRuleYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Sampling rule is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.SamplingRuleYaml = types.StringValue(apiResponseJSON)
	}

	// Self-heal a null id: a transient resolve failure at Create must not leave
	// the attribute null for the lifetime of the resource.
	if state.ID.IsNull() || state.ID.IsUnknown() {
		r.resolveSamplingRule(ctx, &state, &resp.Diagnostics)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *SamplingRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state samplingRuleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan samplingRuleModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-validate here for the same reason as in Create: ValidateConfig cannot
	// see a document that was unknown at plan time.
	parsed := parseAndValidateSamplingRuleYAML(plan.SamplingRuleYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	warnIfCustomLabelsSet(parsed, "sampling_rule_yaml", &resp.Diagnostics)

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.SamplingRuleYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert sampling rule YAML to JSON: %s", err))
		return
	}

	// Update the existing rule (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The rule's identifier is immutable, so the id does not change on update;
	// carry it from state instead of re-resolving it via the API.
	plan.ID = state.ID
	err = r.client.UpdateSamplingRule(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update sampling rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a sampling rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *SamplingRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state samplingRuleModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSamplingRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Sampling rule %s was already gone on the server; treating delete as successful", state.Origin.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete sampling rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a sampling rule resource")
}

// ImportState function is required for resources that support import
func (r *SamplingRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetSamplingRule(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Sampling Rule",
			fmt.Sprintf("Could not get sampling rule with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("sampling_rule_yaml"), apiResponseJSON)...)

	// Resolve the id (best-effort).
	model := samplingRuleModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveSamplingRule(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
