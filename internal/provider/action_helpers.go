package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// timestampLayout is the layout accepted for the `time` and `observed_time`
// action attributes. It matches the dash0 CLI's `--time` / `--observed-time`
// flags.
const timestampLayout = time.RFC3339Nano

// minSeverityNumber and maxSeverityNumber bound the OpenTelemetry severity
// number range (1–24). Zero is accepted at the schema level to mean "unset";
// the emitted record then carries no severity number.
const (
	minSeverityNumber = 1
	maxSeverityNumber = 24
)

// configureActionClient extracts the provider-configured Dash0 client from an
// action ConfigureRequest, mirroring the resource-level Configure methods.
//
// ProviderData is nil when Terraform calls Configure before the provider itself
// has been configured, which is normal and must not produce an error.
func configureActionClient(req action.ConfigureRequest, resp *action.ConfigureResponse) client.Client {
	if req.ProviderData == nil {
		return nil
	}

	c, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Action Configure Type",
			fmt.Sprintf("Expected client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return c
}

// stringMapFromAttribute converts an optional map attribute into a plain
// map[string]string. A null or unknown map yields nil.
func stringMapFromAttribute(ctx context.Context, attr types.Map, diags *diag.Diagnostics) map[string]string {
	if attr.IsNull() || attr.IsUnknown() {
		return nil
	}

	result := make(map[string]string, len(attr.Elements()))
	diags.Append(attr.ElementsAs(ctx, &result, false)...)
	if diags.HasError() {
		return nil
	}

	// An explicitly empty map and an absent one are equivalent for our purposes;
	// normalizing to nil keeps the emitted payload identical in both cases.
	if len(result) == 0 {
		return nil
	}
	return result
}

// parseTimestampAttribute parses an optional RFC3339 timestamp attribute,
// returning the zero time when the attribute is absent. The caller treats the
// zero time as "default to now".
func parseTimestampAttribute(attr types.String, path string, diags *diag.Diagnostics) time.Time {
	if attr.IsNull() || attr.IsUnknown() || attr.ValueString() == "" {
		return time.Time{}
	}

	parsed, err := time.Parse(timestampLayout, attr.ValueString())
	if err != nil {
		diags.AddError(
			fmt.Sprintf("Invalid %s", path),
			fmt.Sprintf(
				"The %s attribute must be an RFC3339 timestamp with optional nanoseconds "+
					"(e.g. \"2024-03-15T10:30:00.123456789Z\"), got %q: %s",
				path, attr.ValueString(), err,
			),
		)
		return time.Time{}
	}
	return parsed
}

// validateSeverityNumber reports an error when a set severity number falls
// outside the OpenTelemetry 1–24 range.
func validateSeverityNumber(attr types.Int64, diags *diag.Diagnostics) {
	if attr.IsNull() || attr.IsUnknown() {
		return
	}
	value := attr.ValueInt64()
	if value < minSeverityNumber || value > maxSeverityNumber {
		diags.AddError(
			"Invalid severity_number",
			fmt.Sprintf(
				"severity_number must be between %d and %d (see the OpenTelemetry specification), got: %d",
				minSeverityNumber, maxSeverityNumber, value,
			),
		)
	}
}

// validateTraceContext reports an error when only one half of the trace context
// is supplied. A span ID without its trace ID cannot be correlated, so the
// half-specified case is a configuration mistake rather than something to
// silently drop.
func validateTraceContext(traceID, spanID types.String, diags *diag.Diagnostics) {
	hasTraceID := !traceID.IsNull() && !traceID.IsUnknown() && traceID.ValueString() != ""
	hasSpanID := !spanID.IsNull() && !spanID.IsUnknown() && spanID.ValueString() != ""

	if hasTraceID != hasSpanID {
		diags.AddError(
			"Incomplete trace context",
			"trace_id and span_id must be specified together: a span ID without its trace ID "+
				"(or vice versa) cannot be correlated with a trace.",
		)
	}
}

// putIfSet adds key to attrs when value holds a non-empty string. It keeps the
// per-attribute mapping in the actions declarative rather than a wall of
// if-statements.
func putIfSet(attrs map[string]string, key string, value types.String) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	if v := value.ValueString(); v != "" {
		attrs[key] = v
	}
}

// mergeAttributes copies extra into base without overwriting keys base already
// holds. First-level attributes therefore win over the escape-hatch maps, so a
// typo in `resource_attributes` cannot silently shadow a dedicated attribute.
func mergeAttributes(base, extra map[string]string) map[string]string {
	for k, v := range extra {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return base
}

// invokeLogEvent sends event and records the outcome on resp.
//
// Failure handling is governed by failOnError. It defaults to false at the
// schema level because these actions emit telemetry: a marker that could not be
// delivered should not fail an apply, and — for an action wired to
// before_create — must not block the resource it annotates from being created.
// Users who want the marker to be load-bearing opt in explicitly.
func invokeLogEvent(
	ctx context.Context,
	c client.Client,
	resp *action.InvokeResponse,
	event client.LogEvent,
	dataset string,
	failOnError bool,
	progressMessage string,
) {
	if c == nil {
		resp.Diagnostics.AddError(
			"Dash0 Client Not Configured",
			"The provider was not configured before the action was invoked. Please report this issue to the provider developers.",
		)
		return
	}

	// SendProgress is supplied by the framework at invoke time; it is nil in
	// unit tests that construct the response directly.
	if resp.SendProgress != nil && progressMessage != "" {
		resp.SendProgress(action.InvokeProgressEvent{Message: progressMessage})
	}

	if err := c.SendLogEvent(ctx, event, dataset); err != nil {
		if failOnError {
			resp.Diagnostics.AddError(
				"Unable to Send Dash0 Log Event",
				fmt.Sprintf(
					"The log event could not be delivered to Dash0 and `fail_on_error` is true: %s",
					err,
				),
			)
			return
		}
		resp.Diagnostics.AddWarning(
			"Unable to Send Dash0 Log Event",
			fmt.Sprintf(
				"The log event could not be delivered to Dash0: %s\n\n"+
					"The apply was not failed because `fail_on_error` is false (the default). "+
					"Set `fail_on_error = true` to treat delivery failures as errors.",
				err,
			),
		)
		return
	}

	tflog.Info(ctx, "Sent Dash0 log event", map[string]any{
		"event_name": event.EventName,
		"dataset":    dataset,
	})
}

// boolValueOrDefault returns the configured boolean, or fallback when the
// attribute is absent. Action schemas have no Computed attributes and therefore
// no schema-level defaults, so optional booleans are defaulted in code.
func boolValueOrDefault(attr types.Bool, fallback bool) bool {
	if attr.IsNull() || attr.IsUnknown() {
		return fallback
	}
	return attr.ValueBool()
}

// stringValueOrDefault returns the configured string, or fallback when the
// attribute is absent or empty.
func stringValueOrDefault(attr types.String, fallback string) string {
	if attr.IsNull() || attr.IsUnknown() || attr.ValueString() == "" {
		return fallback
	}
	return attr.ValueString()
}

// intValueOrDefault returns the configured integer, or fallback when the
// attribute is absent.
func intValueOrDefault(attr types.Int64, fallback int) int {
	if attr.IsNull() || attr.IsUnknown() {
		return fallback
	}
	return int(attr.ValueInt64())
}
