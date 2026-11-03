package provider

import (
	"context"
	"fmt"
	"slices"
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

const customSkillEnabledKey = "dash0.com/enabled"

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &CustomSkillResource{}
	_ resource.ResourceWithConfigure      = &CustomSkillResource{}
	_ resource.ResourceWithImportState    = &CustomSkillResource{}
	_ resource.ResourceWithValidateConfig = &CustomSkillResource{}
	_ resource.ResourceWithModifyPlan     = &CustomSkillResource{}
)

// NewCustomSkillResource is a helper function to simplify the provider implementation.
func NewCustomSkillResource() resource.Resource {
	return &CustomSkillResource{}
}

// CustomSkillResource manages an Agent0 custom skill.
type CustomSkillResource struct {
	client client.Client
}

// customSkillModel is the Terraform state model for a custom skill resource.
type customSkillModel struct {
	Origin          types.String `tfsdk:"origin"`
	ID              types.String `tfsdk:"id"`
	CustomSkillYaml types.String `tfsdk:"custom_skill_yaml"`
}

// address returns the identifier the API addresses the skill by: its origin,
// or its id for a skill created in the Dash0 UI (which has no origin) and
// adopted with terraform import.
func (m customSkillModel) address() string {
	if origin := m.Origin.ValueString(); origin != "" {
		return origin
	}
	return m.ID.ValueString()
}

// Configure adds the provider configured client to the resource.
func (r *CustomSkillResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CustomSkillResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_custom_skill"
}

// ValidateConfig rejects, at plan time, fields that the Dash0 API refuses
// from Terraform.
func (r *CustomSkillResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model customSkillModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.CustomSkillYaml.IsNull() || model.CustomSkillYaml.IsUnknown() {
		return
	}
	validateCustomSkillYAML(model.CustomSkillYaml.ValueString(), &resp.Diagnostics)
}

// customSkillDocument is the part of a custom skill document that the
// plan-time checks read.
type customSkillDocument struct {
	Metadata struct {
		Labels      map[string]interface{} `yaml:"labels"`
		Annotations map[string]interface{} `yaml:"annotations"`
	} `yaml:"metadata"`
	Spec struct {
		Source      interface{} `yaml:"source"`
		Permissions []struct {
			Role    string   `yaml:"role"`
			Actions []string `yaml:"actions"`
		} `yaml:"permissions"`
	} `yaml:"spec"`
}

// rejectCustomSkillWriteGrants adds an error for each grant of
// custom_skill:write, which the Dash0 API rejects on a skill with a tf_
// origin. The admin entry is exempt: the server fixes its actions and ignores
// the ones sent. Invalid YAML is left to Create and Update, which report it.
func rejectCustomSkillWriteGrants(skillYAML string, diags *diag.Diagnostics) {
	var doc customSkillDocument
	if err := yaml.Unmarshal([]byte(skillYAML), &doc); err != nil {
		return
	}
	for i, grant := range doc.Spec.Permissions {
		if grant.Role == "admin" || !slices.Contains(grant.Actions, string(dash0.CustomSkillWrite)) {
			continue
		}
		diags.AddAttributeError(
			path.Root("custom_skill_yaml"),
			"custom_skill:write cannot be granted on a Terraform-managed skill",
			fmt.Sprintf("spec.permissions[%d] grants %s. The Dash0 API rejects write grants on skills managed by Terraform, "+
				"because the Terraform configuration is the only source of the skill's content. "+
				"Grant %s and, optionally, %s instead.",
				i, dash0.CustomSkillWrite, dash0.CustomSkillRead, dash0.CustomSkillDelete),
		)
	}
}

// validateCustomSkillYAML adds an error for each field of the skill document
// that the Dash0 API rejects from Terraform. Invalid YAML is left to Create
// and Update, which report it.
func validateCustomSkillYAML(skillYAML string, diags *diag.Diagnostics) {
	var doc customSkillDocument
	if err := yaml.Unmarshal([]byte(skillYAML), &doc); err != nil {
		return
	}

	if doc.Spec.Source != nil {
		diags.AddAttributeError(
			path.Root("custom_skill_yaml"),
			"spec.source is not supported on a Terraform-managed skill",
			"spec.source records the repository a skill was imported from in the Dash0 UI. "+
				"The Dash0 API rejects it on a skill managed by Terraform. Remove spec.source and declare the skill's content in spec.files.",
		)
	}

	_, inLabels := doc.Metadata.Labels[customSkillEnabledKey]
	_, inAnnotations := doc.Metadata.Annotations[customSkillEnabledKey]
	if inLabels || inAnnotations {
		diags.AddAttributeError(
			path.Root("custom_skill_yaml"),
			customSkillEnabledKey+" cannot be managed with Terraform",
			"Whether a skill is enabled is a per-user choice, and the Dash0 API rejects \"false\" from an API token. "+
				"To ship a skill that is off by default, set defaultEnabled: false on the grants in spec.permissions. "+
				"That is supported only in organizations where custom skills belong to their author.",
		)
	}
}

// ModifyPlan rejects, at plan time, grants of custom_skill:write on a skill
// that Terraform creates or that has a tf_ origin. A skill created in the
// Dash0 UI or with the Dash0 CLI and adopted with terraform import keeps
// accepting them.
func (r *CustomSkillResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan customSkillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.CustomSkillYaml.IsNull() || plan.CustomSkillYaml.IsUnknown() {
		return
	}
	if !req.State.Raw.IsNull() {
		var state customSkillModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() || !strings.HasPrefix(state.Origin.ValueString(), "tf_") {
			return
		}
	}
	rejectCustomSkillWriteGrants(plan.CustomSkillYaml.ValueString(), &resp.Diagnostics)
}

func (r *CustomSkillResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Agent0 custom skill. A custom skill is a set of files, led by a `SKILL.md`, " +
			"that Agent0 can load to follow your organization's instructions. Custom skills are " +
			"organization-level resources and are not scoped to a dataset.\n\n" +
			"Skills managed by Terraform accept no `custom_skill:write` grant: members can read them, and " +
			"optionally delete them, but only the Terraform configuration changes their content.",

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the custom skill, automatically generated on creation. Used to reference the custom skill for updates, reads, deletes, and imports. Null for a skill created in the Dash0 UI and adopted with `terraform import`; such a skill is addressed by its `id`.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned identifier of the custom skill.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"custom_skill_yaml": schema.StringAttribute{
				Description: "The custom skill definition in YAML format. " +
					"The YAML must include `kind: Dash0Skill` and `spec.files`, a list of `relativePath` and `content` pairs that must include a `SKILL.md`. " +
					"The skill's name is read from the `name` field of the `SKILL.md` frontmatter; `metadata.name` is derived by the server and ignored. " +
					"The optional `spec.permissions` list grants `custom_skill:read` and `custom_skill:delete` to a `role` (`admin` or `basic_member`), a `teamId` or a `userId`, " +
					"and replaces the stored grants exactly. When `spec.permissions` is omitted, the grants are left as they are, and a new skill gets the server defaults. " +
					"An `admin` entry sets only `defaultEnabled`; the server always adds an `admin` grant, which drift detection ignores unless you declare one. " +
					"`custom_skill:write` is rejected on skills Terraform creates, and `spec.source` and `dash0.com/enabled` are always rejected. " +
					"To ship a skill that is off by default, set `defaultEnabled: false` on its grants; this is supported only in organizations where custom skills belong to their author.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualComparing(converter.CustomSkillYAMLEquivalent),
				},
			},
		},
	}
}

// customSkillJSON validates the YAML and converts it to the JSON body the API expects.
func customSkillJSON(skillYAML string, diags *diag.Diagnostics) (string, bool) {
	var parsed interface{}
	if err := yaml.Unmarshal([]byte(skillYAML), &parsed); err != nil {
		diags.AddError("Invalid YAML", fmt.Sprintf("Custom skill definition is not valid YAML: %s", err))
		return "", false
	}
	jsonBody, err := converter.ConvertYAMLToJSON(skillYAML)
	if err != nil {
		diags.AddError("Conversion Error", fmt.Sprintf("Unable to convert custom skill YAML to JSON: %s", err))
		return "", false
	}
	return jsonBody, true
}

func (r *CustomSkillResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model customSkillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	jsonBody, ok := customSkillJSON(model.CustomSkillYaml.ValueString(), &resp.Diagnostics)
	if !ok {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	id, err := r.client.CreateCustomSkill(ctx, model.Origin.ValueString(), jsonBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create custom skill, got error: %s", err))
		return
	}
	model.ID = stringOrNull(id)

	tflog.Trace(ctx, "created a custom skill resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *CustomSkillResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customSkillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetCustomSkill(ctx, state.address())
	if removedOutOfBand(ctx, resp, "Custom skill", state.address(), apiResponseJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read custom skill, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a custom skill resource")

	if id, _ := client.CustomSkillIdentity(apiResponseJSON); id != "" {
		state.ID = types.StringValue(id)
	}

	stateYAML := state.CustomSkillYaml.ValueString()
	if stateYAML != "" {
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.CustomSkillYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Custom Skill Comparison Error",
				fmt.Sprintf("Error comparing custom skills: %s. Using API response as source of truth.", err),
			)
			state.CustomSkillYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Custom skill has changed, updating state")
			state.CustomSkillYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Custom skill is equivalent, ignoring server-managed fields")
		}
	} else {
		state.CustomSkillYaml = types.StringValue(apiResponseJSON)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *CustomSkillResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state customSkillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var plan customSkillModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	jsonBody, ok := customSkillJSON(plan.CustomSkillYaml.ValueString(), &resp.Diagnostics)
	if !ok {
		return
	}

	// The origin and id are immutable, so carry them from state.
	plan.Origin = state.Origin
	plan.ID = state.ID
	if err := r.client.UpdateCustomSkill(ctx, plan.address(), jsonBody); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update custom skill, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a custom skill resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CustomSkillResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customSkillModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteCustomSkill(ctx, state.address()); err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Custom skill %s is already gone", state.address()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete custom skill, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a custom skill resource")
}

// ImportState adopts a custom skill by origin or id. A skill created in the
// Dash0 UI has no origin and is imported by its id.
func (r *CustomSkillResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	apiResponseJSON, err := r.client.GetCustomSkill(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Custom Skill",
			fmt.Sprintf("Could not get custom skill with origin or id %s: %s", req.ID, err),
		)
		return
	}

	id, origin := client.CustomSkillIdentity(apiResponseJSON)
	if id == "" {
		resp.Diagnostics.AddError(
			"Error Importing Custom Skill",
			fmt.Sprintf("The API response for custom skill %s has no dash0.com/id label.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), stringOrNull(origin))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), stringOrNull(id))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("custom_skill_yaml"), apiResponseJSON)...)
}
