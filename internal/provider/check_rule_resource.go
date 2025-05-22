package provider

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"strings"

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
	_ resource.Resource                = &checkRuleResource{}
	_ resource.ResourceWithConfigure   = &checkRuleResource{}
	_ resource.ResourceWithImportState = &checkRuleResource{}
)

// NewCheckRuleResource is a helper function to simplify the provider implementation.
func NewCheckRuleResource() resource.Resource {
	return &checkRuleResource{}
}

// checkRuleResource is the resource implementation.
type checkRuleResource struct {
	client dash0ClientInterface
}

type checkRuleResourceModel struct {
	Origin        types.String `tfsdk:"origin"`
	Dataset       types.String `tfsdk:"dataset"`
	CheckRuleYaml types.String `tfsdk:"check_rule_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *checkRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(dash0ClientInterface)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected dash0ClientInterface, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *checkRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check_rule"
}

func (r *checkRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Check Rule (in Prometheus Rule format).",
		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "Identifier of the check rule.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The dataset for which the check rule is created.",
				Required:    true,
			},
			"check_rule_yaml": schema.StringAttribute{
				Description: "The check rule definition in YAML format (Prometheus Rule format).",
				Required:    true,
			},
		},
	}
}

func (r *checkRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model checkRuleResourceModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var checkRuleYaml interface{}
	err := yaml.Unmarshal([]byte(model.CheckRuleYaml.ValueString()), &checkRuleYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Check rule definition is not valid YAML: %s", err),
		)
		return
	}

	err = r.client.CreateCheckRule(ctx, model)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a check rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *checkRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state checkRuleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	checkRule, err := r.client.GetCheckRule(ctx, state.Dataset.ValueString(), state.Origin.ValueString())
	if err != nil {
		// Handle 404 case by returning an error
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a check rule resource")

	// Update state with retrieved data
	state.CheckRuleYaml = checkRule.CheckRuleYaml

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *checkRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state checkRuleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan checkRuleResourceModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var checkRuleYaml interface{}
	err := yaml.Unmarshal([]byte(plan.CheckRuleYaml.ValueString()), &checkRuleYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Check rule definition is not valid YAML: %s", err),
		)
		return
	}

	// Check if dataset has changed
	datasetChanged := state.Dataset.ValueString() != plan.Dataset.ValueString()

	if datasetChanged {
		tflog.Info(ctx, fmt.Sprintf("Dataset changed from %s to %s, recreating check rule",
			state.Dataset.ValueString(), plan.Dataset.ValueString()))

		// Delete the existing check rule
		err := r.client.DeleteCheckRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to delete old check rule when changing dataset, got error: %s", err))
			return
		}

		// Create a new check rule in the new dataset
		err = r.client.CreateCheckRule(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to create check rule in new dataset, got error: %s", err))
			return
		}

		tflog.Trace(ctx, "recreated check rule resource in new dataset")
	} else {
		// Standard update (same dataset)
		err := r.client.UpdateCheckRule(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to update check rule, got error: %s", err))
			return
		}

		tflog.Trace(ctx, "updated check rule resource")
	}

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *checkRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state checkRuleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCheckRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a check rule resource")
}

// ImportState function is required for resources that support import
func (r *checkRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Expect the import ID in the format "dataset,origin"
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

	// Retrieve the check rule using the client
	checkRule, err := r.client.GetCheckRule(ctx, dataset, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Check Rule",
			fmt.Sprintf("Could not get check rule with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Set the state with values from the imported check rule
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("check_rule_yaml"), checkRule.CheckRuleYaml)...)
}