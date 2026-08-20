package provider

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// traceIDHexLen and spanIDHexLen are the expected hex-encoded lengths of an
// OpenTelemetry trace ID (16 bytes) and span ID (8 bytes) respectively.
const (
	traceIDHexLen = 32
	spanIDHexLen  = 16
)

// timestampLayout is the layout accepted for the `time` and `observed_time`
// action attributes. It matches the dash0 CLI's `--time` / `--observed-time`
// flags.
const timestampLayout = time.RFC3339Nano

// minSeverityNumber and maxSeverityNumber bound the OpenTelemetry severity
// number range (1–24). Leaving the attribute entirely unset (null) means "no
// severity" and emits a record with no severity number; 0 itself is rejected
// by validateSeverityNumber like any other out-of-range value.
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

// validateSeverityNumber reports an attribute-scoped error when a set
// severity number falls outside the OpenTelemetry 1–24 range, so Terraform
// can underline the offending argument instead of pointing at the action as
// a whole.
func validateSeverityNumber(attrPath path.Path, attr types.Int64, diags *diag.Diagnostics) {
	if attr.IsNull() || attr.IsUnknown() {
		return
	}
	value := attr.ValueInt64()
	if value < minSeverityNumber || value > maxSeverityNumber {
		diags.AddAttributeError(
			attrPath,
			"Invalid severity_number",
			fmt.Sprintf(
				"severity_number must be between %d and %d (see the OpenTelemetry specification), got: %d",
				minSeverityNumber, maxSeverityNumber, value,
			),
		)
	}
}

// validateTraceContext reports attribute-scoped errors when trace_id/span_id
// are half-specified or not valid hexadecimal of the expected length. A span
// ID without its trace ID (or vice versa) cannot be correlated, and malformed
// hex would otherwise only surface as a warning at invoke time, once
// buildLogs tries to actually decode it.
func validateTraceContext(traceIDPath, spanIDPath path.Path, traceID, spanID types.String, diags *diag.Diagnostics) {
	hasTraceID := !traceID.IsNull() && !traceID.IsUnknown() && traceID.ValueString() != ""
	hasSpanID := !spanID.IsNull() && !spanID.IsUnknown() && spanID.ValueString() != ""

	if hasTraceID != hasSpanID {
		if hasTraceID {
			diags.AddAttributeError(spanIDPath, "Incomplete trace context", "span_id is required when trace_id is set.")
		} else {
			diags.AddAttributeError(traceIDPath, "Incomplete trace context", "trace_id is required when span_id is set.")
		}
		return
	}
	if hasTraceID {
		validateHexID(traceIDPath, "trace_id", traceID.ValueString(), traceIDHexLen, diags)
		validateHexID(spanIDPath, "span_id", spanID.ValueString(), spanIDHexLen, diags)
	}
}

// validateHexID reports an attribute-scoped error when value is not exactly
// hexLen hexadecimal characters.
func validateHexID(attrPath path.Path, name, value string, hexLen int, diags *diag.Diagnostics) {
	if len(value) != hexLen {
		diags.AddAttributeError(
			attrPath,
			fmt.Sprintf("Invalid %s", name),
			fmt.Sprintf("%s must be %d hexadecimal characters, got %d", name, hexLen, len(value)),
		)
		return
	}
	if _, err := hex.DecodeString(value); err != nil {
		diags.AddAttributeError(
			attrPath,
			fmt.Sprintf("Invalid %s", name),
			fmt.Sprintf("%s must be %d hexadecimal characters: %s", name, hexLen, err),
		)
	}
}

// putIfSet adds key to attrs when value holds a non-empty string. It keeps the
// per-attribute mapping in the actions declarative rather than a wall of
// if-statements. A null or unknown value already yields "" from ValueString(),
// so no separate IsNull/IsUnknown check is needed.
func putIfSet(attrs map[string]string, key string, value types.String) {
	if v := value.ValueString(); v != "" {
		attrs[key] = v
	}
}

// mergeAttributes adds extra into base in place, without overwriting keys base
// already holds. First-level attributes therefore win over the escape-hatch
// maps, so a typo in `resource_attributes` cannot silently shadow a dedicated
// attribute. It has no return value because it mutates base directly — maps
// are reference types, so a functional-looking `x = mergeAttributes(x, y)`
// signature would be misleading about what's actually happening.
func mergeAttributes(base, extra map[string]string) {
	for k, v := range extra {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
}

// invokeLogEvent sends event and records the outcome on resp.
//
// Failure handling is governed by failOnError, but only for genuine delivery
// failures. It defaults to false at the schema level because these actions
// emit telemetry: a marker that could not be delivered should not fail an
// apply, and — for an action wired to before_create — must not block the
// resource it annotates from being created. Users who want the marker to be
// load-bearing opt in explicitly.
//
// Configuration mistakes (client.ErrInvalidLogEventConfig: missing OTLP
// endpoint, an OAuth token, an empty body, malformed trace/span IDs) are
// reported as errors unconditionally, regardless of failOnError. None of them
// survive a retry, so letting fail_on_error's default silence them would let
// a broken pipeline stay broken indefinitely while every apply still exits 0.
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
		if errors.Is(err, client.ErrInvalidLogEventConfig) {
			resp.Diagnostics.AddError(
				"Invalid Dash0 Log Event Configuration",
				fmt.Sprintf(
					"The log event could not be sent because of a configuration problem, independent of `fail_on_error`: %s",
					err,
				),
			)
			return
		}
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
