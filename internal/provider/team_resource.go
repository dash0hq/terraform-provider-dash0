package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	dash0 "github.com/dash0hq/dash0-api-client-go"
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

// The team resource does not need an explicit list of extra fields to strip
// during drift comparison. Server-managed CRD envelope fields fall into
// categories the normalizer already handles wholesale:
//
//   - `apiVersion` and `kind` are in the normalizer's default `ignoredFields`
//     (see internal/converter/normalizer.go).
//   - `metadata.labels` (all keys, including present-and-future dash0.com/*)
//     is also in that default list, so labels are stripped en masse on both
//     sides of the comparison.
//   - `metadata.annotations` (all keys, ditto) is stripped by
//     stripMetadataAnnotations when no annotation keys are preserved — which
//     is what we pass here.
//
// A hypothetical future server-managed label like `dash0.com/deleted-at`
// therefore does not cause drift; the entire labels map is discarded before
// the compare step. If the team resource ever needs to preserve a specific
// annotation (e.g. `dash0.com/sharing` as check_rule does), switch the plan
// modifier to `YAMLSemanticEqualWith([]string{...}, "dash0.com/sharing")`
// and add coverage in team_resource_read_test.go.

// warnIfCustomTeamMetadataSet emits a Warning when the user's YAML declares
// any metadata.labels or metadata.annotations outside the dash0.com/*
// namespace. The Dash0 API silently drops non-dash0.com/* entries on write,
// and StripTeamServerFields mirrors that behavior on the read side by
// clearing AdditionalProperties on labels and annotations — so any value the
// user provided round-trips to nothing with no server-side diagnostic. This
// hook surfaces the discard at plan time so users know their intent will not
// take effect.
func warnIfCustomTeamMetadataSet(teamYaml string, diags *diag.Diagnostics) {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(teamYaml), &parsed); err != nil {
		return
	}
	metadata, ok := parsed["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	report := func(section string, entries map[string]interface{}) {
		var custom []string
		for key := range entries {
			if !strings.HasPrefix(key, "dash0.com/") {
				custom = append(custom, key)
			}
		}
		if len(custom) == 0 {
			return
		}
		sort.Strings(custom)
		diags.AddWarning(
			fmt.Sprintf("metadata.%s outside the dash0.com/* namespace are dropped by the Dash0 API", section),
			fmt.Sprintf("The following metadata.%s entries will be silently discarded on write and never appear on read: %s. "+
				"Only metadata.labels and metadata.annotations under the dash0.com/* namespace are persisted.",
				section, strings.Join(custom, ", ")),
		)
	}
	if labels, ok := metadata["labels"].(map[string]interface{}); ok {
		report("labels", labels)
	}
	if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
		report("annotations", annotations)
	}
}

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &TeamResource{}
	_ resource.ResourceWithConfigure      = &TeamResource{}
	_ resource.ResourceWithImportState    = &TeamResource{}
	_ resource.ResourceWithValidateConfig = &TeamResource{}
)

// NewTeamResource is a helper function to simplify the provider implementation.
func NewTeamResource() resource.Resource {
	return &TeamResource{}
}

// TeamResource is the resource implementation.
type TeamResource struct {
	client client.Client
}

// teamModel is the Terraform state model for a team resource.
type teamModel struct {
	Origin   types.String `tfsdk:"origin"`
	ID       types.String `tfsdk:"id"`
	TeamYaml types.String `tfsdk:"team_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *TeamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TeamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team"
}

// ValidateConfig surfaces warnings about config that the Dash0 API will not
// honor. Currently this is limited to non-dash0.com/* labels and annotations
// in metadata, which are dropped by the API on write and stripped by the
// client on read — so any value the user provided round-trips to nothing.
func (r *TeamResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model teamModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.TeamYaml.IsNull() || model.TeamYaml.IsUnknown() {
		return
	}
	warnIfCustomTeamMetadataSet(model.TeamYaml.ValueString(), &resp.Diagnostics)
}

func (r *TeamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Team. Teams group organization members so alert notifications, dashboards, and other assets " +
			"can be attributed to a shared owner. Teams are organization-level resources and are not scoped to a dataset.\n\n" +
			"Membership in `spec.members` accepts either the member's email address or their internal Dash0 id (the " +
			"`dash0.com/id` label value returned by the Members API, e.g. `user_01ABC...`). Emails are matched " +
			"case-insensitively and translated to internal ids during reconciliation on the server. The provider normalizes " +
			"server responses back to email addresses for legibility, so writing emails and refreshing state produces no drift.\n\n" +
			"Only `metadata.labels` and `metadata.annotations` under the `dash0.com/*` namespace are persisted by the Dash0 API. " +
			"Any custom labels or annotations you set are silently dropped on write; the provider surfaces this as a plan-time " +
			"warning via `ValidateConfig` so the discard is visible before apply.",

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the team, automatically generated by the provider on creation. " +
					"Used to reference the team for updates, reads, deletes, and imports.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned UUID of the team, resolved by the provider after creation. Reference this " +
					"value from other resources that need the raw team id.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"team_yaml": schema.StringAttribute{
				Description: "The team definition in YAML format, following the `Dash0Team` CRD envelope: `apiVersion: " +
					"dash0.com/v1alpha1`, `kind: Dash0Team`, `metadata.name` for the technical name, and `spec.display` " +
					"plus `spec.members` for the human-facing attributes and membership. Setting `apiVersion` explicitly is " +
					"recommended so the configuration pins to the current schema and does not silently migrate if a future " +
					"schema version ships. Server-managed metadata fields (`dash0.com/id`, `dash0.com/source`, " +
					"`dash0.com/created-at`, `dash0.com/updated-at`) are stripped from the state on read; the provider stamps " +
					"`dash0.com/origin` from the `origin` attribute on write.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
}

// resolveTeamID populates the team's server-assigned id on the model by
// looking it up via the get endpoint. Failures are surfaced as warnings and
// leave the attribute null rather than failing the operation, mirroring the
// convention established by other resources.
func (r *TeamResource) resolveTeamID(ctx context.Context, model *teamModel, diags *diag.Diagnostics) {
	id, err := r.client.ResolveTeam(ctx, model.Origin.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve team id",
			fmt.Sprintf("The team was saved successfully, but its id could not be determined: %s", err),
		)
		model.ID = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
}

func (r *TeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model teamModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Generate a provider-owned origin with the tf_ prefix. The origin must
	// not contain slashes because the API client sends it verbatim as a URL
	// path segment; UUIDs with dashes satisfy that constraint.
	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format before conversion.
	var parsed interface{}
	err := yaml.Unmarshal([]byte(model.TeamYaml.ValueString()), &parsed)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Team definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API client.
	jsonBody, err := converter.ConvertYAMLToJSON(model.TeamYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert team YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateTeam(ctx, model.Origin.ValueString(), jsonBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create team, got error: %s", err))
		return
	}

	// Resolve the server-assigned id for the newly created team (best-effort).
	r.resolveTeamID(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a team resource")

	// Set state to fully populated data.
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *TeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state.
	var state teamModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetTeam(ctx, state.Origin.ValueString())
	if err != nil {
		// The team was removed out-of-band (CLI, UI, another workspace). The
		// Plugin Framework contract for "gone from the underlying system" is to
		// clear state so the next plan re-creates the resource; surfacing an
		// error would force the user to `terraform state rm` manually.
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Team %s no longer exists on the server; removing from state", state.Origin.ValueString()))
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read team, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a team resource")

	// Compare current state against the retrieved team so drift is detected
	// only on fields the user actually authored. The normalizer's default
	// ignoredFields already strips metadata.labels wholesale (any dash0.com/*
	// key, present or future), and passing a nil preservedAnnotationKeys
	// makes stripMetadataAnnotations discard all metadata.annotations — so no
	// team-specific list is needed here (see the block-comment above
	// teamAlwaysIgnoredFields for the rationale).
	if state.TeamYaml.ValueString() != "" {
		stateYAML := state.TeamYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, converter.ConditionallyIgnoredFields)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, nil)
		if err != nil {
			// Comparison failed — most commonly because the API response is
			// unparseable (edge case: server returned malformed YAML/JSON, or
			// the converter tripped on an unexpected shape). Preserving the
			// prior state.TeamYaml keeps a known-good value on disk so the
			// next successful refresh can reconcile normally, and surfacing
			// an error (rather than a warning) makes the failure visible
			// instead of quietly poisoning state with the offending payload.
			resp.Diagnostics.AddError(
				"Team Comparison Error",
				fmt.Sprintf("Failed to compare team against the API response: %s. "+
					"Leaving state.team_yaml unchanged; the next refresh will retry once the API returns a parseable response.", err),
			)
			return
		} else if !equivalent {
			tflog.Debug(ctx, "Team has changed, updating state")
			state.TeamYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Team is equivalent, ignoring changes in server-managed fields")
		}
	} else {
		state.TeamYaml = types.StringValue(apiResponseJSON)
	}

	// Self-heal state.id when it's null. resolveTeamID is best-effort at
	// Create/Import time (a warning + null id on failure) so a transient
	// members-endpoint or GetTeam failure at Create can leave the resource
	// with id=null forever — downstream references like dash0_team.foo.id
	// would then render as an empty string indefinitely. Re-resolving here
	// lets a subsequent refresh recover the id once the underlying issue
	// clears. When state.ID is already populated we skip: the id is
	// immutable server-side, so re-resolving is wasted work.
	if state.ID.IsNull() {
		r.resolveTeamID(ctx, &state, &resp.Diagnostics)
	}

	// Set refreshed state.
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *TeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state to preserve the origin (immutable across updates)
	// and the previously resolved id.
	var state teamModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan.
	var plan teamModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format.
	var parsed interface{}
	err := yaml.Unmarshal([]byte(plan.TeamYaml.ValueString()), &parsed)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Team definition is not valid YAML: %s", err),
		)
		return
	}

	jsonBody, err := converter.ConvertYAMLToJSON(plan.TeamYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert team YAML to JSON: %s", err))
		return
	}

	// Carry the origin and id from state — origin is immutable after create,
	// and the team's server-assigned id does not change on update.
	plan.Origin = state.Origin
	plan.ID = state.ID
	err = r.client.UpdateTeam(ctx, plan.Origin.ValueString(), jsonBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update team, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a team resource")

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *TeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteTeam(ctx, state.Origin.ValueString())
	if err != nil {
		// Idempotent destroy: a 404 means the team was already removed
		// out-of-band. The desired end-state ("team is gone") is achieved, so
		// let terraform destroy proceed rather than force `terraform state rm`.
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("Team %s was already gone at delete time; treating as success", state.Origin.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete team, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a team resource")
}

// ImportState allows importing an existing team by its origin (or the raw
// team id — the server-side endpoint accepts either).
func (r *TeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	origin := req.ID

	apiResponseJSON, err := r.client.GetTeam(ctx, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Team",
			fmt.Sprintf("Could not get team with origin=%s: %s", origin, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_yaml"), apiResponseJSON)...)

	// Resolve the id (best-effort).
	model := teamModel{Origin: types.StringValue(origin)}
	r.resolveTeamID(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
}
