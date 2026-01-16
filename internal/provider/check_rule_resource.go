package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
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
	_ resource.Resource                = &CheckRuleResource{}
	_ resource.ResourceWithConfigure   = &CheckRuleResource{}
	_ resource.ResourceWithImportState = &CheckRuleResource{}
)

// NewCheckRuleResource is a helper function to simplify the provider implementation.
func NewCheckRuleResource() resource.Resource {
	return &CheckRuleResource{}
}

// CheckRuleResource is the resource implementation.
type CheckRuleResource struct {
	client client.Client
}

// Configure adds the provider configured client to the resource.
func (r *CheckRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected dash0ClientInterface, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *CheckRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check_rule"
}

func (r *CheckRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Manages a Dash0 Check Rule (in Prometheus Rule format).

More information on how prometheus rules are mapped to Dash0 check rules can be found in the [Dash0 Operator documentation](https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#managing-dash0-check-rules).`,

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
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"check_rule_yaml": schema.StringAttribute{
				Description: "The check rule definition in YAML format (Prometheus Rule format).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

func (r *CheckRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var checkRuleModel model.CheckRule
	diags := req.Plan.Get(ctx, &checkRuleModel)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	checkRuleModel.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var checkRuleYaml interface{}
	err := yaml.Unmarshal([]byte(checkRuleModel.CheckRuleYaml.ValueString()), &checkRuleYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Check rule definition is not valid YAML: %s", err),
		)
		return
	}

	err = r.client.CreateCheckRule(ctx, checkRuleModel)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "created a check rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, checkRuleModel)
	resp.Diagnostics.Append(diags...)
}

func (r *CheckRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state model.CheckRule
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	check, err := r.client.GetCheckRule(ctx, state.Dataset.ValueString(), state.Origin.ValueString())
	if err != nil {
		// Handle 404 case by returning an empty state
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a check rule resource")

	// Compare the current state with the retrieved check rule
	// Only update state if there's a significant change (ignoring certain fields)
	if state.CheckRuleYaml.ValueString() != "" {
		equivalent, err := converter.ResourceYAMLEquivalent(state.CheckRuleYaml.ValueString(), check.CheckRuleYaml.ValueString())
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Check Rule Comparison Error",
				fmt.Sprintf("Error comparing check rules: %s. Using API response as source of truth.", err),
			)
			// Fall back to updating with API response on error
			state.CheckRuleYaml = check.CheckRuleYaml
		} else if !equivalent {
			// Only update if check rules are not equivalent
			tflog.Debug(ctx, "Check rule has changed, updating state")
			state.CheckRuleYaml = check.CheckRuleYaml
		} else {
			tflog.Debug(ctx, "Check rule is equivalent, ignoring changes in metadata fields")
			// Keep the current state since the check rules are equivalent
		}
	} else {
		// If there's no current check rule YAML, use the one from the API
		state.CheckRuleYaml = check.CheckRuleYaml
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *CheckRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state model.CheckRule
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan model.CheckRule
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

	// Update the existing check rule (dataset changes force recreation via RequiresReplace)
	plan.Origin = state.Origin
	err = r.client.UpdateCheckRule(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a check rule resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *CheckRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Get current state
	var state model.CheckRule
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCheckRule(ctx, state.Origin.ValueString(), state.Dataset.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(" 6", fmt.Sprintf("Unable to delete check rule, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a check rule resource")
}

// ImportState function is required for resources that support import
func (r *CheckRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
	check, err := r.client.GetCheckRule(ctx, dataset, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Check Rule",
			fmt.Sprintf("Could not get check rule with origin=%s, dataset=%s: %s", origin, dataset, err),
		)
		return
	}

	// Set the resource state with the retrieved check rule
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), check.Origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), check.Dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("check_rule_yaml"), check.CheckRuleYaml)...)
}
