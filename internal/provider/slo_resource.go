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
	_ resource.Resource                = &SLOResource{}
	_ resource.ResourceWithConfigure   = &SLOResource{}
	_ resource.ResourceWithImportState = &SLOResource{}
)

// NewSLOResource is a helper function to simplify the provider implementation.
func NewSLOResource() resource.Resource {
	return &SLOResource{}
}

// SLOResource is the resource implementation.
type SLOResource struct {
	client client.Client
}

// sloModel is the Terraform state model for an SLO resource.
type sloModel struct {
	Origin  types.String `tfsdk:"origin"`
	ID      types.String `tfsdk:"id"`
	Dataset types.String `tfsdk:"dataset"`
	SLOYaml types.String `tfsdk:"slo_yaml"`
	URL     types.String `tfsdk:"url"`
}

// Configure adds the provider configured client to the resource.
func (r *SLOResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SLOResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_slo"
}

func (r *SLOResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Manages a Dash0 Service Level Objective (SLO). SLOs define a reliability target for a service, measured against a service level indicator (SLI) derived from telemetry, using [OpenSLO](https://openslo.com) v1 documents. See [SLOs](https://dash0.com/docs/dash0/monitoring/alerting/slos) and [Manage SLOs as Code](https://dash0.com/docs/dash0/monitoring/alerting/manage-slos-as-code) for more details.`,
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the SLO, automatically generated on creation. Used to reference the SLO for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned UUID of the SLO, resolved by the provider after creation. Reference this value when wiring the SLO's identifier into another resource.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the SLO belongs to. Provide the dataset's identifier, which is immutable, not the 'name'. Datasets are used to separate observability data within a Dash0 organization. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"slo_yaml": schema.StringAttribute{
				Description: "The SLO definition in [OpenSLO](https://openslo.com) v1 YAML format (`apiVersion: openslo/v1`, `kind: SLO`), specifying the objective target, service level indicator, budgeting method, and time window. See [Create SLOs](https://dash0.com/docs/dash0/monitoring/alerting/create-slos) for the available options. The `dash0.com/sharing` metadata annotation is supported to control sharing settings; changes to it trigger a resource update. All other metadata annotations are managed by the server and ignored during drift detection.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(converter.AnnotationSharing),
				},
			},
			"url": schema.StringAttribute{
				Description: "The URL to open this SLO in the Dash0 web app, derived from the Dash0 API URL and the SLO's server-assigned identifier. Computed by the provider after creation. May be empty if the app URL cannot be derived (e.g. for self-hosted deployments with a custom web app domain).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// resolveSLO populates the SLO's server-assigned id and web app URL on the
// model by looking them up via the list endpoint. Both are best-effort
// metadata: failures are surfaced as warnings and leave the attributes null
// rather than failing the operation.
func (r *SLOResource) resolveSLO(ctx context.Context, model *sloModel, diags *diag.Diagnostics) {
	id, sloURL, err := r.client.ResolveSLO(ctx, model.Origin.ValueString(), model.Dataset.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve SLO metadata",
			fmt.Sprintf("The SLO was saved successfully, but its id and URL could not be determined: %s", err),
		)
		model.ID = types.StringNull()
		model.URL = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
	model.URL = stringOrNull(sloURL)
}

func (r *SLOResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model sloModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var sloYaml interface{}
	err := yaml.Unmarshal([]byte(model.SLOYaml.ValueString()), &sloYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("SLO definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.SLOYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert SLO YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateSLO(ctx, model.Origin.ValueString(), jsonBody, model.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create SLO, got error: %s", err))
		return
	}

	// Resolve the id and web app URL for the newly created SLO (best-effort).
	r.resolveSLO(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created an SLO resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *SLOResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state sloModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetSLO(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read SLO, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read an SLO resource")

	// Compare the current state with the retrieved SLO
	if state.SLOYaml.ValueString() != "" {
		stateYAML := state.SLOYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, []string{converter.AnnotationSharing})
		if err != nil {
			resp.Diagnostics.AddWarning(
				"SLO Comparison Error",
				fmt.Sprintf("Error comparing SLOs: %s. Using API response as source of truth.", err),
			)
			state.SLOYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "SLO has changed, updating state")
			state.SLOYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "SLO is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.SLOYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *SLOResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state sloModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan sloModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var sloYaml interface{}
	err := yaml.Unmarshal([]byte(plan.SLOYaml.ValueString()), &sloYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("SLO definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.SLOYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert SLO YAML to JSON: %s", err))
		return
	}

	// Update the existing SLO (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	// The SLO's identifier is immutable, so neither the id nor the URL change on
	// update; carry them from state instead of re-resolving them via the API.
	plan.ID = state.ID
	plan.URL = state.URL
	err = r.client.UpdateSLO(ctx, plan.Origin.ValueString(), jsonBody, plan.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update SLO, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated an SLO resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *SLOResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sloModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSLO(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete SLO, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted an SLO resource")
}

// ImportState function is required for resources that support import
func (r *SLOResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	apiResponseJSON, err := r.client.GetSLO(ctx, origin, dataset)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing SLO",
			fmt.Sprintf("Could not get SLO with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slo_yaml"), apiResponseJSON)...)

	// Resolve the id and web app URL (best-effort).
	model := sloModel{Origin: types.StringValue(origin), Dataset: types.StringValue(dataset)}
	r.resolveSLO(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("url"), model.URL)...)
}
