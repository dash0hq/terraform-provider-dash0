package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	customplanmodifier "github.com/dash0hq/terraform-provider-dash0/internal/provider/planmodifier"
)

var (
	_ resource.Resource                   = &TelemetryTransformationRuleResource{}
	_ resource.ResourceWithConfigure      = &TelemetryTransformationRuleResource{}
	_ resource.ResourceWithImportState    = &TelemetryTransformationRuleResource{}
	_ resource.ResourceWithValidateConfig = &TelemetryTransformationRuleResource{}
)

const telemetryTransformationRuleYAMLAttribute = "telemetry_transformation_rule_yaml"

type TelemetryTransformationRuleResource struct {
	client         client.Client
	defaultDataset string
}

type telemetryTransformationRuleModel struct {
	Origin                          types.String `tfsdk:"origin"`
	ID                              types.String `tfsdk:"id"`
	Dataset                         types.String `tfsdk:"dataset"`
	TelemetryTransformationRuleYAML types.String `tfsdk:"telemetry_transformation_rule_yaml"`
}

func NewTelemetryTransformationRuleResource() resource.Resource {
	return &TelemetryTransformationRuleResource{}
}

func (r *TelemetryTransformationRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_telemetry_transformation_rule"
}

func (r *TelemetryTransformationRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 telemetry transformation rule, which applies named OTTL transformations to telemetry at ingestion. " +
			"Creation uses POST; updates replace an existing rule with PUT and do not create missing rules. " +
			"Changing `dataset` destroys the rule and creates a new one with a fresh origin.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A tf_-prefixed identifier generated on creation. For imported rules, this is the origin or ID supplied at import. Used for subsequent reads, updates, and deletion.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned identifier of the telemetry transformation rule.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset identifier, not its display name. If omitted, the provider-level dataset default is used. Changing this value replaces the rule with a new origin.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			telemetryTransformationRuleYAMLAttribute: schema.StringAttribute{
				Description: "The rule as YAML with `kind: Dash0TelemetryTransformationRule`, `metadata`, and `spec`. " +
					"Set `spec.enabled` explicitly, the signal and context, and named `spec.transformations` with ordered statements. " +
					"Priority defaults to 200; 0, 100, 200, 300, and 400 are supported. No apiVersion is required. " +
					"Stored `spec.examples` are managed as part of the rule: omitting them removes them on update. Export the full rule with GET, not list, to keep examples. " +
					"Server metadata does not cause drift. A supplied `dash0.com/updated-at` annotation is preserved for concurrency checks and a stale value fails the update. " +
					"Remove that annotation from exported files for ongoing Terraform management without a fixed concurrency token. " +
					"Examples persist as configuration, not retained telemetry; do not put sensitive data in them.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualComparing(converter.TelemetryTransformationRuleYAMLEquivalent),
				},
			},
		},
	}
}

func (r *TelemetryTransformationRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(resourceProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected provider.resourceProviderData, got: %T.", req.ProviderData))
		return
	}
	r.client = data.client
	r.defaultDataset = data.defaultDataset
}

func (r *TelemetryTransformationRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model telemetryTransformationRuleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.TelemetryTransformationRuleYAML.IsNull() || model.TelemetryTransformationRuleYAML.IsUnknown() {
		return
	}
	telemetryTransformationRuleJSON(model.TelemetryTransformationRuleYAML.ValueString(), &resp.Diagnostics)
}

func telemetryTransformationRuleJSON(document string, diags *diag.Diagnostics) string {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		diags.AddAttributeError(path.Root(telemetryTransformationRuleYAMLAttribute), "Invalid telemetry transformation rule YAML", err.Error())
		return ""
	}
	if kind, _ := parsed["kind"].(string); kind != string(dash0.TelemetryTransformationRuleDefinitionKindDash0TelemetryTransformationRule) {
		diags.AddAttributeError(path.Root(telemetryTransformationRuleYAMLAttribute), "Invalid telemetry transformation rule kind", "Declare kind: Dash0TelemetryTransformationRule in a YAML mapping.")
		return ""
	}
	errorIfEnabledAbsent(parsed, telemetryTransformationRuleYAMLAttribute, "telemetry transformation rule", diags)
	if diags.HasError() {
		return ""
	}
	body, err := converter.ConvertYAMLToJSON(document)
	if err != nil {
		diags.AddAttributeError(path.Root(telemetryTransformationRuleYAMLAttribute), "Invalid telemetry transformation rule YAML", err.Error())
		return ""
	}
	return body
}

func (r *TelemetryTransformationRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model telemetryTransformationRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := telemetryTransformationRuleJSON(model.TelemetryTransformationRuleYAML.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	model.Origin = types.StringValue("tf_" + uuid.New().String())
	if model.Dataset.IsNull() || model.Dataset.IsUnknown() {
		model.Dataset = types.StringValue(r.defaultDataset)
	}
	id, err := r.client.CreateTelemetryTransformationRule(ctx, model.Origin.ValueString(), body, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create telemetry transformation rule with origin %q: %s. If the response was lost, check this origin before trying again and import the rule if it was created.", model.Origin.ValueString(), err))
		return
	}
	model.ID = stringOrNull(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
	tflog.Trace(ctx, "created a telemetry transformation rule")
}

func (r *TelemetryTransformationRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state telemetryTransformationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := r.client.GetTelemetryTransformationRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if removedOutOfBandInDataset(ctx, resp, r.client, state.Dataset.ValueString(), "Telemetry transformation rule", state.Origin.ValueString(), body, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read telemetry transformation rule: %s", err))
		return
	}
	var rule dash0.TelemetryTransformationRuleDefinition
	if err := json.Unmarshal([]byte(body), &rule); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to decode telemetry transformation rule: %s", err))
		return
	}
	state.ID = stringOrNull(dash0.GetTelemetryTransformationRuleID(&rule))
	additionalIgnored := converter.FieldsAbsentFromYAML(state.TelemetryTransformationRuleYAML.ValueString(), converter.ConditionallyIgnoredFields)
	equivalent, err := converter.TelemetryTransformationRuleYAMLEquivalent(state.TelemetryTransformationRuleYAML.ValueString(), body, additionalIgnored)
	if err != nil {
		resp.Diagnostics.AddWarning("Telemetry Transformation Rule Comparison Error", fmt.Sprintf("Unable to compare rule definitions: %s. Using the API response.", err))
	}
	if err != nil || !equivalent {
		state.TelemetryTransformationRuleYAML = types.StringValue(body)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	tflog.Trace(ctx, "read a telemetry transformation rule")
}

func (r *TelemetryTransformationRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan telemetryTransformationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := telemetryTransformationRuleJSON(plan.TelemetryTransformationRuleYAML.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Origin = state.Origin
	plan.ID = state.ID
	if plan.Dataset.IsNull() || plan.Dataset.IsUnknown() {
		plan.Dataset = state.Dataset
	}
	if err := r.client.UpdateTelemetryTransformationRule(ctx, plan.Origin.ValueString(), body, plan.Dataset.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update telemetry transformation rule: %s", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	tflog.Trace(ctx, "updated a telemetry transformation rule")
}

func (r *TelemetryTransformationRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state telemetryTransformationRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteTelemetryTransformationRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString()); err != nil && !dash0.IsNotFound(err) {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete telemetry transformation rule: %s", err))
		return
	}
	tflog.Trace(ctx, "deleted a telemetry transformation rule")
}

func (r *TelemetryTransformationRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ",")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		resp.Diagnostics.AddError("Invalid Import ID", "Expected dataset,origin-or-id with a nonempty dataset and rule identifier.")
		return
	}
	dataset, identifier := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	body, err := r.client.GetTelemetryTransformationRule(ctx, identifier, dataset)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Telemetry Transformation Rule", fmt.Sprintf("Unable to get rule %q in dataset %q: %s", identifier, dataset, err))
		return
	}
	if isSoftDeleted(body) {
		resp.Diagnostics.AddError("Error Importing Telemetry Transformation Rule", "The rule has been deleted and cannot be imported.")
		return
	}
	var rule dash0.TelemetryTransformationRuleDefinition
	if err := json.Unmarshal([]byte(body), &rule); err != nil {
		resp.Diagnostics.AddError("Error Importing Telemetry Transformation Rule", fmt.Sprintf("Unable to decode rule: %s", err))
		return
	}
	model := telemetryTransformationRuleModel{
		Origin:                          types.StringValue(identifier),
		ID:                              stringOrNull(dash0.GetTelemetryTransformationRuleID(&rule)),
		Dataset:                         types.StringValue(dataset),
		TelemetryTransformationRuleYAML: types.StringValue(body),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
