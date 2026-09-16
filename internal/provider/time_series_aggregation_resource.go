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

// annotationDeletedAt marks a soft-deleted time series aggregation. Deletes are
// soft for this asset kind: after a DELETE, a GET by origin returns 200 with
// this annotation set rather than a 404.
const annotationDeletedAt = "dash0.com/deleted-at"

// timeSeriesAggregationConditionallyIgnoredFields are fields the Dash0 API
// defaults server-side on write and echoes back on read. When the user does not
// declare them, comparing them would produce a diff the user cannot resolve.
//
// spec.enabled is deliberately NOT in this list. TimeSeriesAggregationSpec.Enabled
// is a non-pointer bool with no omitempty, so the client cannot omit it: every
// write asserts a value. Ignoring it would mean an aggregation enabled outside
// Terraform is silently switched back off by the next unrelated apply, with no
// diff ever shown. ValidateConfig requires the field instead, which keeps it in
// drift detection.
//
// The list is deliberately scoped to this resource. converter.ConditionallyIgnoredFields
// is read by seven other resources and by the shared plan modifier; widening it
// here would change their drift detection too. slices.Clone is required so the
// append cannot alias and mutate the shared backing array.
var timeSeriesAggregationConditionallyIgnoredFields = append(
	slices.Clone(converter.ConditionallyIgnoredFields),
	"spec.priority", // server default: 2; a *int with omitempty, so genuinely omittable
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &TimeSeriesAggregationResource{}
	_ resource.ResourceWithConfigure      = &TimeSeriesAggregationResource{}
	_ resource.ResourceWithImportState    = &TimeSeriesAggregationResource{}
	_ resource.ResourceWithValidateConfig = &TimeSeriesAggregationResource{}
)

// NewTimeSeriesAggregationResource is a helper function to simplify the provider implementation.
func NewTimeSeriesAggregationResource() resource.Resource {
	return &TimeSeriesAggregationResource{}
}

// TimeSeriesAggregationResource is the resource implementation.
type TimeSeriesAggregationResource struct {
	client client.Client
	// defaultDataset is the provider-level default dataset, inherited by this
	// resource's `dataset` attribute when it is omitted from configuration.
	defaultDataset string
}

// timeSeriesAggregationModel is the Terraform state model for a time series
// aggregation resource. There is no `url` attribute: time series aggregations
// have no deeplink asset type, so there is no web app page to link to.
type timeSeriesAggregationModel struct {
	Origin                    types.String `tfsdk:"origin"`
	ID                        types.String `tfsdk:"id"`
	Dataset                   types.String `tfsdk:"dataset"`
	TimeSeriesAggregationYaml types.String `tfsdk:"time_series_aggregation_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *TimeSeriesAggregationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TimeSeriesAggregationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_time_series_aggregation"
}

func (r *TimeSeriesAggregationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Time Series Aggregation. Time series aggregations roll up raw metric " +
			"data points into pre-aggregated series, reducing cardinality and query cost while keeping the " +
			"dimensions you care about.\n\n" +
			"~> **Note:** Every time series aggregation API call requires the organization-admin role. A token " +
			"without it is rejected with a 403 naming the missing role.\n\n" +
			"Origins are unique per organization, while each aggregation belongs to exactly one dataset. " +
			"Applying the same document to a second dataset under the same origin is rejected by the API. " +
			"Terraform is structurally safe here because every resource instance generates its own random " +
			"`tf_`-prefixed origin, but note that `dataset` is `RequiresReplace`: changing it destroys the " +
			"aggregation and re-creates it under a new origin.\n\n" +
			"~> **Note:** `spec.enabled` is required. The provider always sends a value for it, so an " +
			"omitted field would silently create a disabled aggregation and later switch off one enabled elsewhere.\n\n" +
			"Resource-level attribute aggregation (`spec.attributeModifications[].spec.context: resource`) is " +
			"not supported by the Dash0 web app today. The provider passes the value through unvalidated, " +
			"matching the Dash0 CLI, so it may be accepted by the API but not reflected in the UI.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the time series aggregation, automatically generated on creation. Used to reference the aggregation for updates, reads, deletes, and imports. Origins are unique per organization.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned UUID of the time series aggregation, resolved by the provider after creation. Reference this value when wiring the aggregation's identifier into another resource.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the time series aggregation belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. If omitted, the provider-level `dataset` default is used (see the provider's `dataset` attribute). Changing this value forces the resource to be recreated under a new origin.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"time_series_aggregation_yaml": schema.StringAttribute{
				Description: "The time series aggregation definition in YAML format. The YAML must declare `kind: Dash0TimeSeriesAggregation`, an explicit `spec.enabled`, and a `spec` describing which metrics to match and how to aggregate them. " +
					"`spec.priority` is defaulted by the server (to `2`) and is excluded from drift detection when omitted from the configuration. " +
					"All `metadata.labels` are server-managed and ignored during drift detection.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualConditionally(timeSeriesAggregationConditionallyIgnoredFields),
				},
			},
		},
	}
}

// ValidateConfig runs plan-time validation for time_series_aggregation_yaml so
// users see problems on `terraform plan` rather than on the subsequent apply.
//
// Only `kind` is checked, not `apiVersion`: TimeSeriesAggregationDefinition has
// no apiVersion field, so there is no version to dispatch on.
//
// `spec.attributeModifications[].spec.context` is deliberately not validated —
// the Dash0 CLI validates nothing there, and making Terraform stricter than the
// sibling IaC tool for the same API would diverge the two.
func (r *TimeSeriesAggregationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model timeSeriesAggregationModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.TimeSeriesAggregationYaml.IsNull() || model.TimeSeriesAggregationYaml.IsUnknown() {
		return
	}

	parsed := parseAndValidateTimeSeriesAggregationYAML(model.TimeSeriesAggregationYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	warnIfCustomLabelsSet(parsed, &resp.Diagnostics)
}

// parseAndValidateTimeSeriesAggregationYAML parses the user's document and
// enforces every invariant the provider depends on, appending errors to diags.
// It returns nil when any of them fails.
//
// This is shared by ValidateConfig, Create, and Update rather than living in
// ValidateConfig alone. ValidateConfig cannot see a value that is unknown at
// plan time — the ordinary case when the YAML interpolates another resource's
// computed attribute, e.g. via templatefile() — and returns early. Create and
// Update always have a known value, so they are the backstop that an unknown
// cannot slip past. Without it an omitted spec.enabled reaches the API as
// false, is stripped from drift comparison as an absent zero value, and leaves
// a permanently disabled aggregation with a permanently clean plan.
func parseAndValidateTimeSeriesAggregationYAML(document string, diags *diag.Diagnostics) map[string]interface{} {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		diags.AddAttributeError(
			path.Root("time_series_aggregation_yaml"),
			"Invalid YAML in time_series_aggregation_yaml",
			fmt.Sprintf("time_series_aggregation_yaml is not valid YAML: %s", err),
		)
		return nil
	}

	// An empty, whitespace-only, or explicitly null document unmarshals into a
	// nil map without error (a scalar or sequence errors above instead), so
	// guard before the shape check.
	if parsed == nil {
		diags.AddAttributeError(
			path.Root("time_series_aggregation_yaml"),
			"time_series_aggregation_yaml is empty or not a YAML mapping",
			"time_series_aggregation_yaml must be a YAML mapping following the Dash0TimeSeriesAggregation CRD envelope (kind, metadata, spec).",
		)
		return nil
	}

	if kind, _ := parsed["kind"].(string); kind != "Dash0TimeSeriesAggregation" {
		diags.AddAttributeError(
			path.Root("time_series_aggregation_yaml"),
			"time_series_aggregation_yaml is missing or has the wrong kind",
			fmt.Sprintf("time_series_aggregation_yaml must declare `kind: Dash0TimeSeriesAggregation`; got %q. The "+
				"dash0_time_series_aggregation resource only manages the Dash0TimeSeriesAggregation CRD kind.", kind),
		)
		return nil
	}

	errorIfEnabledAbsent(parsed, diags)
	if diags.HasError() {
		return nil
	}
	return parsed
}

// errorIfEnabledAbsent requires the user's YAML to declare spec.enabled.
//
// The field cannot be left to the server: TimeSeriesAggregationSpec.Enabled is a
// non-pointer bool with no omitempty, so every write the provider makes asserts
// a value, and an omitted field is sent as false. Accepting the omission would
// mean an aggregation someone enabled in the Dash0 UI is silently disabled again
// by the next unrelated apply. Requiring it makes that intent explicit and keeps
// the field in drift detection.
func errorIfEnabledAbsent(parsed map[string]interface{}, diags *diag.Diagnostics) {
	if spec, ok := parsed["spec"].(map[string]interface{}); ok {
		if _, present := spec["enabled"]; present {
			return
		}
	}
	diags.AddAttributeError(
		path.Root("time_series_aggregation_yaml"),
		"spec.enabled must be set explicitly",
		"time_series_aggregation_yaml must declare `spec.enabled`. The provider always sends a value for "+
			"this field, so omitting it silently creates a disabled aggregation that produces no rollups — "+
			"and a later apply would switch off an aggregation that had been enabled outside Terraform. "+
			"Set `spec.enabled: true` to have the aggregation take effect, or `spec.enabled: false` to "+
			"declare it intentionally disabled.",
	)
}

// warnIfCustomLabelsSet emits a Warning when the user's YAML declares
// metadata.labels.custom. Normalization strips the entire metadata.labels
// subtree before comparison, so changes to these labels are invisible to drift
// detection: the plan stays clean no matter what the server stores.
func warnIfCustomLabelsSet(parsed map[string]interface{}, diags *diag.Diagnostics) {
	metadata, ok := parsed["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		return
	}
	custom, ok := labels["custom"].(map[string]interface{})
	if !ok || len(custom) == 0 {
		return
	}
	keys := make([]string, 0, len(custom))
	for k := range custom {
		keys = append(keys, fmt.Sprintf("%v", k))
	}
	slices.Sort(keys)
	diags.AddAttributeWarning(
		path.Root("time_series_aggregation_yaml"),
		"metadata.labels.custom is not covered by drift detection",
		fmt.Sprintf("metadata.labels.custom is declared (keys: %s), but the provider strips the whole "+
			"metadata.labels subtree before comparing the configuration with the API response. Changes to "+
			"these labels are therefore silently lossy: they are sent on write, but the plan stays clean "+
			"whether or not the server stored them, and an out-of-band change to them is never reported.",
			strings.Join(keys, ", ")),
	)
}

// resolveTimeSeriesAggregation populates the aggregation's server-assigned id on
// the model by looking it up via the list endpoint. The id is best-effort
// metadata: failures are surfaced as warnings and leave the attribute null
// rather than failing the operation.
func (r *TimeSeriesAggregationResource) resolveTimeSeriesAggregation(ctx context.Context, model *timeSeriesAggregationModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveTimeSeriesAggregation(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve time series aggregation metadata",
			fmt.Sprintf("The time series aggregation was saved successfully, but its id could not be determined: %s", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

// isTimeSeriesAggregationDeleted reports whether the given API response is a
// soft-delete tombstone. Time series aggregation deletes are soft: after a
// DELETE, a GET by origin returns 200 with metadata.annotations["dash0.com/deleted-at"]
// set instead of a 404, so a branch keyed only on IsNotFound would never fire.
func isTimeSeriesAggregationDeleted(apiResponseJSON string) bool {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(apiResponseJSON), &parsed); err != nil {
		return false
	}
	metadata, ok := parsed["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	annotations, ok := metadata["annotations"].(map[string]interface{})
	if !ok {
		return false
	}
	deletedAt, ok := annotations[annotationDeletedAt].(string)
	return ok && deletedAt != ""
}

func (r *TimeSeriesAggregationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model timeSeriesAggregationModel
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
	// past. See parseAndValidateTimeSeriesAggregationYAML.
	parsed := parseAndValidateTimeSeriesAggregationYAML(model.TimeSeriesAggregationYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// The warning is emitted here too, for the same reason: a document that was
	// unknown at plan time never reached the ValidateConfig call site.
	warnIfCustomLabelsSet(parsed, &resp.Diagnostics)

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.TimeSeriesAggregationYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert time series aggregation YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateTimeSeriesAggregation(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create time series aggregation, got error: %s", err))
		return
	}

	// Resolve the id for the newly created aggregation (best-effort).
	r.resolveTimeSeriesAggregation(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a time series aggregation resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *TimeSeriesAggregationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state timeSeriesAggregationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetTimeSeriesAggregation(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Time series aggregation %s no longer exists on the server; removing from state", state.Origin.ValueString()))
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read time series aggregation, got error: %s", err))
		return
	}

	// Deletes are soft: a deleted aggregation comes back as a 200 tombstone,
	// not a 404. Treat it as gone so the next apply re-creates it.
	if isTimeSeriesAggregationDeleted(apiResponseJSON) {
		tflog.Debug(ctx, fmt.Sprintf("Time series aggregation %s is soft-deleted on the server; removing from state", state.Origin.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	tflog.Trace(ctx, "read a time series aggregation resource")

	// Compare the current state with the retrieved time series aggregation
	if state.TimeSeriesAggregationYaml.ValueString() != "" {
		stateYAML := state.TimeSeriesAggregationYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, timeSeriesAggregationConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, nil)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Time Series Aggregation Comparison Error",
				fmt.Sprintf("Error comparing time series aggregations: %s. Using API response as source of truth.", err),
			)
			state.TimeSeriesAggregationYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Time series aggregation has changed, updating state")
			state.TimeSeriesAggregationYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Time series aggregation is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.TimeSeriesAggregationYaml = types.StringValue(apiResponseJSON)
	}

	// Self-heal a null id: a transient resolve failure at Create must not leave
	// the attribute null for the lifetime of the resource.
	if state.ID.IsNull() || state.ID.IsUnknown() {
		r.resolveTimeSeriesAggregation(ctx, &state, &resp.Diagnostics)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *TimeSeriesAggregationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state timeSeriesAggregationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan timeSeriesAggregationModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-validate here for the same reason as in Create: ValidateConfig cannot
	// see a document that was unknown at plan time.
	parsed := parseAndValidateTimeSeriesAggregationYAML(plan.TimeSeriesAggregationYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	warnIfCustomLabelsSet(parsed, &resp.Diagnostics)

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.TimeSeriesAggregationYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert time series aggregation YAML to JSON: %s", err))
		return
	}

	// Update the existing aggregation (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The aggregation's identifier is immutable, so the id does not change on
	// update; carry it from state instead of re-resolving it via the API.
	plan.ID = state.ID
	err = r.client.UpdateTimeSeriesAggregation(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update time series aggregation, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a time series aggregation resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *TimeSeriesAggregationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state timeSeriesAggregationModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteTimeSeriesAggregation(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Time series aggregation %s was already gone on the server; treating delete as successful", state.Origin.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete time series aggregation, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a time series aggregation resource")
}

// ImportState function is required for resources that support import
func (r *TimeSeriesAggregationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetTimeSeriesAggregation(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Time Series Aggregation",
			fmt.Sprintf("Could not get time series aggregation with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Deletes are soft, so a GET can succeed against an already-deleted
	// aggregation. Refuse to adopt a tombstone into state.
	if isTimeSeriesAggregationDeleted(apiResponseJSON) {
		resp.Diagnostics.AddError(
			"Error Importing Time Series Aggregation",
			fmt.Sprintf("The time series aggregation with origin=%s, dataset=%s has been deleted (it carries a %s "+
				"annotation) and cannot be imported. Remove the import block and create the aggregation instead.",
				origin, dataset, annotationDeletedAt),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("time_series_aggregation_yaml"), apiResponseJSON)...)

	// Resolve the id (best-effort).
	model := timeSeriesAggregationModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveTimeSeriesAggregation(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
