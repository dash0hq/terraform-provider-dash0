package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
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
	_ resource.Resource                = &SpamFilterResource{}
	_ resource.ResourceWithConfigure   = &SpamFilterResource{}
	_ resource.ResourceWithImportState = &SpamFilterResource{}
)

// NewSpamFilterResource is a helper function to simplify the provider implementation.
func NewSpamFilterResource() resource.Resource {
	return &SpamFilterResource{}
}

// SpamFilterResource is the resource implementation.
type SpamFilterResource struct {
	client client.Client
}

// spamFilterModel is the Terraform state model for a spam filter resource.
type spamFilterModel struct {
	Origin         types.String `tfsdk:"origin"`
	Dataset        types.String `tfsdk:"dataset"`
	SpamFilterYaml types.String `tfsdk:"spam_filter_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *SpamFilterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SpamFilterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spam_filter"
}

func (r *SpamFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Spam Filter. Spam filters allow you to drop noisy or unwanted telemetry data " +
			"before it is stored, reducing costs and improving signal-to-noise ratio. " +
			"Filters are scoped to a dataset and can target logs, spans, or metrics.\n\n" +
			"See [About Spam Filters](https://dash0.com/docs/dash0/data-management/spam-filters) for more details.",

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the spam filter, automatically generated on creation. Used to reference the spam filter for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) that the spam filter belongs to. Datasets are used to separate observability data within a Dash0 organization. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"spam_filter_yaml": schema.StringAttribute{
				Description: "The spam filter definition in YAML format. " +
					"The YAML must include a `metadata.name` field and a `spec` with `contexts` (list of signal types: `log`, `span`, `metric`) " +
					"and `filter` (list of key-value matchers). " +
					"See [About Spam Filters](https://dash0.com/docs/dash0/data-management/spam-filters) for the available options.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

func (r *SpamFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model spamFilterModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var spamFilterYaml interface{}
	err := yaml.Unmarshal([]byte(model.SpamFilterYaml.ValueString()), &spamFilterYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Spam filter definition is not valid YAML: %s", err),
		)
		return
	}

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
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read spam filter, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a spam filter resource")

	// Compare the current state with the retrieved spam filter
	if state.SpamFilterYaml.ValueString() != "" {
		stateYAML := state.SpamFilterYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored...)
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

	// Validate YAML format
	var spamFilterYaml interface{}
	err := yaml.Unmarshal([]byte(plan.SpamFilterYaml.ValueString()), &spamFilterYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Spam filter definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.SpamFilterYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert spam filter YAML to JSON: %s", err))
		return
	}

	// Update the existing spam filter (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
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
}
