package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
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
	_ resource.Resource                   = &AwsLogGroupPolicyResource{}
	_ resource.ResourceWithConfigure      = &AwsLogGroupPolicyResource{}
	_ resource.ResourceWithImportState    = &AwsLogGroupPolicyResource{}
	_ resource.ResourceWithValidateConfig = &AwsLogGroupPolicyResource{}
)

// awsLogGroupPolicyIgnoredFields are ignored in drift detection: the server
// sets metadata.name to the integration id and ignores it on write.
var awsLogGroupPolicyIgnoredFields = []string{"metadata.name"}

// NewAwsLogGroupPolicyResource is a helper function to simplify the provider implementation.
func NewAwsLogGroupPolicyResource() resource.Resource {
	return &AwsLogGroupPolicyResource{}
}

// AwsLogGroupPolicyResource manages the log-group instrumentation policy of one AWS integration.
type AwsLogGroupPolicyResource struct {
	client client.Client
}

type awsLogGroupPolicyModel struct {
	IntegrationID         types.String `tfsdk:"integration_id"`
	Origin                types.String `tfsdk:"origin"`
	DeletionDisposition   types.String `tfsdk:"deletion_disposition"`
	AwsLogGroupPolicyYaml types.String `tfsdk:"aws_log_group_policy_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *AwsLogGroupPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AwsLogGroupPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_log_group_policy"
}

func (r *AwsLogGroupPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the log-group instrumentation policy of a Dash0 AWS integration: " +
			"which CloudWatch log groups Dash0 forwards, selected by the log groups' own tags.\n\n" +
			"Each AWS integration has at most one policy. " +
			"Declare at most one `dash0_aws_log_group_policy` per integration. " +
			"The policy belongs to the integration's dataset, so the resource has no `dataset` attribute.\n\n" +
			"A log group is forwarded when its tags match any of `spec.rules`; all conditions of a rule must hold. " +
			"Log groups selected by hand in Dash0 keep forwarding regardless of the policy.\n\n" +
			"Terraform writes the whole policy on every apply, without a revision check, so changes made in the Dash0 UI are overwritten. " +
			"Dash0 records the writer of a policy as its owner (the `origin`). " +
			"If another client already owns an active policy for the integration, the first apply fails with a conflict: " +
			"import the policy, or delete it in Dash0, first.\n\n" +
			"Destroying the resource deletes the policy. " +
			"`deletion_disposition` decides what happens to the log groups it forwards: `keep` (the default) keeps them forwarding, " +
			"`remove` removes their subscription filters. " +
			"The AWS integration itself is kept.",

		Attributes: map[string]schema.Attribute{
			"integration_id": schema.StringAttribute{
				Description: "The id (UUID) of the Dash0 AWS integration whose log-group policy this resource manages. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"origin": schema.StringAttribute{
				Description: "The `dash0.com/origin` label of the policy, which records who manages it. Generated on creation as `tf_<uuid>` and refreshed from Dash0 on read. Null for a policy imported without an origin.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"deletion_disposition": schema.StringAttribute{
				Description: "What happens to the log groups the policy forwards when the resource is destroyed or replaced: " +
					"`keep` keeps them forwarding, `remove` removes their subscription filters. Defaults to `keep`. " +
					"Set it to `remove` and apply before destroying to stop forwarding.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(string(dash0.AwsLogGroupPolicyDispositionKeep)),
			},
			"aws_log_group_policy_yaml": schema.StringAttribute{
				Description: "The policy in YAML format, with `apiVersion: dash0.com/v1alpha1`, `kind: Dash0AwsLogGroupInstrumentationPolicy` " +
					"and a `spec.rules` list. Each rule has a `conditions` list; a condition has a `key` " +
					"(a log-group tag written as `dash0.cloud.tags.<tag name>`), an `operator` " +
					"(`is`, `is_one_of`, `is_set`, `starts_with`, `ends_with` or `contains`), and a `value` or, for `is_one_of`, `values`. " +
					"`spec.onNoLongerMatching` (`keep` or `remove`, default `keep`) decides what happens to a log group that stops matching. " +
					"`metadata` is managed by Dash0 and ignored.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualWith(awsLogGroupPolicyIgnoredFields),
				},
			},
		},
	}
}

// ValidateConfig checks integration_id, deletion_disposition and the YAML at plan time.
func (r *AwsLogGroupPolicyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model awsLogGroupPolicyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if id := model.IntegrationID; !id.IsNull() && !id.IsUnknown() && !isUUID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("integration_id"), "Invalid integration_id",
			fmt.Sprintf("integration_id must be the UUID of a Dash0 AWS integration, got %q.", id.ValueString()))
	}

	if d := model.DeletionDisposition; !d.IsNull() && !d.IsUnknown() && !dash0.AwsLogGroupPolicyDisposition(d.ValueString()).Valid() {
		resp.Diagnostics.AddAttributeError(path.Root("deletion_disposition"), "Invalid deletion_disposition",
			fmt.Sprintf("deletion_disposition must be %q or %q, got %q.", dash0.AwsLogGroupPolicyDispositionKeep, dash0.AwsLogGroupPolicyDispositionRemove, d.ValueString()))
	}

	if y := model.AwsLogGroupPolicyYaml; !y.IsNull() && !y.IsUnknown() {
		var parsed map[string]interface{}
		if err := yaml.Unmarshal([]byte(y.ValueString()), &parsed); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("aws_log_group_policy_yaml"), "Invalid YAML",
				fmt.Sprintf("aws_log_group_policy_yaml is not valid YAML: %s", err))
			return
		}
		if _, err := parseAwsLogGroupPolicyYAML(y.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("aws_log_group_policy_yaml"), "Invalid AWS log-group policy", err.Error())
		}
	}
}

func (r *AwsLogGroupPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model awsLogGroupPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())
	if !r.put(ctx, &model, &resp.Diagnostics) {
		return
	}

	tflog.Trace(ctx, "created an AWS log-group policy resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *AwsLogGroupPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan awsLogGroupPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The origin is planned from state, so an imported policy without one keeps none.
	plan.Origin = state.Origin
	if !plan.AwsLogGroupPolicyYaml.Equal(state.AwsLogGroupPolicyYaml) && !r.put(ctx, &plan, &resp.Diagnostics) {
		return
	}

	tflog.Trace(ctx, "updated an AWS log-group policy resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// put creates or replaces the integration's policy with the model's YAML.
func (r *AwsLogGroupPolicyResource) put(ctx context.Context, model *awsLogGroupPolicyModel, diags *diag.Diagnostics) bool {
	jsonBody, err := converter.ConvertYAMLToJSON(model.AwsLogGroupPolicyYaml.ValueString())
	if err != nil {
		diags.AddError("Invalid YAML", fmt.Sprintf("Unable to convert AWS log-group policy YAML to JSON: %s", err))
		return false
	}
	if err := r.client.UpdateAwsLogGroupPolicy(ctx, model.IntegrationID.ValueString(), model.Origin.ValueString(), jsonBody); err != nil {
		if dash0.IsConflict(err) {
			diags.AddError("AWS Log-Group Policy Owned Elsewhere",
				fmt.Sprintf("Another client manages the log-group policy of integration %s. Import it with `terraform import`, or delete it in Dash0, then apply again. Got error: %s",
					model.IntegrationID.ValueString(), err))
			return false
		}
		diags.AddError("Client Error", fmt.Sprintf("Unable to write AWS log-group policy, got error: %s", err))
		return false
	}
	return true
}

func (r *AwsLogGroupPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsLogGroupPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	integrationID := state.IntegrationID.ValueString()
	apiJSON, origin, deleted, err := r.client.GetAwsLogGroupPolicy(ctx, integrationID)
	if err != nil && dash0.IsNotFound(err) || err == nil && deleted {
		tflog.Debug(ctx, fmt.Sprintf("AWS log-group policy of integration %s is gone; removing from state", integrationID))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read AWS log-group policy, got error: %s", err))
		return
	}

	state.Origin = stringOrNull(origin)
	if state.DeletionDisposition.IsNull() {
		state.DeletionDisposition = types.StringValue(string(dash0.AwsLogGroupPolicyDispositionKeep))
	}
	equivalent, err := awsLogGroupPolicyEquivalent(state.AwsLogGroupPolicyYaml.ValueString(), apiJSON)
	if err != nil {
		resp.Diagnostics.AddWarning("AWS Log-Group Policy Comparison Error",
			fmt.Sprintf("Error comparing AWS log-group policies: %s. Using API response as source of truth.", err))
		state.AwsLogGroupPolicyYaml = types.StringValue(apiJSON)
	} else if !equivalent {
		tflog.Debug(ctx, "AWS log-group policy has changed, updating state")
		state.AwsLogGroupPolicyYaml = types.StringValue(apiJSON)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// awsLogGroupPolicyEquivalent compares the state YAML with the API JSON after
// filling in the defaults the server applies, so omitting
// spec.onNoLongerMatching does not show up as drift.
func awsLogGroupPolicyEquivalent(stateYAML, apiJSON string) (bool, error) {
	policy, err := parseAwsLogGroupPolicyYAML(stateYAML)
	if err != nil {
		return false, err
	}
	if policy.Spec.OnNoLongerMatching == nil {
		keep := dash0.AwsLogGroupPolicyDispositionKeep
		policy.Spec.OnNoLongerMatching = &keep
	}
	for i := range policy.Spec.Rules {
		if policy.Spec.Rules[i].Conditions == nil {
			policy.Spec.Rules[i].Conditions = []dash0.AwsLogGroupPolicyCondition{}
		}
	}
	policy.Metadata = dash0.AwsLogGroupPolicyMetadata{}
	normalized, err := json.Marshal(policy)
	if err != nil {
		return false, err
	}
	return converter.ResourceYAMLEquivalent(string(normalized), apiJSON, awsLogGroupPolicyIgnoredFields, nil)
}

func parseAwsLogGroupPolicyYAML(policyYAML string) (*dash0.AwsLogGroupInstrumentationPolicy, error) {
	policyJSON, err := converter.ConvertYAMLToJSON(policyYAML)
	if err != nil {
		return nil, err
	}
	return client.ParseAwsLogGroupPolicy(policyJSON)
}

func (r *AwsLogGroupPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsLogGroupPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	disposition := state.DeletionDisposition.ValueString()
	if disposition == "" {
		disposition = string(dash0.AwsLogGroupPolicyDispositionKeep)
	}
	if err := r.client.DeleteAwsLogGroupPolicy(ctx, state.IntegrationID.ValueString(), disposition); err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("AWS log-group policy of integration %s is gone; treating delete as successful", state.IntegrationID.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete AWS log-group policy, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted an AWS log-group policy resource")
}

// ImportState imports the policy of the AWS integration whose id is the import ID.
func (r *AwsLogGroupPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	integrationID := req.ID
	if !isUUID(integrationID) {
		resp.Diagnostics.AddError("Invalid Import ID",
			fmt.Sprintf("Expected the UUID of a Dash0 AWS integration as import ID. Got: %s", integrationID))
		return
	}

	apiJSON, origin, deleted, err := r.client.GetAwsLogGroupPolicy(ctx, integrationID)
	if err == nil && deleted || err != nil && dash0.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Importing AWS Log-Group Policy",
			fmt.Sprintf("AWS integration %s has no log-group policy to import. Create the resource instead.", integrationID))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error Importing AWS Log-Group Policy",
			fmt.Sprintf("Could not get AWS log-group policy of integration %s: %s", integrationID, err))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("integration_id"), integrationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), stringOrNull(origin))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("deletion_disposition"), string(dash0.AwsLogGroupPolicyDispositionKeep))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("aws_log_group_policy_yaml"), apiJSON)...)
}
