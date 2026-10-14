package provider

import (
	"context"
	"fmt"

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

const (
	datasetKind      = "Dash0Dataset"
	datasetIDLabel   = "dash0.com/id"
	datasetOriginKey = "dash0.com/origin"
)

// datasetServerManagedFields never participate in drift detection: the
// resource does not manage them yet, or the server derives them.
var datasetServerManagedFields = []string{
	"spec.permissions",
	"spec.telemetryFilters",
	"spec.permittedActions",
}

// datasetServerDefaultedFields are ignored only when the user did not set
// them, so the server may fill in defaults (retention, privacy settings, ...)
// without causing a diff. When the user sets a field, drift on it is detected.
var datasetServerDefaultedFields = []string{
	"spec.tracing",
	"spec.tracing.convertSpanEventsToLogRecords",
	"spec.logging",
	"spec.metrics",
	"spec.profiling",
	"spec.webEvents",
	"spec.geoLocation",
	"spec.ipAddresses",
	"spec.genAiAttributeRedaction",
	"spec.darkplane",
	"spec.cardholderDataRedaction",
	"spec.semanticConventionUpgrades",
	"spec.preferred",
}

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &DatasetResource{}
	_ resource.ResourceWithConfigure      = &DatasetResource{}
	_ resource.ResourceWithImportState    = &DatasetResource{}
	_ resource.ResourceWithValidateConfig = &DatasetResource{}
	_ resource.ResourceWithModifyPlan     = &DatasetResource{}
)

// NewDatasetResource is a helper function to simplify the provider implementation.
func NewDatasetResource() resource.Resource {
	return &DatasetResource{}
}

// DatasetResource is the resource implementation.
type DatasetResource struct {
	client client.Client
}

// datasetModel is the Terraform state model for a dataset resource.
type datasetModel struct {
	Origin      types.String `tfsdk:"origin"`
	ID          types.String `tfsdk:"id"`
	DatasetYaml types.String `tfsdk:"dataset_yaml"`
}

// datasetSlugFromYAML returns metadata.labels["dash0.com/id"], the dataset slug.
func datasetSlugFromYAML(datasetYaml string) (string, error) {
	var parsed struct {
		Metadata struct {
			Labels map[string]interface{} `yaml:"labels"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(datasetYaml), &parsed); err != nil {
		return "", fmt.Errorf("dataset_yaml is not valid YAML: %w", err)
	}
	slug, _ := parsed.Metadata.Labels[datasetIDLabel].(string)
	if slug == "" {
		return "", fmt.Errorf("dataset_yaml must set the dataset slug in metadata.labels[%q]", datasetIDLabel)
	}
	return slug, nil
}

// Configure adds the provider configured client to the resource.
func (r *DatasetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
}

func (r *DatasetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dataset"
}

func (r *DatasetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 dataset. Datasets are organization-level resources identified by an immutable slug.\n\n" +
			"~> **Warning:** Destroying a dataset soft-deletes it. After 30 days the dataset and ALL its telemetry are " +
			"purged permanently. Re-creating a dataset with the same slug within 30 days restores it.\n\n" +
			"The slug is set in `metadata.labels[\"dash0.com/id\"]` and cannot change: changing it replaces the dataset " +
			"(destroy, then create under the new slug). `spec.permissions` and `spec.telemetryFilters` are not managed by " +
			"this resource yet and are ignored when detecting drift. Spec settings you omit (retention, privacy options) " +
			"keep the server's values without causing a diff.",

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the dataset, generated by the provider on creation. " +
					"Null for datasets imported without an origin.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The dataset slug, taken from `metadata.labels[\"dash0.com/id\"]` in `dataset_yaml`.",
				Computed:    true,
			},
			"dataset_yaml": schema.StringAttribute{
				Description: "The dataset definition in YAML format, with `kind: Dash0Dataset`, `metadata.name` for the display " +
					"name, `metadata.labels[\"dash0.com/id\"]` for the slug, and optional `spec` settings. " +
					"Server-managed metadata is stripped from the state on read.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualConditionallyWith(datasetServerDefaultedFields, datasetServerManagedFields),
				},
			},
		},
	}
}

// ValidateConfig checks dataset_yaml at plan time: syntax, kind and slug.
func (r *DatasetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var yamlAttr types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("dataset_yaml"), &yamlAttr)...)
	if resp.Diagnostics.HasError() || yamlAttr.IsNull() || yamlAttr.IsUnknown() {
		return
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlAttr.ValueString()), &parsed); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("dataset_yaml"), "Invalid YAML in dataset_yaml",
			fmt.Sprintf("dataset_yaml is not valid YAML: %s", err))
		return
	}
	if kind, _ := parsed["kind"].(string); kind != datasetKind {
		resp.Diagnostics.AddAttributeError(path.Root("dataset_yaml"), "dataset_yaml is missing or has the wrong kind",
			fmt.Sprintf("dataset_yaml must declare `kind: %s`; got %q.", datasetKind, kind))
		return
	}
	if _, err := datasetSlugFromYAML(yamlAttr.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("dataset_yaml"), "dataset_yaml is missing the dataset slug", err.Error())
		return
	}
	if spec, ok := parsed["spec"].(map[string]interface{}); ok {
		for _, field := range []string{"permissions", "telemetryFilters"} {
			if _, set := spec[field]; set {
				resp.Diagnostics.AddAttributeWarning(path.Root("dataset_yaml"),
					fmt.Sprintf("spec.%s is not managed by dash0_dataset", field),
					fmt.Sprintf("spec.%s is ignored when detecting drift; changes made outside Terraform are not reverted.", field))
			}
		}
	}
}

// ModifyPlan sets id from the configured slug and forces replacement when the
// slug changes, because the Dash0 API treats the slug as immutable.
func (r *DatasetResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	// Read the slug from config: the plan value may have been replaced with the
	// state YAML, which ignores labels and so would hide a slug change.
	var cfg types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("dataset_yaml"), &cfg)...)
	if resp.Diagnostics.HasError() || cfg.IsNull() || cfg.IsUnknown() {
		return
	}
	slug, err := datasetSlugFromYAML(cfg.ValueString())
	if err != nil {
		return // ValidateConfig reports it
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), slug)...)

	if req.State.Raw.IsNull() {
		return // create
	}
	var stateID types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &stateID)...)
	if !stateID.IsNull() && stateID.ValueString() != slug {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("dataset_yaml"))
	}
}

// datasetJSON validates dataset_yaml and converts it to the JSON request body.
func datasetJSON(datasetYaml string, diags *diag.Diagnostics) (string, bool) {
	var parsed interface{}
	if err := yaml.Unmarshal([]byte(datasetYaml), &parsed); err != nil {
		diags.AddError("Invalid YAML", fmt.Sprintf("Dataset definition is not valid YAML: %s", err))
		return "", false
	}
	body, err := converter.ConvertYAMLToJSON(datasetYaml)
	if err != nil {
		diags.AddError("Conversion Error", fmt.Sprintf("Unable to convert dataset YAML to JSON: %s", err))
		return "", false
	}
	return body, true
}

func (r *DatasetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model datasetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, ok := datasetJSON(model.DatasetYaml.ValueString(), &resp.Diagnostics)
	if !ok {
		return
	}
	slug, err := datasetSlugFromYAML(model.DatasetYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Missing dataset slug", err.Error())
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())
	model.ID = types.StringValue(slug)

	if err := r.client.CreateDataset(ctx, model.Origin.ValueString(), body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create dataset, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a dataset resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *DatasetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state datasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetDataset(ctx, state.ID.ValueString())
	if removedOutOfBand(ctx, resp, "Dataset", state.ID.ValueString(), apiResponseJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read dataset, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a dataset resource")

	if stateYAML := state.DatasetYaml.ValueString(); stateYAML != "" {
		ignored := append(converter.FieldsAbsentFromYAML(stateYAML, datasetServerDefaultedFields), datasetServerManagedFields...)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, ignored, nil)
		if err != nil {
			resp.Diagnostics.AddError(
				"Dataset Comparison Error",
				fmt.Sprintf("Failed to compare dataset against the API response: %s. Leaving state.dataset_yaml unchanged.", err),
			)
			return
		}
		if !equivalent {
			tflog.Debug(ctx, "Dataset has changed, updating state")
			state.DatasetYaml = types.StringValue(apiResponseJSON)
		}
	} else {
		state.DatasetYaml = types.StringValue(apiResponseJSON)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DatasetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan datasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, ok := datasetJSON(plan.DatasetYaml.ValueString(), &resp.Diagnostics)
	if !ok {
		return
	}

	// The slug and origin are immutable across updates; a slug change is a replace.
	plan.ID = state.ID
	plan.Origin = state.Origin
	if err := r.client.UpdateDataset(ctx, plan.ID.ValueString(), plan.Origin.ValueString(), body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update dataset, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a dataset resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DatasetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state datasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteDataset(ctx, state.ID.ValueString()); err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Dataset %s was already gone at delete time; treating as success", state.ID.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete dataset, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a dataset resource")
}

// ImportState imports an existing dataset by its slug.
func (r *DatasetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	slug := req.ID

	apiResponseJSON, err := r.client.GetDataset(ctx, slug)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Dataset", fmt.Sprintf("Could not get dataset with slug=%s: %s", slug, err))
		return
	}

	var origin string
	var parsed struct {
		Metadata struct {
			Labels map[string]interface{} `yaml:"labels"`
		} `yaml:"metadata"`
	}
	if yaml.Unmarshal([]byte(apiResponseJSON), &parsed) == nil {
		origin, _ = parsed.Metadata.Labels[datasetOriginKey].(string)
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), slug)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), stringOrNull(origin))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset_yaml"), apiResponseJSON)...)
}
