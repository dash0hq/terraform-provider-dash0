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
	_ resource.Resource                = &SyntheticCheckResource{}
	_ resource.ResourceWithConfigure   = &SyntheticCheckResource{}
	_ resource.ResourceWithImportState = &SyntheticCheckResource{}
)

// NewSyntheticCheckResource is a helper function to simplify the provider implementation.
func NewSyntheticCheckResource() resource.Resource {
	return &SyntheticCheckResource{}
}

// SyntheticCheckResource is the resource implementation.
type SyntheticCheckResource struct {
	client client.Client
}

// syntheticCheckModel is the Terraform state model for a synthetic check resource.
type syntheticCheckModel struct {
	Origin             types.String `tfsdk:"origin"`
	ID                 types.String `tfsdk:"id"`
	Dataset            types.String `tfsdk:"dataset"`
	SyntheticCheckYaml types.String `tfsdk:"synthetic_check_yaml"`
	URL                types.String `tfsdk:"url"`
}

// Configure adds the provider configured client to the resource.
func (r *SyntheticCheckResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SyntheticCheckResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_synthetic_check"
}

func (r *SyntheticCheckResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Manages a Dash0 Synthetic Check. Synthetic checks periodically probe endpoints or URLs from multiple locations to monitor availability, latency, and correctness of your services. See [Synthetic Monitoring](https://dash0.com/docs/dash0/monitoring/synthetics/synthetic-monitoring) and [Manage Synthetic Checks as Code](https://dash0.com/docs/dash0/monitoring/synthetics/manage-synthetic-checks-as-code) for more details.`,
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the synthetic check, automatically generated on creation. Used to reference the synthetic check for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned UUID of the synthetic check, resolved by the provider after creation. Reference this value when wiring the check's identifier into another resource (for example, a check rule that gates on the synthetic check's outcome).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the synthetic check belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"synthetic_check_yaml": schema.StringAttribute{
				Description: "The synthetic check definition in YAML format, specifying the check type, target URL, schedule, and assertion criteria. See [Create Synthetic Checks](https://dash0.com/docs/dash0/monitoring/synthetics/create-synthetic-checks) for the available options. The `dash0.com/sharing` metadata annotation is supported to control sharing settings; changes to it trigger a resource update. All other metadata annotations are managed by the server and ignored during drift detection.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(converter.AnnotationSharing),
				},
			},
			"url": schema.StringAttribute{
				Description: "The URL to open this synthetic check in the Dash0 web app, derived from the Dash0 API URL and the synthetic check's server-assigned identifier. Computed by the provider after creation. May be empty if the app URL cannot be derived (e.g. for self-hosted deployments with a custom web app domain).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// resolveSyntheticCheck populates the synthetic check's server-assigned id and
// web app URL on the model by looking them up via the list endpoint. Both are
// best-effort metadata: failures are surfaced as warnings and leave the
// attributes null rather than failing the operation.
func (r *SyntheticCheckResource) resolveSyntheticCheck(ctx context.Context, model *syntheticCheckModel, diags *diag.Diagnostics) {
	id, syntheticCheckURL, err := r.client.ResolveSyntheticCheck(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve synthetic check metadata",
			fmt.Sprintf("The synthetic check was saved successfully, but its id and URL could not be determined: %s", err),
		)
		model.ID = types.StringNull()
		model.URL = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
	model.URL = stringOrNull(syntheticCheckURL)
}

func (r *SyntheticCheckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model syntheticCheckModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var checkYaml interface{}
	err := yaml.Unmarshal([]byte(model.SyntheticCheckYaml.ValueString()), &checkYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Synthetic check definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.SyntheticCheckYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert synthetic check YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateSyntheticCheck(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create synthetic check, got error: %s", err))
		return
	}

	// Resolve the id and web app URL for the newly created synthetic check (best-effort).
	r.resolveSyntheticCheck(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a synthetic check resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *SyntheticCheckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state syntheticCheckModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetSyntheticCheck(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read synthetic check, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a synthetic check resource")

	// Compare the current state with the retrieved synthetic check
	if state.SyntheticCheckYaml.ValueString() != "" {
		stateYAML := state.SyntheticCheckYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, []string{converter.AnnotationSharing})
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Synthetic Check Comparison Error",
				fmt.Sprintf("Error comparing synthetic checks: %s. Using API response as source of truth.", err),
			)
			state.SyntheticCheckYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Synthetic check has changed, updating state")
			state.SyntheticCheckYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Synthetic check is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.SyntheticCheckYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *SyntheticCheckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state syntheticCheckModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan syntheticCheckModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var checkYaml interface{}
	err := yaml.Unmarshal([]byte(plan.SyntheticCheckYaml.ValueString()), &checkYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Synthetic check definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.SyntheticCheckYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert synthetic check YAML to JSON: %s", err))
		return
	}

	// Update the existing synthetic check (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The synthetic check's identifier is immutable, so neither the id nor the
	// URL change on update; carry them from state instead of re-resolving them
	// via the API.
	plan.ID = state.ID
	plan.URL = state.URL
	err = r.client.UpdateSyntheticCheck(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update synthetic check, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a synthetic check resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *SyntheticCheckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state syntheticCheckModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSyntheticCheck(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete synthetic check, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a synthetic check resource")
}

// ImportState function is required for resources that support import
func (r *SyntheticCheckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetSyntheticCheck(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Synthetic Check",
			fmt.Sprintf("Could not get synthetic check with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("synthetic_check_yaml"), apiResponseJSON)...)

	// Resolve the id and web app URL (best-effort).
	model := syntheticCheckModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveSyntheticCheck(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("url"), model.URL)...)
}
