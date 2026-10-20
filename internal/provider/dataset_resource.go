package provider

import (
	"context"
	"fmt"
	"slices"

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
	datasetKind       = "Dash0Dataset"
	datasetAPIVersion = "dash0.com/v1alpha1"
	datasetIDLabel    = "dash0.com/id"
	datasetOriginKey  = "dash0.com/origin"
)

// datasetServerManagedFields never participate in drift detection: the
// resource does not manage them yet, or the server derives them.
var datasetServerManagedFields = []string{
	"spec.permittedActions",
	// spec.slug mirrors metadata.labels["dash0.com/id"] and is not part of the
	// dash0-api-client-go DatasetSpec schema, so it is silently dropped when
	// GetDataset round-trips the API response through that typed struct. The
	// raw API response (e.g. from `dash0 -X api`) still carries it, so without
	// this it would always show as drift between config and state.
	"spec.slug",
}

// datasetServerDefaultedFields are ignored only when the user did not set
// them, so the server may fill in defaults without causing a diff. Paths go
// down to the leaves, so setting one key (for example retention.logs) does not
// make its server-filled siblings drift. When the user sets a field, drift on
// it is detected.
var datasetServerDefaultedFields = []string{
	// The API does not return the admin role row, so it never drifts.
	"spec.permissions",
	"spec.preferred",
	"spec.retention",
	"spec.retention.traces",
	"spec.retention.logs",
	"spec.retention.metrics",
	"spec.retention.profiles",
	"spec.retention.webEvents",
	"spec.privacy",
	"spec.privacy.ipAddresses",
	"spec.privacy.ipAddresses.endUser",
	"spec.privacy.ipAddresses.endUser.storage",
	"spec.privacy.geoLocation",
	"spec.privacy.geoLocation.endUser",
	"spec.privacy.geoLocation.endUser.storage",
	"spec.privacy.redaction",
	"spec.privacy.redaction.cardholderData",
	"spec.privacy.redaction.cardholderData.enabled",
	"spec.privacy.redaction.genAi",
	"spec.privacy.redaction.genAi.conversation",
	"spec.privacy.redaction.genAi.toolCalls",
	"spec.privacy.redaction.genAi.errors",
	"spec.processing",
	"spec.processing.spanEventsToLogs",
	"spec.processing.semanticConventionUpgrades",
	"spec.processing.darkplane",
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
			"~> **Note:** Destroying a dataset soft-deletes it. Dashboards, views, check rules, synthetic checks, recording rules, SLOs and time series aggregations are kept for 30 days; " +
			"applying the same slug within that time restores them. Spam filters are not restored. After 30 days the dataset, its permissions and those assets are removed. " +
			"Telemetry is removed as its retention expires.\n\n" +
			"The slug is set in `metadata.labels[\"dash0.com/id\"]` and cannot change: changing it replaces the dataset " +
			"(destroy, then create under the new slug). Spec settings you omit (retention, privacy, processing, permissions) " +
			"keep the server's values without causing a diff.\n\n" +
			"`spec.permissions` lists the user, team and role grants on the dataset. When set, it replaces the grants on the server and " +
			"changes made outside Terraform show as drift. The admin role grant is managed by Dash0: it is not listed and never changes. " +
			"A grant cannot include `dataset:write`. An empty list does not remove grants: the server keeps them, so the plan keeps showing a change.",

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
				Description: "The dataset definition in YAML, following the Dash0 API dataset document: `apiVersion: dash0.com/v1alpha1`, " +
					"`kind: Dash0Dataset`, `metadata.name` for the display name, `metadata.labels[\"dash0.com/id\"]` for the slug, " +
					"and optional `spec` settings (`retention`, `privacy`, `processing`, `preferred`, `permissions`). " +
					"Server-managed metadata is stripped from the state on read.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualConditionallyWith(datasetServerDefaultedFields, datasetServerManagedFields),
				},
			},
		},
	}
}

// ValidateConfig checks dataset_yaml at plan time: syntax, apiVersion, kind,
// slug, and that no permission grants dataset:write.
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
	if v, _ := parsed["apiVersion"].(string); v != datasetAPIVersion {
		resp.Diagnostics.AddAttributeError(path.Root("dataset_yaml"), "dataset_yaml is missing or has an unsupported apiVersion",
			fmt.Sprintf("dataset_yaml must declare `apiVersion: %s`; got %q.", datasetAPIVersion, v))
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
	spec, _ := parsed["spec"].(map[string]interface{})
	permissions, _ := spec["permissions"].([]interface{})
	for i, p := range permissions {
		entry, _ := p.(map[string]interface{})
		actions, _ := entry["actions"].([]interface{})
		if slices.Contains(actions, interface{}("dataset:write")) {
			resp.Diagnostics.AddAttributeError(path.Root("dataset_yaml"), "spec.permissions grants dataset:write",
				fmt.Sprintf("spec.permissions[%d] grants dataset:write. Dash0 rejects write grants on datasets managed by Terraform: change the dataset in Terraform instead.", i))
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
		// Target id, not dataset_yaml: the semantic-equal modifier ignores labels, so
		// dataset_yaml shows no change on a slug-only edit and Terraform would skip it.
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("id"))
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

	// The server ignores spec.permissions on create. State is set first, so a
	// failure here taints the dataset instead of leaving it untracked.
	if len(converter.FieldsAbsentFromYAML(model.DatasetYaml.ValueString(), []string{"spec.permissions"})) == 0 {
		if err := r.client.UpdateDataset(ctx, slug, model.Origin.ValueString(), body); err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Dataset %s was created, but applying spec.permissions failed: %s", slug, err))
		}
	}
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

// ImportState imports an existing dataset by its slug or its origin. The state
// id always comes from the slug in the API response: taking it from the import
// ID would store the origin and make the next plan replace the dataset.
func (r *DatasetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	apiResponseJSON, err := r.client.GetDataset(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Dataset", fmt.Sprintf("Could not get dataset with slug or origin %q: %s", req.ID, err))
		return
	}

	slug, err := datasetSlugFromYAML(apiResponseJSON)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing Dataset", fmt.Sprintf("The API response for %q has no dataset slug: %s", req.ID, err))
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
