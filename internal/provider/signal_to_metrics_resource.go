package provider

import (
	"context"
	"fmt"
	"slices"
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
	_ resource.Resource                   = &SignalToMetricsResource{}
	_ resource.ResourceWithConfigure      = &SignalToMetricsResource{}
	_ resource.ResourceWithImportState    = &SignalToMetricsResource{}
	_ resource.ResourceWithValidateConfig = &SignalToMetricsResource{}
)

// NewSignalToMetricsResource is a helper function to simplify the provider implementation.
func NewSignalToMetricsResource() resource.Resource {
	return &SignalToMetricsResource{}
}

// SignalToMetricsResource is the resource implementation.
type SignalToMetricsResource struct {
	client client.Client
	// defaultDataset is the provider-level default dataset, inherited by this
	// resource's `dataset` attribute when it is omitted from configuration.
	defaultDataset string
}

// signalToMetricsModel is the Terraform state model for a signal-to-metrics
// resource. There is no `url` attribute: signal-to-metrics rules have no
// deeplink asset type, so there is no web app page to link to.
type signalToMetricsModel struct {
	Origin              types.String `tfsdk:"origin"`
	ID                  types.String `tfsdk:"id"`
	Dataset             types.String `tfsdk:"dataset"`
	SignalToMetricsYaml types.String `tfsdk:"signal_to_metrics_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *SignalToMetricsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SignalToMetricsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_signal_to_metrics"
}

func (r *SignalToMetricsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Signal-to-Metrics rule. A signal-to-metrics rule turns the spans or logs that " +
			"match its filter into metric data points at ingestion: it counts them, or records a number taken from " +
			"each one as a sum, gauge, or exponential histogram. See " +
			"[Convert Signals to Metrics](https://dash0.com/docs/dash0/telemetry/signal-control/convert-signals-to-metrics) " +
			"for the rule fields and their limits.\n\n" +
			"~> **Note:** `spec.enabled` is required. The provider always sends a value for it, so an " +
			"omitted field would silently create a disabled rule and later switch off one enabled elsewhere.\n\n" +
			"Deletes are soft: a destroyed rule stops producing metrics, and the API keeps a tombstone of it. " +
			"Changing `dataset` destroys the rule and re-creates it under a new origin.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the signal-to-metrics rule, automatically generated on creation. Used to reference the rule for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned identifier of the signal-to-metrics rule, resolved by the provider after creation.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the signal-to-metrics rule belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. If omitted, the provider-level `dataset` default is used (see the provider's `dataset` attribute). Changing this value forces the resource to be recreated under a new origin.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"signal_to_metrics_yaml": schema.StringAttribute{
				Description: "The signal-to-metrics rule definition in YAML format. The YAML must declare `kind: Dash0SignalToMetrics`, an explicit `spec.enabled`, `spec.match` (the source signal and its filters), and `spec.output` (the metric name and interval). " +
					"`spec.targetDatasetMode: original` is the default and is treated as equal to an omitted field. " +
					"`metadata.labels` and `metadata.annotations` are server-managed and ignored, and `metadata.labels.custom` is rejected because signal-to-metrics rules have no custom labels.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualNormalizing(normalizeSignalToMetricsDefaults),
				},
			},
		},
	}
}

// normalizeSignalToMetricsDefaults removes spec.targetDatasetMode when it is
// `original`, the value the server applies when the field is absent. It is
// applied to both sides of a drift comparison, never to the stored value.
//
// The server stores the field verbatim and does not echo a default, so an
// omitted field is not drift by itself. But a document written with an
// explicit `original` — by the Dash0 UI, for example, and then imported —
// would otherwise differ from a configuration that omits it. A conditional
// ignore would also absorb that, but it would hide a real out-of-band change
// to `alternative` or `both`, which routes the metric to another dataset.
//
// A document that does not parse is returned unchanged, so the comparison
// fails on it the same way it would without normalization.
func normalizeSignalToMetricsDefaults(document string) string {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		return document
	}
	spec, ok := parsed["spec"].(map[string]interface{})
	if !ok || spec["targetDatasetMode"] != string(dash0.SignalToMetricsTargetDatasetModeOriginal) {
		return document
	}
	delete(spec, "targetDatasetMode")
	normalized, err := yaml.Marshal(parsed)
	if err != nil {
		return document
	}
	return string(normalized)
}

// ValidateConfig runs plan-time validation for signal_to_metrics_yaml so users
// see problems on `terraform plan` rather than on the subsequent apply.
//
// Only `kind` is checked, not `apiVersion`: SignalToMetricsDefinition has no
// apiVersion field, so there is no version to dispatch on.
func (r *SignalToMetricsResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model signalToMetricsModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.SignalToMetricsYaml.IsNull() || model.SignalToMetricsYaml.IsUnknown() {
		return
	}

	parseAndValidateSignalToMetricsYAML(model.SignalToMetricsYaml.ValueString(), &resp.Diagnostics)
}

// parseAndValidateSignalToMetricsYAML parses the user's document and enforces
// every invariant the provider depends on, appending errors to diags. It
// returns nil when any of them fails.
//
// This is shared by ValidateConfig, Create, and Update rather than living in
// ValidateConfig alone. ValidateConfig cannot see a value that is unknown at
// plan time — the ordinary case when the YAML interpolates another resource's
// computed attribute — and returns early. Create and Update always have a
// known value, so they are the backstop that an unknown cannot slip past.
func parseAndValidateSignalToMetricsYAML(document string, diags *diag.Diagnostics) map[string]interface{} {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		diags.AddAttributeError(
			path.Root("signal_to_metrics_yaml"),
			"Invalid YAML in signal_to_metrics_yaml",
			fmt.Sprintf("signal_to_metrics_yaml is not valid YAML: %s", err),
		)
		return nil
	}

	// An empty, whitespace-only, or explicitly null document unmarshals into a
	// nil map without error, so guard before the shape check.
	if parsed == nil {
		diags.AddAttributeError(
			path.Root("signal_to_metrics_yaml"),
			"signal_to_metrics_yaml is empty or not a YAML mapping",
			"signal_to_metrics_yaml must be a YAML mapping following the Dash0SignalToMetrics envelope (kind, metadata, spec).",
		)
		return nil
	}

	if kind, _ := parsed["kind"].(string); kind != string(dash0.Dash0SignalToMetrics) {
		diags.AddAttributeError(
			path.Root("signal_to_metrics_yaml"),
			"signal_to_metrics_yaml is missing or has the wrong kind",
			fmt.Sprintf("signal_to_metrics_yaml must declare `kind: Dash0SignalToMetrics`; got %q. The "+
				"dash0_signal_to_metrics resource only manages the Dash0SignalToMetrics kind of the Dash0 API.", kind),
		)
		return nil
	}

	// SignalToMetricsSpec.Enabled is a non-pointer bool with no omitempty.
	errorIfEnabledAbsent(parsed, "signal_to_metrics_yaml", "signal to metrics rule", diags)
	errorIfSignalToMetricsCustomLabelsSet(parsed, diags)
	if diags.HasError() {
		return nil
	}
	return parsed
}

// errorIfSignalToMetricsCustomLabelsSet rejects metadata.labels.custom.
//
// SignalToMetricsLabels has no custom field, so the labels are dropped when
// the document is decoded for the API and never reach the server. Normalization
// strips metadata.labels before comparison, so the loss would never show up in
// a plan either. An error is used rather than the shared warnIfCustomLabelsSet,
// whose message says the labels are sent on write: here they are not, and a
// warning would let the config claim labels the rule does not carry.
func errorIfSignalToMetricsCustomLabelsSet(parsed map[string]interface{}, diags *diag.Diagnostics) {
	metadata, _ := parsed["metadata"].(map[string]interface{})
	labels, _ := metadata["labels"].(map[string]interface{})
	custom, _ := labels["custom"].(map[string]interface{})
	if len(custom) == 0 {
		return
	}
	keys := make([]string, 0, len(custom))
	for k := range custom {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	diags.AddAttributeError(
		path.Root("signal_to_metrics_yaml"),
		"metadata.labels.custom is not supported for signal-to-metrics rules",
		fmt.Sprintf("metadata.labels.custom is declared (keys: %s), but signal-to-metrics rules have no custom labels: "+
			"the Dash0 API would drop them, and the plan could not show it. Remove metadata.labels.custom from "+
			"signal_to_metrics_yaml.", strings.Join(keys, ", ")),
	)
}

// signalToMetricsDatasetLabel returns the dash0.com/dataset label of an API
// response, or "" when it is absent or the document does not parse.
func signalToMetricsDatasetLabel(apiResponseJSON string) string {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(apiResponseJSON), &parsed); err != nil {
		return ""
	}
	metadata, _ := parsed["metadata"].(map[string]interface{})
	labels, _ := metadata["labels"].(map[string]interface{})
	dataset, _ := labels["dash0.com/dataset"].(string)
	return dataset
}

// resolveSignalToMetrics populates the rule's server-assigned id on the model
// by looking it up via the list endpoint. The id is best-effort metadata:
// failures are surfaced as warnings and leave the attribute null rather than
// failing the operation.
func (r *SignalToMetricsResource) resolveSignalToMetrics(ctx context.Context, model *signalToMetricsModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveSignalToMetrics(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve signal-to-metrics rule metadata",
			fmt.Sprintf("The signal-to-metrics rule's id could not be determined: %s. It is left null; the signal-to-metrics rule itself is unaffected.", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

func (r *SignalToMetricsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model signalToMetricsModel
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
	// past. See parseAndValidateSignalToMetricsYAML.
	parseAndValidateSignalToMetricsYAML(model.SignalToMetricsYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.SignalToMetricsYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert signal-to-metrics rule YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateSignalToMetrics(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create signal-to-metrics rule, got error: %s", err))
		return
	}

	// Resolve the id for the newly created rule (best-effort).
	r.resolveSignalToMetrics(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a signal-to-metrics resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *SignalToMetricsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state signalToMetricsModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deletes are soft, so a rule deleted out of band comes back as a tombstone
	// rather than a 404; removedOutOfBandInDataset handles both.
	apiResponseJSON, err := r.client.GetSignalToMetrics(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if removedOutOfBandInDataset(ctx, resp, r.client, state.Dataset.ValueString(), "Signal-to-metrics rule", state.Origin.ValueString(), apiResponseJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read signal-to-metrics rule, got error: %s", err))
		return
	}

	// The backend keys a rule on (organization, origin) and uses ?dataset= on a
	// GET only to authorize the request, so a rule moved to another dataset out
	// of band still reads back. Drift detection ignores labels, so without this
	// the plan would be empty and the next update would silently move the rule
	// back. Record the dataset the rule is in: a configured dataset then plans a
	// replacement, and an omitted one adopts the move.
	if actual := signalToMetricsDatasetLabel(apiResponseJSON); actual != "" && actual != state.Dataset.ValueString() {
		resp.Diagnostics.AddWarning(
			"Signal-to-metrics rule moved to another dataset",
			fmt.Sprintf("The signal-to-metrics rule with origin=%s is in dataset %q, not %q. It was moved outside Terraform.",
				state.Origin.ValueString(), actual, state.Dataset.ValueString()),
		)
		state.Dataset = types.StringValue(actual)
	}

	tflog.Trace(ctx, "read a signal-to-metrics resource")

	// Compare the current state with the retrieved rule
	if state.SignalToMetricsYaml.ValueString() != "" {
		stateYAML := normalizeSignalToMetricsDefaults(state.SignalToMetricsYaml.ValueString())
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, normalizeSignalToMetricsDefaults(apiResponseJSON), additionalIgnored, nil)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Signal-to-Metrics Rule Comparison Error",
				fmt.Sprintf("Error comparing signal-to-metrics rules: %s. Using API response as source of truth.", err),
			)
			state.SignalToMetricsYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Signal-to-metrics rule has changed, updating state")
			state.SignalToMetricsYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Signal-to-metrics rule is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.SignalToMetricsYaml = types.StringValue(apiResponseJSON)
	}

	// Self-heal a null id: a transient resolve failure at Create must not leave
	// the attribute null for the lifetime of the resource.
	if state.ID.IsNull() || state.ID.IsUnknown() {
		r.resolveSignalToMetrics(ctx, &state, &resp.Diagnostics)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *SignalToMetricsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state signalToMetricsModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan signalToMetricsModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-validate here for the same reason as in Create: ValidateConfig cannot
	// see a document that was unknown at plan time.
	parseAndValidateSignalToMetricsYAML(plan.SignalToMetricsYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.SignalToMetricsYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert signal-to-metrics rule YAML to JSON: %s", err))
		return
	}

	// Update the existing rule (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The rule's identifier is immutable, so the id does not change on update;
	// carry it from state instead of re-resolving it via the API.
	plan.ID = state.ID
	err = r.client.UpdateSignalToMetrics(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update signal-to-metrics rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a signal-to-metrics resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *SignalToMetricsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state signalToMetricsModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deleting an already-deleted rule returns 404, which is the desired end
	// state rather than an error.
	err := r.client.DeleteSignalToMetrics(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Signal-to-metrics rule %s was already gone on the server; treating delete as successful", state.Origin.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete signal-to-metrics rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a signal-to-metrics resource")
}

// ImportState function is required for resources that support import
func (r *SignalToMetricsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetSignalToMetrics(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Signal-to-Metrics Rule",
			fmt.Sprintf("Could not get signal-to-metrics rule with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Deletes are soft, so a GET can succeed against an already-deleted rule.
	// Refuse to adopt a tombstone into state.
	if isSoftDeleted(apiResponseJSON) {
		resp.Diagnostics.AddError(
			"Error Importing Signal-to-Metrics Rule",
			fmt.Sprintf("The signal-to-metrics rule with origin=%s, dataset=%s has been deleted (it carries a %s "+
				"annotation) and cannot be imported. Remove the import block and create the rule instead.",
				origin, dataset, annotationDeletedAt),
		)
		return
	}

	// The backend keys a rule on (organization, origin), so a GET by origin finds
	// the rule whatever dataset the request names, but a PUT moves the rule into
	// the dataset it names. Adopting the rule under the wrong dataset would
	// therefore move it on the next apply, so refuse.
	if actual := signalToMetricsDatasetLabel(apiResponseJSON); actual != "" && actual != dataset {
		resp.Diagnostics.AddError(
			"Error Importing Signal-to-Metrics Rule",
			fmt.Sprintf("The signal-to-metrics rule with origin=%s belongs to dataset %q, not %q. Import it with the ID '%s,%s'.",
				origin, actual, dataset, actual, origin),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("signal_to_metrics_yaml"), apiResponseJSON)...)

	// Resolve the id (best-effort).
	model := signalToMetricsModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveSignalToMetrics(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
