package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ action.Action                   = &DeploymentEventAction{}
	_ action.ActionWithConfigure      = &DeploymentEventAction{}
	_ action.ActionWithValidateConfig = &DeploymentEventAction{}
)

const (
	// deploymentEventName is the event name Dash0 recognizes as a deployment
	// marker. See https://dash0.com/docs/dash0/opentelemetry/semconvs/events/deployment.
	deploymentEventName = "dash0.deployment"

	// defaultDeploymentSeverityNumber is the OpenTelemetry severity number for
	// INFO, which is what a deployment marker is: an informational event, not a
	// problem. It matches the value the dash0 CLI's send-log-event action uses.
	defaultDeploymentSeverityNumber = 9
)

// NewDeploymentEventAction is a helper function to simplify provider implementation.
func NewDeploymentEventAction() action.Action {
	return &DeploymentEventAction{}
}

// DeploymentEventAction sends a dash0.deployment event to Dash0.
type DeploymentEventAction struct {
	client client.Client
}

// deploymentEventActionModel maps the action configuration.
type deploymentEventActionModel struct {
	ServiceName               types.String `tfsdk:"service_name"`
	ServiceNamespace          types.String `tfsdk:"service_namespace"`
	ServiceVersion            types.String `tfsdk:"service_version"`
	DeploymentEnvironmentName types.String `tfsdk:"deployment_environment_name"`
	DeploymentName            types.String `tfsdk:"deployment_name"`
	DeploymentID              types.String `tfsdk:"deployment_id"`
	DeploymentStatus          types.String `tfsdk:"deployment_status"`
	VcsRepositoryURL          types.String `tfsdk:"vcs_repository_url"`
	VcsRefHeadRevision        types.String `tfsdk:"vcs_ref_head_revision"`
	VcsRefHeadName            types.String `tfsdk:"vcs_ref_head_name"`
	Body                      types.String `tfsdk:"body"`
	EventName                 types.String `tfsdk:"event_name"`
	SeverityNumber            types.Int64  `tfsdk:"severity_number"`
	ResourceAttributes        types.Map    `tfsdk:"resource_attributes"`
	LogAttributes             types.Map    `tfsdk:"log_attributes"`
	Time                      types.String `tfsdk:"time"`
	Dataset                   types.String `tfsdk:"dataset"`
	FailOnError               types.Bool   `tfsdk:"fail_on_error"`
}

// Metadata returns the action type name.
func (a *DeploymentEventAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment_event"
}

// Configure adds the provider configured client to the action.
func (a *DeploymentEventAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = configureActionClient(req, resp)
}

// Schema defines the schema for the action.
func (a *DeploymentEventAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sends a [deployment event](https://dash0.com/docs/dash0/opentelemetry/semconvs/events/deployment) to Dash0, marking the point in time at which a service was deployed.\nDeployment events can be overlaid on charts as [dashboard annotations](https://dash0.com/docs/dash0/dashboards/add-annotations), which is what makes \"what changed just before this graph moved?\" answerable.\nAttach it to a resource with a `lifecycle` `action_trigger` block, or invoke it on its own with `terraform apply -invoke`.\nRequires the `otlp_url` provider attribute (or the DASH0_OTLP_URL environment variable).\nRequires a static `auth_`-prefixed auth token: the Dash0 OTLP/HTTP ingress endpoint this action sends to does not accept OAuth access tokens, so credentials resolved from an OAuth-enabled dash0 CLI profile fail with an actionable error.\nActions require Terraform 1.14 or later.",
		Attributes: map[string]schema.Attribute{
			"service_name": schema.StringAttribute{
				Required:    true,
				Description: "The name of the deployed service. Maps to the `service.name` resource attribute.",
			},
			"service_namespace": schema.StringAttribute{
				Optional:    true,
				Description: "The namespace of the deployed service. Maps to the `service.namespace` resource attribute.",
			},
			"service_version": schema.StringAttribute{
				Optional:    true,
				Description: "The version of the deployed service, for example an image tag or release number. Maps to the `service.version` resource attribute.",
			},
			"deployment_environment_name": schema.StringAttribute{
				Optional:    true,
				Description: "The environment deployed to, for example \"production\". Maps to the `deployment.environment.name` resource attribute.",
			},
			"deployment_name": schema.StringAttribute{
				Optional:    true,
				Description: "The name of the deployment. Maps to the `deployment.name` resource attribute.",
			},
			"deployment_id": schema.StringAttribute{
				Optional:    true,
				Description: "The identifier of the deployment, for example a CI run ID. Maps to the `deployment.id` resource attribute.",
			},
			"deployment_status": schema.StringAttribute{
				Optional:    true,
				Description: "The outcome of the deployment, for example \"succeeded\" or \"failed\". Maps to the `deployment.status` log attribute — unlike the other attributes here it describes the event rather than the deployed entity. Note that Terraform cannot report a failed deployment: `action_trigger` has no after-failure event, so a failed apply emits nothing. Use the dash0 CLI or its `send-log-event` GitHub Action for failure markers.",
			},
			"vcs_repository_url": schema.StringAttribute{
				Optional:    true,
				Description: "The URL of the repository the deployed revision came from. Maps to the `vcs.repository.url.full` resource attribute, an identifying attribute of the `vcs.repository` entity.",
			},
			"vcs_ref_head_revision": schema.StringAttribute{
				Optional:    true,
				Description: "The deployed revision, for example a commit SHA. Maps to the `vcs.ref.head.revision` resource attribute, an identifying attribute of the `vcs.ref` entity.",
			},
			"vcs_ref_head_name": schema.StringAttribute{
				Optional:    true,
				Description: "The name of the deployed ref, for example a branch or tag name. Maps to the `vcs.ref.head.name` resource attribute.",
			},
			"body": schema.StringAttribute{
				Optional:    true,
				Description: "The human-readable message for the event. Defaults to a message derived from `service_name`, and `service_version` when set.",
			},
			"event_name": schema.StringAttribute{
				Optional:    true,
				Description: "The event name. Defaults to `dash0.deployment`, which is the name Dash0 recognizes as a deployment marker. Override only to emit a related, differently named event.",
			},
			"severity_number": schema.Int64Attribute{
				Optional:    true,
				Description: "The OpenTelemetry severity number (1–24). Defaults to 9 (INFO), which is what a deployment marker is.",
			},
			"resource_attributes": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Additional resource attributes, merged with the ones derived from the dedicated attributes above. Dedicated attributes take precedence, so a key set here cannot silently shadow one of them.",
			},
			"log_attributes": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Additional log record attributes, merged with `deployment_status`. Dedicated attributes take precedence.",
			},
			"time": schema.StringAttribute{
				Optional:    true,
				Description: "The event timestamp as an RFC3339 timestamp with optional nanoseconds (for example \"2024-03-15T10:30:00.123456789Z\"). Defaults to the time the action is invoked, which is normally what you want for a deployment marker.",
			},
			"dataset": schema.StringAttribute{
				Required:    true,
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) the event is sent to. Provide the dataset's identifier, which is immutable, not the 'name'.",
			},
			"fail_on_error": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether a delivery failure fails the Terraform run. Defaults to `false`: an undelivered deployment marker is reported as a warning so that a transient ingestion problem does not fail a deployment, and so that an action wired to `before_create` cannot block the resource it annotates. Set to `true` when the marker is load-bearing.",
			},
		},
	}
}

// ValidateConfig performs plan-time validation so that malformed timestamps and
// out-of-range severities surface before the apply rather than mid-run.
func (a *DeploymentEventAction) ValidateConfig(ctx context.Context, req action.ValidateConfigRequest, resp *action.ValidateConfigResponse) {
	var cfg deploymentEventActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validateSeverityNumber(cfg.SeverityNumber, &resp.Diagnostics)
	parseTimestampAttribute(cfg.Time, "time", &resp.Diagnostics)
}

// Invoke sends the deployment event.
func (a *DeploymentEventAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg deploymentEventActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validateSeverityNumber(cfg.SeverityNumber, &resp.Diagnostics)
	timestamp := parseTimestampAttribute(cfg.Time, "time", &resp.Diagnostics)
	extraResourceAttributes := stringMapFromAttribute(ctx, cfg.ResourceAttributes, &resp.Diagnostics)
	extraLogAttributes := stringMapFromAttribute(ctx, cfg.LogAttributes, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resource attributes describe the entity being deployed. The vcs.* keys
	// belong here rather than on the log record because upstream OpenTelemetry
	// models vcs.repository and vcs.ref as entities, with
	// vcs.repository.url.full and vcs.ref.head.revision as their identifying
	// attributes — placed on the log record they could not identify anything.
	resourceAttributes := map[string]string{}
	putIfSet(resourceAttributes, "service.name", cfg.ServiceName)
	putIfSet(resourceAttributes, "service.namespace", cfg.ServiceNamespace)
	putIfSet(resourceAttributes, "service.version", cfg.ServiceVersion)
	putIfSet(resourceAttributes, "deployment.environment.name", cfg.DeploymentEnvironmentName)
	putIfSet(resourceAttributes, "deployment.name", cfg.DeploymentName)
	putIfSet(resourceAttributes, "deployment.id", cfg.DeploymentID)
	putIfSet(resourceAttributes, "vcs.repository.url.full", cfg.VcsRepositoryURL)
	putIfSet(resourceAttributes, "vcs.ref.head.revision", cfg.VcsRefHeadRevision)
	putIfSet(resourceAttributes, "vcs.ref.head.name", cfg.VcsRefHeadName)
	resourceAttributes = mergeAttributes(resourceAttributes, extraResourceAttributes)

	// deployment.status describes this event, not the deployed entity, so it is
	// a log record attribute.
	logAttributes := map[string]string{}
	putIfSet(logAttributes, "deployment.status", cfg.DeploymentStatus)
	logAttributes = mergeAttributes(logAttributes, extraLogAttributes)

	event := client.LogEvent{
		Body:               stringValueOrDefault(cfg.Body, defaultDeploymentBody(cfg)),
		EventName:          stringValueOrDefault(cfg.EventName, deploymentEventName),
		SeverityNumber:     intValueOrDefault(cfg.SeverityNumber, defaultDeploymentSeverityNumber),
		SeverityText:       "",
		Timestamp:          timestamp,
		ResourceAttributes: resourceAttributes,
		LogAttributes:      logAttributes,
	}

	invokeLogEvent(
		ctx,
		a.client,
		resp,
		event,
		cfg.Dataset.ValueString(),
		boolValueOrDefault(cfg.FailOnError, false),
		fmt.Sprintf("Sending deployment event for %s to Dash0", cfg.ServiceName.ValueString()),
	)
}

// defaultDeploymentBody derives a human-readable message when `body` is not
// set, so that the common case needs no boilerplate.
func defaultDeploymentBody(cfg deploymentEventActionModel) string {
	service := cfg.ServiceName.ValueString()
	if version := stringValueOrDefault(cfg.ServiceVersion, ""); version != "" {
		return fmt.Sprintf("Deployed %s %s", service, version)
	}
	return fmt.Sprintf("Deployed %s", service)
}
