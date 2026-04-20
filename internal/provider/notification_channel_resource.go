package provider

import (
	"context"
	"fmt"

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

// notificationChannelConditionallyIgnoredFields are fields the API enriches on
// notification channel retrieval that should be ignored during comparison when
// the user did not include them in their config.
var notificationChannelConditionallyIgnoredFields = append(
	converter.ConditionallyIgnoredFields,
	"spec.frequency", // server default (e.g. "10m0s")
	"spec.routing",   // server default (empty assets/filters)
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &NotificationChannelResource{}
	_ resource.ResourceWithConfigure   = &NotificationChannelResource{}
	_ resource.ResourceWithImportState = &NotificationChannelResource{}
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
	NotificationChannelYaml types.String `tfsdk:"notification_channel_yaml"`
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

func (r *NotificationChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Manages a Dash0 Notification Channel. Notification channels define how alerts are delivered to external systems such as Slack, PagerDuty, email, and webhooks. Notification channels are organization-level resources and are not scoped to a dataset. See [Send Alert Check Notifications](https://www.dash0.com/docs/dash0/monitoring/alerting/send-alert-check-notifications) and [Route Alert Check Notifications](https://www.dash0.com/docs/dash0/monitoring/alerting/route-alert-check-notifications) for more details. YAML examples are available in the provider repository.`,

		Attributes: map[string]schema.Attribute{
			"origin": schema.StringAttribute{
				Description: "A unique identifier for the notification channel, automatically generated on creation. Used to reference the notification channel for updates, reads, deletes, and imports.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"notification_channel_yaml": schema.StringAttribute{
				Description: "The notification channel definition in YAML format, using the Dash0 CRD envelope structure with kind, metadata, and spec fields. See [Notification Channels](https://dash0.com/docs/dash0/monitoring/alerting/notification-channels) for the available options.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqual(),
				},
			},
		},
	}
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
		equivalent, err := converter.ResourceYAMLEquivalent(stateYAML, apiResponseJSON, additionalIgnored...)
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
}
