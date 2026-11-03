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
	_ resource.Resource                   = &SpamFilterResource{}
	_ resource.ResourceWithConfigure      = &SpamFilterResource{}
	_ resource.ResourceWithImportState    = &SpamFilterResource{}
	_ resource.ResourceWithValidateConfig = &SpamFilterResource{}
)

// NewSpamFilterResource is a helper function to simplify the provider implementation.
func NewSpamFilterResource() resource.Resource {
	return &SpamFilterResource{}
}

// SpamFilterResource is the resource implementation.
type SpamFilterResource struct {
	client client.Client
	// defaultDataset is the provider-level default dataset, inherited by this
	// resource's `dataset` attribute when it is omitted from configuration.
	defaultDataset string
}

// spamFilterModel is the Terraform state model for a spam filter resource.
type spamFilterModel struct {
	Origin         types.String `tfsdk:"origin"`
	ID             types.String `tfsdk:"id"`
	Dataset        types.String `tfsdk:"dataset"`
	SpamFilterYaml types.String `tfsdk:"spam_filter_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *SpamFilterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SpamFilterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spam_filter"
}

func (r *SpamFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Spam Filter. Spam filters allow you to drop noisy or unwanted telemetry data " +
			"before it is stored, reducing costs and improving signal-to-noise ratio. " +
			"Filters are scoped to a dataset and can target logs, spans, metric data points, web events, or profile samples.\n\n" +
			"To switch a filter off without deleting it, set the `dash0.com/enabled` annotation to `\"false\"`. " +
			"A filter that omits the annotation is enabled, so Terraform switches it back on if it was disabled outside Terraform.\n\n" +
			"See [Set Spam Filters](https://dash0.com/docs/dash0/cost-control/spam-filters) for more details.",

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the spam filter, automatically generated on creation. Used to reference the spam filter for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned UUID of the spam filter, resolved by the provider after creation. Useful for cross-referencing the filter from other resources or external systems. Spam filters are not addressable in the Dash0 web app, so no `url` is exposed.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the spam filter belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. If omitted, the provider-level `dataset` default is used (see the provider's `dataset` attribute). Changing this value forces the resource to be recreated.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"spam_filter_yaml": schema.StringAttribute{
				Description: "The spam filter definition in YAML format. " +
					"The YAML must include a `metadata.name` field and a `spec` with a `filter` (list of key-value matchers) " +
					"and either `contexts` (`v1alpha1`, a list holding exactly one signal type) or " +
					"`context` (`v1alpha2`, a single signal type). The signal type is one of `log`, `span`, `datapoint`, " +
					"`web_event` or `profile_sample`. The `apiVersion` field determines which shape is expected. " +
					"The optional `metadata.annotations` key `dash0.com/enabled` takes the quoted string `\"true\"` (the default) or `\"false\"`. " +
					"`metadata.labels` is not covered by drift detection.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualComparing(converter.SpamFilterYAMLEquivalent),
				},
			},
		},
	}
}

// ValidateConfig runs plan-time validation for spam_filter_yaml so users see
// problems on `terraform plan` rather than on the subsequent apply.
func (r *SpamFilterResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model spamFilterModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.SpamFilterYaml.IsNull() || model.SpamFilterYaml.IsUnknown() {
		return
	}

	parsed := parseAndValidateSpamFilterYAML(model.SpamFilterYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	warnIfCustomLabelsSet(parsed, "spam_filter_yaml", &resp.Diagnostics)
}

// parseAndValidateSpamFilterYAML parses the user's document and checks the
// invariants the provider depends on, appending errors to diags. It returns
// nil when any of them fails.
//
// Create and Update call it too: ValidateConfig returns early when the value
// is unknown at plan time (the YAML interpolates another resource's computed
// attribute), and by Create and Update the value is always known.
func parseAndValidateSpamFilterYAML(spamFilterYAML string, diags *diag.Diagnostics) map[string]interface{} {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(spamFilterYAML), &parsed); err != nil {
		diags.AddAttributeError(
			path.Root("spam_filter_yaml"),
			"Invalid YAML",
			fmt.Sprintf("Spam filter definition is not valid YAML: %s", err),
		)
		return nil
	}

	// The API types dash0.com/enabled as a string enum. An unquoted YAML
	// boolean would otherwise fail later with an opaque JSON decoding error.
	if metadata, ok := parsed["metadata"].(map[string]interface{}); ok {
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
			// An empty value decodes to nil and means enabled, like an absent one.
			if value, present := annotations[converter.AnnotationEnabled]; present && value != nil {
				if s, isString := value.(string); !isString || (s != "true" && s != "false") {
					diags.AddAttributeError(
						path.Root("spam_filter_yaml"),
						"Invalid dash0.com/enabled annotation",
						fmt.Sprintf("metadata.annotations.%s must be the quoted string \"true\" or \"false\", got %v (%T). "+
							"Quote the value, for example `dash0.com/enabled: \"false\"`.",
							converter.AnnotationEnabled, value, value),
					)
					return nil
				}
			}
		}
	}

	return parsed
}

// resolveSpamFilter populates the spam filter's server-assigned id on the
// model by looking it up via the list endpoint. The id is best-effort
// metadata: failures are surfaced as warnings and leave the attribute null
// rather than failing the operation.
func (r *SpamFilterResource) resolveSpamFilter(ctx context.Context, model *spamFilterModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveSpamFilter(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve spam filter metadata",
			fmt.Sprintf("The spam filter's id could not be determined: %s. It is left null; the spam filter itself is unaffected.", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

func (r *SpamFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model spamFilterModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())
	if model.Dataset.IsNull() || model.Dataset.IsUnknown() {
		model.Dataset = types.StringValue(r.defaultDataset)
	}

	// Re-validate here, not just in ValidateConfig: see parseAndValidateSpamFilterYAML.
	parsed := parseAndValidateSpamFilterYAML(model.SpamFilterYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	warnIfCustomLabelsSet(parsed, "spam_filter_yaml", &resp.Diagnostics)

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.SpamFilterYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert spam filter YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateSpamFilter(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create spam filter, got error: %s", err))
		return
	}

	// Resolve the id for the newly created spam filter (best-effort).
	r.resolveSpamFilter(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a spam filter resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *SpamFilterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state spamFilterModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetSpamFilter(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if removedOutOfBandInDataset(ctx, resp, r.client, state.Dataset.ValueString(), "Spam filter", state.Origin.ValueString(), apiResponseJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read spam filter, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a spam filter resource")

	// Compare the current state with the retrieved spam filter
	if state.SpamFilterYaml.ValueString() != "" {
		stateYAML := state.SpamFilterYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.SpamFilterYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Spam Filter Comparison Error",
				fmt.Sprintf("Error comparing spam filters: %s. Using API response as source of truth.", err),
			)
			state.SpamFilterYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Spam filter has changed, updating state")
			state.SpamFilterYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Spam filter is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.SpamFilterYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *SpamFilterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state spamFilterModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan spamFilterModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-validate here for the same reason as in Create.
	parsed := parseAndValidateSpamFilterYAML(plan.SpamFilterYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	warnIfCustomLabelsSet(parsed, "spam_filter_yaml", &resp.Diagnostics)

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.SpamFilterYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert spam filter YAML to JSON: %s", err))
		return
	}

	// Update the existing spam filter (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The spam filter's identifier is immutable, so the id never changes on
	// update; carry it from state instead of re-resolving it via the API.
	plan.ID = state.ID
	err = r.client.UpdateSpamFilter(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update spam filter, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a spam filter resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *SpamFilterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state spamFilterModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSpamFilter(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Spam filter %s was already gone on the server; treating delete as successful", state.Origin.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete spam filter, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a spam filter resource")
}

// ImportState function is required for resources that support import
func (r *SpamFilterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetSpamFilter(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Spam Filter",
			fmt.Sprintf("Could not get spam filter with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("spam_filter_yaml"), apiResponseJSON)...)

	// Resolve the id (best-effort).
	model := spamFilterModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveSpamFilter(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
