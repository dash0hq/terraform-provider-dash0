package provider

import (
	"context"
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
	_ resource.Resource                   = &EvalResource{}
	_ resource.ResourceWithConfigure      = &EvalResource{}
	_ resource.ResourceWithImportState    = &EvalResource{}
	_ resource.ResourceWithValidateConfig = &EvalResource{}
)

const evalYAMLAttribute = "eval_yaml"

// NewEvalResource is a helper function to simplify the provider implementation.
func NewEvalResource() resource.Resource {
	return &EvalResource{}
}

// EvalResource is the resource implementation.
type EvalResource struct {
	client         client.Client
	defaultDataset string
}

type evalModel struct {
	Origin   types.String `tfsdk:"origin"`
	ID       types.String `tfsdk:"id"`
	Dataset  types.String `tfsdk:"dataset"`
	EvalYaml types.String `tfsdk:"eval_yaml"`
}

func (r *EvalResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *EvalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_eval"
}

func (r *EvalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 generative AI evaluation (eval). An eval sends a sample of the GenAI spans " +
			"that match its filters to an LLM judge, which scores each span with the eval's prompts.\n\n" +
			"~> **Note:** `metadata.name` and the kind of `spec.judge.output` cannot change after create, because eval results are stored under the name and per output kind. Changing either destroys the eval and creates a new one, and the new eval starts with no results.\n\n" +
			"`metadata.name` must be unique within the dataset, and `spec.match.filters` must contain an `is_set` filter on `gen_ai.operation.name` without values. " +
			"Deletes are soft: a destroyed eval stops judging spans, and the API keeps a tombstone of it. " +
			"Changing `dataset` destroys the eval and re-creates it under a new origin.",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the eval, automatically generated on creation. Used to reference the eval for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned identifier of the eval, resolved by the provider after creation.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the eval belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. If omitted, the provider-level `dataset` default is used (see the provider's `dataset` attribute). Changing this value forces the resource to be recreated under a new origin.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			evalYAMLAttribute: schema.StringAttribute{
				Description: "The eval definition in YAML format. The YAML must declare `kind: Dash0GenerativeAiEvaluation`, `metadata.name`, an explicit `spec.enabled`, and `spec.display`, `spec.match`, `spec.sample` and `spec.judge`. " +
					"The `dash0.com/folder-path` and `dash0.com/sharing` metadata annotations are supported, and changes to them trigger a resource update; `dash0.com/sharing` has no effect on access. " +
					"The server-managed `metadata.labels` and `metadata.annotations` are ignored, and any other label or annotation is rejected, because the Dash0 API would drop it.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(evalComparedAnnotations...),
					stringplanmodifier.RequiresReplaceIf(
						requiresReplaceOnImmutableEvalChange,
						"Changing metadata.name or the kind of spec.judge.output forces a replacement.",
						"Changing `metadata.name` or the kind of `spec.judge.output` forces a replacement.",
					),
				},
			},
		},
	}
}

// evalImmutableFields returns metadata.name and the kind of
// spec.judge.output of a document, or ok=false when it does not parse.
func evalImmutableFields(document string) (name string, outputKind string, ok bool) {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil || parsed == nil {
		return "", "", false
	}
	metadata, _ := parsed["metadata"].(map[string]interface{})
	name, _ = metadata["name"].(string)
	spec, _ := parsed["spec"].(map[string]interface{})
	judge, _ := spec["judge"].(map[string]interface{})
	output, _ := judge["output"].(map[string]interface{})
	outputKind, _ = output["kind"].(string)
	return name, outputKind, true
}

// requiresReplaceOnImmutableEvalChange plans a replacement
// when the configuration changes a field the API refuses to update. Without
// it the plan would show an in-place update that fails at apply with a 400.
func requiresReplaceOnImmutableEvalChange(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.StateValue.IsNull() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	stateName, stateKind, stateOK := evalImmutableFields(req.StateValue.ValueString())
	configName, configKind, configOK := evalImmutableFields(req.ConfigValue.ValueString())
	if !stateOK || !configOK {
		return
	}
	resp.RequiresReplace = stateName != configName || stateKind != configKind
}

func (r *EvalResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model evalModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.EvalYaml.IsNull() || model.EvalYaml.IsUnknown() {
		return
	}

	parseAndValidateEvalYAML(model.EvalYaml.ValueString(), &resp.Diagnostics)
}

// parseAndValidateEvalYAML enforces the invariants the
// provider depends on, appending errors to diags. Create and Update call it
// too, because ValidateConfig skips a value that is unknown at plan time.
func parseAndValidateEvalYAML(document string, diags *diag.Diagnostics) {
	attribute := path.Root(evalYAMLAttribute)

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(document), &parsed); err != nil {
		diags.AddAttributeError(attribute, "Invalid YAML in "+evalYAMLAttribute,
			fmt.Sprintf("%s is not valid YAML: %s", evalYAMLAttribute, err))
		return
	}
	if parsed == nil {
		diags.AddAttributeError(attribute, evalYAMLAttribute+" is empty or not a YAML mapping",
			evalYAMLAttribute+" must be a YAML mapping following the Dash0GenerativeAiEvaluation envelope (kind, metadata, spec).")
		return
	}

	if kind, _ := parsed["kind"].(string); kind != string(dash0.Dash0GenerativeAiEvaluation) {
		diags.AddAttributeError(attribute, evalYAMLAttribute+" is missing or has the wrong kind",
			fmt.Sprintf("%s must declare `kind: Dash0GenerativeAiEvaluation`; got %q.", evalYAMLAttribute, kind))
		return
	}

	metadata, _ := parsed["metadata"].(map[string]interface{})
	if name, _ := metadata["name"].(string); strings.TrimSpace(name) == "" {
		diags.AddAttributeError(attribute, "metadata.name is missing",
			"An eval needs a `metadata.name`. It identifies the eval within its dataset and cannot change after create.")
	}

	errorIfEnabledAbsent(parsed, evalYAMLAttribute, "generative AI evaluation", diags)
	errorIfEvalLacksOperationNameFilter(parsed, diags)
	errorIfEvalUnsupportedMetadata(metadata, diags)
}

// errorIfEvalLacksOperationNameFilter mirrors the API rule
// that every eval matches only GenAI spans: spec.match.filters must contain an
// is_set filter on gen_ai.operation.name without values.
func errorIfEvalLacksOperationNameFilter(parsed map[string]interface{}, diags *diag.Diagnostics) {
	spec, _ := parsed["spec"].(map[string]interface{})
	match, _ := spec["match"].(map[string]interface{})
	filters, _ := match["filters"].([]interface{})
	for _, f := range filters {
		filter, _ := f.(map[string]interface{})
		_, hasValue := filter["value"]
		_, hasValues := filter["values"]
		if filter["key"] == "gen_ai.operation.name" && filter["operator"] == "is_set" && !hasValue && !hasValues {
			return
		}
	}
	diags.AddAttributeError(path.Root(evalYAMLAttribute),
		"spec.match.filters has no gen_ai.operation.name filter",
		"spec.match.filters must contain `{key: gen_ai.operation.name, operator: is_set}` without values, so the eval judges only GenAI spans. The Dash0 API rejects an eval without it.")
}

var evalLabelKeys = []string{
	"dash0.com/dataset",
	"dash0.com/id",
	"dash0.com/origin",
	"dash0.com/source",
	"dash0.com/version",
}

// evalComparedAnnotations are the annotations the user
// manages. The API stores and returns them, so drift detection compares them.
var evalComparedAnnotations = []string{
	converter.AnnotationFolderPath,
	converter.AnnotationSharing,
}

var evalAnnotationKeys = append([]string{
	"dash0.com/created-at",
	"dash0.com/created-by",
	"dash0.com/deleted-at",
	"dash0.com/updated-at",
}, evalComparedAnnotations...)

// errorIfEvalUnsupportedMetadata rejects labels and
// annotations that the API has no field for. They would be dropped on decode,
// and drift detection ignores labels, so the loss would never show in a plan.
func errorIfEvalUnsupportedMetadata(metadata map[string]interface{}, diags *diag.Diagnostics) {
	if keys := unsupportedMetadataKeys(metadata["labels"], evalLabelKeys); len(keys) > 0 {
		diags.AddAttributeError(path.Root(evalYAMLAttribute),
			"metadata.labels has keys that evals do not support",
			fmt.Sprintf("metadata.labels declares %s, but evals carry only the server-managed labels (%s). The Dash0 API would drop the other labels. Remove them from %s.",
				strings.Join(keys, ", "), strings.Join(evalLabelKeys, ", "), evalYAMLAttribute))
	}
	if keys := unsupportedMetadataKeys(metadata["annotations"], evalAnnotationKeys); len(keys) > 0 {
		diags.AddAttributeError(path.Root(evalYAMLAttribute),
			"metadata.annotations has keys that evals do not support",
			fmt.Sprintf("metadata.annotations declares %s, but evals carry only these annotations: %s. The Dash0 API would drop the others. Remove them from %s.",
				strings.Join(keys, ", "), strings.Join(evalAnnotationKeys, ", "), evalYAMLAttribute))
	}
}

// resolveEval populates the server-assigned id on the model.
// The id is best-effort metadata: a failure is a warning and leaves it null.
func (r *EvalResource) resolveEval(ctx context.Context, model *evalModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveEval(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve eval metadata",
			fmt.Sprintf("The eval's id could not be determined: %s. It is left null; the eval itself is unaffected.", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

func (r *EvalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model evalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())
	if model.Dataset.IsNull() || model.Dataset.IsUnknown() {
		model.Dataset = types.StringValue(r.defaultDataset)
	}

	parseAndValidateEvalYAML(model.EvalYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	jsonBody, err := converter.ConvertYAMLToJSON(model.EvalYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert eval YAML to JSON: %s", err))
		return
	}

	if err := r.client.CreateEval(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create eval, got error: %s", err))
		return
	}

	r.resolveEval(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a generative AI evaluation resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *EvalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state evalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deletes are soft, so an eval deleted out of band comes back as a tombstone
	// rather than a 404; removedOutOfBandInDataset handles both.
	apiResponseJSON, err := r.client.GetEval(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if removedOutOfBandInDataset(ctx, resp, r.client, state.Dataset.ValueString(), "Generative AI evaluation", state.Origin.ValueString(), apiResponseJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read eval, got error: %s", err))
		return
	}

	if state.EvalYaml.ValueString() != "" {
		equivalent, err := converter.ResourceYAMLEquivalent(
			state.EvalYaml.ValueString(),
			apiResponseJSON,
			nil,
			evalComparedAnnotations,
		)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Eval Comparison Error",
				fmt.Sprintf("Error comparing evals: %s. Using API response as source of truth.", err),
			)
			state.EvalYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Eval has changed, updating state")
			state.EvalYaml = types.StringValue(apiResponseJSON)
		}
	} else {
		state.EvalYaml = types.StringValue(apiResponseJSON)
	}

	if state.ID.IsNull() || state.ID.IsUnknown() {
		r.resolveEval(ctx, &state, &resp.Diagnostics)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *EvalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state evalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var plan evalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parseAndValidateEvalYAML(plan.EvalYaml.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	jsonBody, err := converter.ConvertYAMLToJSON(plan.EvalYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert eval YAML to JSON: %s", err))
		return
	}

	plan.Origin = state.Origin
	plan.ID = state.ID
	if err := r.client.UpdateEval(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update eval, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a generative AI evaluation resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *EvalResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state evalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deleting an already-deleted eval returns 404, which is the desired end state.
	err := r.client.DeleteEval(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Eval %s was already gone on the server; treating delete as successful", state.Origin.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete eval, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a generative AI evaluation resource")
}

func (r *EvalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetEval(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Eval",
			fmt.Sprintf("Could not get eval with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	if isSoftDeleted(apiResponseJSON) {
		resp.Diagnostics.AddError(
			"Error Importing Eval",
			fmt.Sprintf("The eval with origin=%s, dataset=%s has been deleted (it carries a %s annotation) and cannot be imported. "+
				"Remove the import block and create the eval instead.", origin, dataset, annotationDeletedAt),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(evalYAMLAttribute), apiResponseJSON)...)

	model := evalModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveEval(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
