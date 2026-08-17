package client

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// scopeName is the OpenTelemetry instrumentation scope name reported on
// telemetry emitted by this provider. It identifies the provider as the
// producer of the log record, the way the dash0 CLI reports itself as
// "dash0-cli" and its send-log-event action as
// "github.com/dash0hq/dash0-cli@send-log-event".
const scopeName = "github.com/dash0hq/terraform-provider-dash0"

// LogEvent describes a single OTLP log record to emit to Dash0.
//
// Zero values mean "not set" and are omitted from the emitted record, with two
// exceptions: Timestamp and ObservedTimestamp both default to the current time
// when left zero, matching the dash0 CLI's `logs send` behavior.
type LogEvent struct {
	// Body is the log record body. Required.
	Body string
	// EventName is the OTLP log record event name (for example
	// "dash0.deployment"). Optional.
	EventName string
	// SeverityNumber is the OpenTelemetry severity number (1–24). Optional;
	// zero means unset.
	SeverityNumber int
	// SeverityText is a free-form severity label (for example "INFO"). It is
	// independent of SeverityNumber. Optional.
	SeverityText string
	// Timestamp is the log record timestamp. Defaults to now when zero.
	Timestamp time.Time
	// ObservedTimestamp is the observed timestamp. Defaults to now when zero.
	ObservedTimestamp time.Time
	// ResourceAttributes are attached to the OTLP resource, describing the
	// entity the record is about (service, deployment, VCS repository, ...).
	ResourceAttributes map[string]string
	// LogAttributes are attached to the log record itself, describing the
	// individual event rather than the entity.
	LogAttributes map[string]string
	// TraceID is a 32-character hex trace ID. Must be set together with
	// SpanID. Optional.
	TraceID string
	// SpanID is a 16-character hex span ID. Must be set together with
	// TraceID. Optional.
	SpanID string
}

// SendLogEvent emits a single log record to the configured Dash0 OTLP/HTTP
// ingress endpoint. When dataset is non-empty it is sent as the Dash0-Dataset
// header; otherwise the organization's default dataset receives the record.
//
// It returns an actionable error when the provider was configured without an
// OTLP endpoint, since that is a configuration mistake rather than a transport
// failure and the underlying library's error does not name the provider
// attribute the user needs to set. It returns a similarly actionable error
// when the resolved credentials are an OAuth access token, since the Dash0
// OTLP/HTTP ingress endpoint does not accept those today — sending would only
// surface as an opaque 401 from the server.
func (c *dash0Client) SendLogEvent(ctx context.Context, event LogEvent, dataset string) error {
	if c.otlpURL == "" {
		return fmt.Errorf(
			"no Dash0 OTLP endpoint configured: set the `otlp_url` attribute in the provider " +
				"block, set the DASH0_OTLP_URL environment variable, or configure a dash0 CLI " +
				"profile with an OTLP URL")
	}
	if c.isOAuthToken {
		return fmt.Errorf(
			"the configured Dash0 credentials are an OAuth access token (`dash0_at_` prefix), " +
				"which the Dash0 OTLP/HTTP ingress endpoint does not accept, even though the Dash0 " +
				"API does; use a static auth token (`auth_` prefix, from " +
				"https://app.dash0.com/goto/settings/auth-tokens) via the `auth_token` provider " +
				"attribute, the DASH0_AUTH_TOKEN environment variable, or a dash0 CLI profile " +
				"without OAuth")
	}
	if event.Body == "" {
		return fmt.Errorf("log event body must not be empty")
	}

	logs, err := buildLogs(event, scopeName, c.version)
	if err != nil {
		return err
	}

	var datasetPtr *string
	if dataset != "" {
		datasetPtr = &dataset
	}

	tflog.Debug(ctx, "Sending log event to Dash0", map[string]any{
		"event_name": event.EventName,
		"dataset":    dataset,
		"otlp_url":   c.otlpURL,
	})

	if err := c.inner.SendLogs(ctx, logs, datasetPtr); err != nil {
		return fmt.Errorf("failed to send log event: %w", err)
	}
	return nil
}

// buildLogs converts a LogEvent into the single-record plog.Logs payload that
// the OTLP exporter expects.
func buildLogs(event LogEvent, scope, scopeVersion string) (plog.Logs, error) {
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()

	resourceAttrs := rl.Resource().Attributes()
	for k, v := range event.ResourceAttributes {
		resourceAttrs.PutStr(k, v)
	}

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName(scope)
	sl.Scope().SetVersion(scopeVersion)

	now := time.Now()

	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr(event.Body)

	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = now
	}
	observedTimestamp := event.ObservedTimestamp
	if observedTimestamp.IsZero() {
		observedTimestamp = now
	}
	lr.SetTimestamp(pcommon.NewTimestampFromTime(timestamp))
	lr.SetObservedTimestamp(pcommon.NewTimestampFromTime(observedTimestamp))

	if event.EventName != "" {
		lr.SetEventName(event.EventName)
	}
	if event.SeverityNumber != 0 {
		lr.SetSeverityNumber(plog.SeverityNumber(event.SeverityNumber))
	}
	if event.SeverityText != "" {
		lr.SetSeverityText(event.SeverityText)
	}

	logAttrs := lr.Attributes()
	for k, v := range event.LogAttributes {
		logAttrs.PutStr(k, v)
	}

	// Trace context is only meaningful as a pair: a span ID without its trace
	// ID cannot be correlated, so reject the half-specified case rather than
	// silently dropping one side.
	if (event.TraceID == "") != (event.SpanID == "") {
		return logs, fmt.Errorf("trace_id and span_id must be specified together")
	}
	if event.TraceID != "" {
		traceID, err := parseTraceID(event.TraceID)
		if err != nil {
			return logs, err
		}
		lr.SetTraceID(traceID)

		spanID, err := parseSpanID(event.SpanID)
		if err != nil {
			return logs, err
		}
		lr.SetSpanID(spanID)
	}

	return logs, nil
}

// parseTraceID decodes a 32-character hex string into a pcommon.TraceID.
func parseTraceID(s string) (pcommon.TraceID, error) {
	var traceID pcommon.TraceID
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return traceID, fmt.Errorf("invalid trace_id %q: must be 32 hexadecimal characters: %w", s, err)
	}
	if len(decoded) != len(traceID) {
		return traceID, fmt.Errorf("invalid trace_id %q: must be 32 hexadecimal characters, got %d", s, len(s))
	}
	copy(traceID[:], decoded)
	return traceID, nil
}

// parseSpanID decodes a 16-character hex string into a pcommon.SpanID.
func parseSpanID(s string) (pcommon.SpanID, error) {
	var spanID pcommon.SpanID
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return spanID, fmt.Errorf("invalid span_id %q: must be 16 hexadecimal characters: %w", s, err)
	}
	if len(decoded) != len(spanID) {
		return spanID, fmt.Errorf("invalid span_id %q: must be 16 hexadecimal characters, got %d", s, len(s))
	}
	copy(spanID[:], decoded)
	return spanID, nil
}
