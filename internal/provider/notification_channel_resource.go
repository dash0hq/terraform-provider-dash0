package provider

import (
	"context"
	"fmt"

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

// notificationChannelConditionallyIgnoredFields are fields the API enriches on
// notification channel retrieval that should be ignored during comparison when
// the user did not include them in their config.
var notificationChannelConditionallyIgnoredFields = append(
	converter.ConditionallyIgnoredFields,
	"spec.frequency", // server default (e.g. "10m0s")
	"spec.routing",   // server default (empty assets/filters)
)

// notificationChannelAlwaysIgnoredFields are fields the API maintains on the
// channel even when the user includes other siblings in routing. The Dash0
// API discards spec.routing.assets on write and instead populates it as a
// back-reference whenever a check rule or synthetic check binds itself to the
// channel by id. Comparing the field during drift detection would therefore
// produce a perpetual diff whenever any check rule or synthetic check is
// bound to the channel.
var notificationChannelAlwaysIgnoredFields = []string{
	"spec.routing.assets",
}

// warnIfRoutingAssetsSet emits a Warning when the user's YAML declares a
// non-empty spec.routing.assets list. The Dash0 API discards this field on
// write, so the value will not take effect; binding a check rule or synthetic
// check to a notification channel must be expressed on the check resource.
func warnIfRoutingAssetsSet(channelYaml string, diags *diag.Diagnostics) {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(channelYaml), &parsed); err != nil {
		return
	}
	spec, ok := parsed["spec"].(map[string]interface{})
	if !ok {
		return
	}
	routing, ok := spec["routing"].(map[string]interface{})
	if !ok {
		return
	}
	assets, ok := routing["assets"].([]interface{})
	if !ok || len(assets) == 0 {
		return
	}
	diags.AddWarning(
		"spec.routing.assets is API-managed and ignored on write",
		"The Dash0 API populates spec.routing.assets as a back-reference when "+
			"other resources bind to this notification channel by id, and "+
			"discards any value supplied on write. The entries you provided "+
			"will not take effect. To bind a check rule, set the annotation "+
			"dash0.com/notification-channel-ids on the check rule; to bind a "+
			"synthetic check, set spec.notifications.channels on the "+
			"synthetic check.",
	)
}

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                   = &NotificationChannelResource{}
	_ resource.ResourceWithConfigure      = &NotificationChannelResource{}
	_ resource.ResourceWithImportState    = &NotificationChannelResource{}
	_ resource.ResourceWithValidateConfig = &NotificationChannelResource{}
)

// NewNotificationChannelResource is a helper function to simplify the provider implementation.
func NewNotificationChannelResource() resource.Resource {
	return &NotificationChannelResource{}
}

// NotificationChannelResource is the resource implementation.
type NotificationChannelResource struct {
	client client.Client
}

// notificationChannelModel is the Terraform state model for a notification channel resource.
type notificationChannelModel struct {
	Origin                  types.String `tfsdk:"origin"`
	ID                      types.String `tfsdk:"id"`
	NotificationChannelYaml types.String `tfsdk:"notification_channel_yaml"`
	URL                     types.String `tfsdk:"url"`
}

// Configure adds the provider configured client to the resource.
func (r *NotificationChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *NotificationChannelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_channel"
}

// ValidateConfig surfaces warnings about config that the Dash0 API will not
// honor. Currently this is limited to spec.routing.assets, which is discarded
// on write and reflects only server-maintained back-references on read.
func (r *NotificationChannelResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model notificationChannelModel
	diags := req.Config.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.NotificationChannelYaml.IsNull() || model.NotificationChannelYaml.IsUnknown() {
		return
	}
	warnIfRoutingAssetsSet(model.NotificationChannelYaml.ValueString(), &resp.Diagnostics)
}

func (r *NotificationChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 Notification Channel. Notification channels define how alerts are delivered to " +
			"external systems such as Slack, PagerDuty, email, and webhooks. Notification channels are " +
			"organization-level resources and are not scoped to a dataset.\n\n" +
			"See [Send Alert Check Notifications](https://www.dash0.com/docs/dash0/monitoring/alerting/send-alert-check-notifications) " +
			"and [Route Alert Check Notifications](https://www.dash0.com/docs/dash0/monitoring/alerting/route-alert-check-notifications) " +
			"for more details.\n\n" +
			"Supported channel types: `slack` (webhook), `slack_bot`, `email_v2`, `pagerduty`, `opsgenie`, " +
			"`webhook`, `teams_webhook`, `discord_webhook`, `google_chat_webhook`.",

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the notification channel, automatically generated on creation. Used to reference the notification channel for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": schema.StringAttribute{
				Description: "The server-assigned UUID of the notification channel, resolved by the provider after creation. Reference this value when wiring the channel into another resource's YAML — for example, in a `dash0_synthetic_check`'s `spec.notifications.channels` list, which requires raw UUIDs rather than origins.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"notification_channel_yaml": schema.StringAttribute{
				Description: "The notification channel definition in YAML format. " +
					"The YAML must include `kind: Dash0NotificationChannel`, a `metadata.name` field, " +
					"and a `spec` with `type` and type-specific `config`. " +
					"Optional fields include `frequency` (default `10m`) and `routing` for filtering which alerts are delivered. " +
					"Note that `spec.routing.assets` is populated by the Dash0 API as a back-reference when a check rule or " +
					"synthetic check binds to this channel by id, and is discarded if supplied on write; bind a check rule by " +
					"setting the `dash0.com/notification-channel-ids` annotation on the rule, or a synthetic check by setting " +
					"`spec.notifications.channels` on the synthetic check. " +
					"See [Send Alert Check Notifications](https://www.dash0.com/docs/dash0/monitoring/alerting/send-alert-check-notifications) for the available options.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualWith(notificationChannelAlwaysIgnoredFields),
				},
			},
			"url": schema.StringAttribute{
				Description: "The URL to open this notification channel in the Dash0 web app, derived from the Dash0 API URL and the channel's server-assigned identifier. Computed by the provider after creation. May be empty if the app URL cannot be derived (e.g. for self-hosted deployments with a custom web app domain).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// resolveNotificationChannel populates the channel's server-assigned id and
// web app URL on the model by looking them up via the list endpoint. Both are
// best-effort metadata: failures are surfaced as warnings and leave the
// attributes null rather than failing the operation.
func (r *NotificationChannelResource) resolveNotificationChannel(ctx context.Context, model *notificationChannelModel, diags *diag.Diagnostics) {
	id, channelURL, err := r.client.ResolveNotificationChannel(ctx, model.Origin.ValueString())
	if err != nil {
		diags.AddWarning(
			"Unable to resolve notification channel metadata",
			fmt.Sprintf("The notification channel was saved successfully, but its id and URL could not be determined: %s", err),
		)
		model.ID = types.StringNull()
		model.URL = types.StringNull()
		return
	}
	model.ID = stringOrNull(id)
	model.URL = stringOrNull(channelURL)
}

func (r *NotificationChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model notificationChannelModel
	diags := req.Plan.Get(ctx, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())

	// Validate YAML format
	var channelYaml interface{}
	err := yaml.Unmarshal([]byte(model.NotificationChannelYaml.ValueString()), &channelYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Notification channel definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(model.NotificationChannelYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert notification channel YAML to JSON: %s", err))
		return
	}

	err = r.client.CreateNotificationChannel(ctx, model.Origin.ValueString(), jsonBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create notification channel, got error: %s", err))
		return
	}

	// Resolve the id and web app URL for the newly created channel (best-effort).
	r.resolveNotificationChannel(ctx, &model, &resp.Diagnostics)

	tflog.Trace(ctx, "created a notification channel resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, model)
	resp.Diagnostics.Append(diags...)
}

func (r *NotificationChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state notificationChannelModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResponseJSON, err := r.client.GetNotificationChannel(ctx, state.Origin.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read notification channel, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "read a notification channel resource")

	// Compare the current state with the retrieved notification channel
	if state.NotificationChannelYaml.ValueString() != "" {
		stateYAML := state.NotificationChannelYaml.ValueString()
		additionalIgnored := converter.FieldsAbsentFromYAML(stateYAML, notificationChannelConditionallyIgnoredFields)
		additionalIgnored = append(additionalIgnored, notificationChannelAlwaysIgnoredFields...)
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored, nil)
		if err != nil {
			resp.Diagnostics.AddWarning(
				"Notification Channel Comparison Error",
				fmt.Sprintf("Error comparing notification channels: %s. Using API response as source of truth.", err),
			)
			state.NotificationChannelYaml = types.StringValue(apiResponseJSON)
		} else if !equivalent {
			tflog.Debug(ctx, "Notification channel has changed, updating state")
			state.NotificationChannelYaml = types.StringValue(apiResponseJSON)
		} else {
			tflog.Debug(ctx, "Notification channel is equivalent, ignoring changes in metadata fields")
		}
	} else {
		state.NotificationChannelYaml = types.StringValue(apiResponseJSON)
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *NotificationChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Get current state
	var state notificationChannelModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Retrieve values from plan
	var plan notificationChannelModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate YAML format
	var channelYaml interface{}
	err := yaml.Unmarshal([]byte(plan.NotificationChannelYaml.ValueString()), &channelYaml)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid YAML",
			fmt.Sprintf("Notification channel definition is not valid YAML: %s", err),
		)
		return
	}

	// Convert YAML to JSON for the API
	jsonBody, err := converter.ConvertYAMLToJSON(plan.NotificationChannelYaml.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Conversion Error", fmt.Sprintf("Unable to convert notification channel YAML to JSON: %s", err))
		return
	}

	// Update the existing notification channel
	plan.Origin = state.Origin
	// The channel's server-assigned identifier is immutable, so neither the id
	// nor the URL change on update; carry them from state instead of
	// re-resolving them via the API.
	plan.ID = state.ID
	plan.URL = state.URL
	err = r.client.UpdateNotificationChannel(ctx, plan.Origin.ValueString(), jsonBody)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update notification channel, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "updated a notification channel resource")

	// Set state to fully populated data
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *NotificationChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state notificationChannelModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteNotificationChannel(ctx, state.Origin.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete notification channel, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted a notification channel resource")
}

// ImportState function is required for resources that support import
func (r *NotificationChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	origin := req.ID

	apiResponseJSON, err := r.client.GetNotificationChannel(ctx, origin)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Importing Notification Channel",
			fmt.Sprintf("Could not get notification channel with origin=%s: %s", origin, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), origin)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("notification_channel_yaml"), apiResponseJSON)...)

	// Resolve the id and web app URL (best-effort).
	model := notificationChannelModel{Origin: types.StringValue(origin)}
	r.resolveNotificationChannel(ctx, &model, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("url"), model.URL)...)
}
