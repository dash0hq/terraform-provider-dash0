package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ action.Action                   = &LogEventAction{}
	_ action.ActionWithConfigure      = &LogEventAction{}
	_ action.ActionWithValidateConfig = &LogEventAction{}
)

// NewLogEventAction is a helper function to simplify provider implementation.
func NewLogEventAction() action.Action {
	return &LogEventAction{}
}

// LogEventAction sends a single OTLP log record to Dash0.
type LogEventAction struct {
	client client.Client
}

// logEventActionModel maps the action configuration.
type logEventActionModel struct {
	Body               types.String `tfsdk:"body"`
	EventName          types.String `tfsdk:"event_name"`
	SeverityNumber     types.Int64  `tfsdk:"severity_number"`
	SeverityText       types.String `tfsdk:"severity_text"`
	ResourceAttributes types.Map    `tfsdk:"resource_attributes"`
	LogAttributes      types.Map    `tfsdk:"log_attributes"`
	Time               types.String `tfsdk:"time"`
	ObservedTime       types.String `tfsdk:"observed_time"`
	TraceID            types.String `tfsdk:"trace_id"`
	SpanID             types.String `tfsdk:"span_id"`
	Dataset            types.String `tfsdk:"dataset"`
	FailOnError        types.Bool   `tfsdk:"fail_on_error"`
}

// Metadata returns the action type name.
func (a *LogEventAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_event"
}

// Configure adds the provider configured client to the action.
func (a *LogEventAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = configureActionClient(req, resp)
}

// Schema defines the schema for the action.
func (a *LogEventAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sends a single [log event](https://dash0.com/docs/dash0/telemetry/logging/understand-log-events) to Dash0 via OTLP.\nThis is the general-purpose counterpart to the `dash0_deployment_event` action and mirrors the [`dash0 logs send`](https://dash0.com/docs/dash0/miscellaneous/tooling/dash0-cli/commands#logs-send) CLI command: it emits an arbitrary log record with a free-form event name and attributes.\nRequires the `otlp_url` provider attribute (or the DASH0_OTLP_URL environment variable).\nRequires a static `auth_`-prefixed auth token: the Dash0 OTLP/HTTP ingress endpoint this action sends to does not accept OAuth access tokens, so credentials resolved from an OAuth-enabled dash0 CLI profile fail with an actionable error.\nActions require Terraform 1.14 or later.",
		Attributes: map[string]schema.Attribute{
			"body": schema.StringAttribute{
				Required:    true,
				Description: "The log record body, i.e. the human-readable message.",
			},
			"event_name": schema.StringAttribute{
				Optional:    true,
				Description: "The event name, which identifies the kind of event this record represents (for example `dash0.deployment`). See the [event registry](https://dash0.com/docs/dash0/opentelemetry/semconvs/events/overview) for the events Dash0 recognizes. Omit for a plain log record with no event name.",
			},
			"severity_number": schema.Int64Attribute{
				Optional:    true,
				Description: "The OpenTelemetry severity number (1–24). Dash0 derives the severity range from this value. It is independent of `severity_text`. Omit to send no severity number.",
			},
			"severity_text": schema.StringAttribute{
				Optional:    true,
				Description: "The severity text (for example \"INFO\", \"WARN\", \"ERROR\"). This is independent of `severity_number` and can carry custom severity labels the way logging libraries do.",
			},
			"resource_attributes": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Attributes describing the entity the record is about, attached to the OTLP resource (for example `service.name`, `deployment.environment.name`). Prefer [OpenTelemetry semantic convention](https://opentelemetry.io/docs/specs/semconv/) keys.",
			},
			"log_attributes": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Attributes describing this individual event rather than the entity, attached to the log record itself.",
			},
			"time": schema.StringAttribute{
				Optional:    true,
				Description: "The log record timestamp as an RFC3339 timestamp with optional nanoseconds (for example \"2024-03-15T10:30:00.123456789Z\"). Defaults to the time the action is invoked.",
			},
			"observed_time": schema.StringAttribute{
				Optional:    true,
				Description: "The observed timestamp as an RFC3339 timestamp with optional nanoseconds. Defaults to the time the action is invoked.",
			},
			"trace_id": schema.StringAttribute{
				Optional:    true,
				Description: "The trace ID to correlate this record with, as 32 hexadecimal characters. Must be set together with `span_id`.",
			},
			"span_id": schema.StringAttribute{
				Optional:    true,
				Description: "The span ID to correlate this record with, as 16 hexadecimal characters. Must be set together with `trace_id`.",
			},
			"dataset": schema.StringAttribute{
				Required:    true,
				Description: "The identifier of the [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) the event is sent to. Provide the dataset's identifier, which is immutable, not the 'name'.",
			},
			"fail_on_error": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether a delivery failure fails the Terraform run. Defaults to `false`: undelivered telemetry is reported as a warning so that a transient ingestion problem does not fail an apply, and so that an action wired to `before_create` cannot block the resource it annotates. Set to `true` when the event is load-bearing. Configuration mistakes (a missing `otlp_url`, an OAuth token, an empty `body`, or malformed `trace_id`/`span_id`) always fail regardless of this setting, since none of them survive a retry.",
			},
		},
	}
}

// ValidateConfig performs plan-time validation so that malformed timestamps,
// out-of-range severities, and half-specified trace context surface before the
// apply rather than mid-run.
func (a *LogEventAction) ValidateConfig(ctx context.Context, req action.ValidateConfigRequest, resp *action.ValidateConfigResponse) {
	var cfg logEventActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validateSeverityNumber(path.Root("severity_number"), cfg.SeverityNumber, &resp.Diagnostics)
	validateTraceContext(path.Root("trace_id"), path.Root("span_id"), cfg.TraceID, cfg.SpanID, &resp.Diagnostics)
	parseTimestampAttribute(cfg.Time, "time", &resp.Diagnostics)
	parseTimestampAttribute(cfg.ObservedTime, "observed_time", &resp.Diagnostics)
}

// Invoke sends the log event.
func (a *LogEventAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var cfg logEventActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// severity_number and trace_id/span_id pairing/format are already checked
	// by ValidateConfig, which the framework runs before Invoke; re-running
	// them here would only repeat work already done at plan time.
	timestamp := parseTimestampAttribute(cfg.Time, "time", &resp.Diagnostics)
	observedTimestamp := parseTimestampAttribute(cfg.ObservedTime, "observed_time", &resp.Diagnostics)
	resourceAttributes := stringMapFromAttribute(ctx, cfg.ResourceAttributes, &resp.Diagnostics)
	logAttributes := stringMapFromAttribute(ctx, cfg.LogAttributes, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	event := client.LogEvent{
		Body:               cfg.Body.ValueString(),
		EventName:          cfg.EventName.ValueString(),
		SeverityNumber:     int(cfg.SeverityNumber.ValueInt64()),
		SeverityText:       cfg.SeverityText.ValueString(),
		Timestamp:          timestamp,
		ObservedTimestamp:  observedTimestamp,
		ResourceAttributes: resourceAttributes,
		LogAttributes:      logAttributes,
		TraceID:            cfg.TraceID.ValueString(),
		SpanID:             cfg.SpanID.ValueString(),
	}

	progress := "Sending log event to Dash0"
	if event.EventName != "" {
		progress = fmt.Sprintf("Sending %s log event to Dash0", event.EventName)
	}

	invokeLogEvent(
		ctx,
		a.client,
		resp,
		event,
		cfg.Dataset.ValueString(),
		cfg.FailOnError.ValueBool(),
		progress,
	)
}
