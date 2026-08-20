package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func TestBuildLogs_MapsAllFields(t *testing.T) {
	timestamp := time.Date(2026, 3, 15, 10, 30, 0, 123456789, time.UTC)
	observed := timestamp.Add(time.Second)

	logs, err := buildLogs(LogEvent{
		Body:              "Deployed checkout-api v2.1.0",
		EventName:         "dash0.deployment",
		SeverityNumber:    9,
		SeverityText:      "INFO",
		Timestamp:         timestamp,
		ObservedTimestamp: observed,
		ResourceAttributes: map[string]string{
			"service.name":            "checkout-api",
			"vcs.repository.url.full": "https://github.com/acme/checkout-api",
		},
		LogAttributes: map[string]string{
			"deployment.status": "succeeded",
		},
		TraceID: "0af7651916cd43dd8448eb211c80319c",
		SpanID:  "b7ad6b7169203331",
	}, "test-scope", "1.2.3")
	require.NoError(t, err)

	require.Equal(t, 1, logs.ResourceLogs().Len())
	rl := logs.ResourceLogs().At(0)

	serviceName, ok := rl.Resource().Attributes().Get("service.name")
	require.True(t, ok, "service.name should be a resource attribute")
	assert.Equal(t, "checkout-api", serviceName.Str())

	repoURL, ok := rl.Resource().Attributes().Get("vcs.repository.url.full")
	require.True(t, ok, "vcs.repository.url.full should be a resource attribute")
	assert.Equal(t, "https://github.com/acme/checkout-api", repoURL.Str())

	require.Equal(t, 1, rl.ScopeLogs().Len())
	sl := rl.ScopeLogs().At(0)
	assert.Equal(t, "test-scope", sl.Scope().Name())
	assert.Equal(t, "1.2.3", sl.Scope().Version())

	require.Equal(t, 1, sl.LogRecords().Len())
	lr := sl.LogRecords().At(0)
	assert.Equal(t, "Deployed checkout-api v2.1.0", lr.Body().Str())
	assert.Equal(t, "dash0.deployment", lr.EventName())
	assert.Equal(t, plog.SeverityNumber(9), lr.SeverityNumber())
	assert.Equal(t, "INFO", lr.SeverityText())
	assert.Equal(t, timestamp.UnixNano(), lr.Timestamp().AsTime().UnixNano())
	assert.Equal(t, observed.UnixNano(), lr.ObservedTimestamp().AsTime().UnixNano())

	status, ok := lr.Attributes().Get("deployment.status")
	require.True(t, ok, "deployment.status should be a log record attribute")
	assert.Equal(t, "succeeded", status.Str())

	// The event describes the entity via the resource, so entity attributes must
	// not leak onto the log record.
	_, found := lr.Attributes().Get("service.name")
	assert.False(t, found, "service.name must not be a log record attribute")

	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", lr.TraceID().String())
	assert.Equal(t, "b7ad6b7169203331", lr.SpanID().String())
}

func TestBuildLogs_OmitsUnsetFields(t *testing.T) {
	logs, err := buildLogs(LogEvent{Body: "plain message"}, "scope", "0.0.1")
	require.NoError(t, err)

	lr := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	assert.Equal(t, "plain message", lr.Body().Str())
	assert.Empty(t, lr.EventName())
	assert.Equal(t, plog.SeverityNumberUnspecified, lr.SeverityNumber())
	assert.Empty(t, lr.SeverityText())
	assert.True(t, lr.TraceID().IsEmpty())
	assert.True(t, lr.SpanID().IsEmpty())
	assert.Equal(t, 0, lr.Attributes().Len())
	assert.Equal(t, 0, logs.ResourceLogs().At(0).Resource().Attributes().Len())
}

func TestBuildLogs_DefaultsTimestampsToNow(t *testing.T) {
	before := time.Now()
	logs, err := buildLogs(LogEvent{Body: "message"}, "scope", "0.0.1")
	require.NoError(t, err)
	after := time.Now()

	lr := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	for name, actual := range map[string]time.Time{
		"timestamp":          lr.Timestamp().AsTime(),
		"observed timestamp": lr.ObservedTimestamp().AsTime(),
	} {
		assert.False(t, actual.Before(before.Add(-time.Second)), "%s should default to now", name)
		assert.False(t, actual.After(after.Add(time.Second)), "%s should default to now", name)
	}
}

func TestBuildLogs_RejectsHalfSpecifiedTraceContext(t *testing.T) {
	for name, event := range map[string]LogEvent{
		"trace id only": {Body: "b", TraceID: "0af7651916cd43dd8448eb211c80319c"},
		"span id only":  {Body: "b", SpanID: "b7ad6b7169203331"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := buildLogs(event, "scope", "0.0.1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be specified together")
		})
	}
}

func TestBuildLogs_RejectsMalformedTraceContext(t *testing.T) {
	tests := map[string]struct {
		traceID string
		spanID  string
		wantErr string
	}{
		"trace id too short": {
			traceID: "0af765", spanID: "b7ad6b7169203331",
			wantErr: "invalid trace_id",
		},
		"trace id not hex": {
			traceID: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", spanID: "b7ad6b7169203331",
			wantErr: "invalid trace_id",
		},
		"span id too short": {
			traceID: "0af7651916cd43dd8448eb211c80319c", spanID: "b7ad",
			wantErr: "invalid span_id",
		},
		"span id not hex": {
			traceID: "0af7651916cd43dd8448eb211c80319c", spanID: "zzzzzzzzzzzzzzzz",
			wantErr: "invalid span_id",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := buildLogs(LogEvent{Body: "b", TraceID: tt.traceID, SpanID: tt.spanID}, "scope", "0.0.1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSendLogEvent_WithoutOtlpURL(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), false, "test", 3, "")
	require.NoError(t, err)

	err = c.SendLogEvent(context.Background(), LogEvent{Body: "message"}, "default")
	require.Error(t, err)
	// The diagnostic has to name the knobs the user can turn, not just report
	// that OTLP is unconfigured.
	assert.Contains(t, err.Error(), "otlp_url")
	assert.Contains(t, err.Error(), "DASH0_OTLP_URL")
}

func TestSendLogEvent_RejectsOAuthToken(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("dash0_at_test-token"), true, "test", 3,
		"https://ingress.example.com")
	require.NoError(t, err)

	err = c.SendLogEvent(context.Background(), LogEvent{Body: "message"}, "default")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OAuth access token")
	assert.Contains(t, err.Error(), "auth_token")
}

func TestSendLogEvent_RejectsEmptyBody(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), false, "test", 3,
		"https://ingress.example.com")
	require.NoError(t, err)

	err = c.SendLogEvent(context.Background(), LogEvent{}, "default")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "body must not be empty")
}

func TestNewDash0Client_WithOtlpURL(t *testing.T) {
	c, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), false, "1.2.3", 3,
		"https://ingress.example.com")
	require.NoError(t, err)
	assert.Equal(t, "https://ingress.example.com", c.otlpURL)
	assert.Equal(t, "1.2.3", c.version)
}

func TestNewDash0Client_RejectsOtlpURLWithSignalPath(t *testing.T) {
	// The library appends /v1/logs itself; including it would produce a
	// double-suffixed URL, so it must be rejected at construction time.
	_, err := NewDash0Client("https://api.example.com", dash0.StaticAuthTokenProvider("auth_test-token"), false, "test", 3,
		"https://ingress.example.com/v1/logs")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "/v1/logs"), "error should name the offending suffix, got: %s", err)
}
