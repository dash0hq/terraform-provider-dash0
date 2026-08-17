package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// invokeLogEventAction runs the action against a mock client and returns the
// event it tried to send along with the invoke diagnostics.
func invokeLogEventAction(
	t *testing.T,
	values map[string]tftypes.Value,
	sendErr error,
) (client.LogEvent, string, *action.InvokeResponse) {
	t.Helper()

	var captured client.LogEvent
	var capturedDataset string

	m := &MockClient{}
	m.On("SendLogEvent", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured = args.Get(1).(client.LogEvent)
			capturedDataset = args.String(2)
		}).
		Return(sendErr)

	a := &LogEventAction{client: m}
	cfg := actionTestConfig(t, actionSchemaOf(t, a), values)

	resp := &action.InvokeResponse{}
	a.Invoke(context.Background(), action.InvokeRequest{Config: cfg}, resp)

	return captured, capturedDataset, resp
}

func TestLogEventAction_Metadata(t *testing.T) {
	a := &LogEventAction{}
	resp := &action.MetadataResponse{}
	a.Metadata(context.Background(), action.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_log_event", resp.TypeName)
}

func TestLogEventAction_SendsAllFields(t *testing.T) {
	event, dataset, resp := invokeLogEventAction(t, map[string]tftypes.Value{
		"body":            tfString("Application started"),
		"event_name":      tfString("acme.startup"),
		"severity_number": tfNumber(9),
		"severity_text":   tfString("INFO"),
		"dataset":         tfString("staging"),
		"time":            tfString("2026-03-15T10:30:00Z"),
		"observed_time":   tfString("2026-03-15T10:30:01Z"),
		"trace_id":        tfString("0af7651916cd43dd8448eb211c80319c"),
		"span_id":         tfString("b7ad6b7169203331"),
		"resource_attributes": tfStringMap(map[string]string{
			"service.name": "checkout-api",
		}),
		"log_attributes": tfStringMap(map[string]string{
			"user.id": "12345",
		}),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.Equal(t, "staging", dataset)
	assert.Equal(t, "Application started", event.Body)
	assert.Equal(t, "acme.startup", event.EventName)
	assert.Equal(t, 9, event.SeverityNumber)
	assert.Equal(t, "INFO", event.SeverityText)
	assert.Equal(t, map[string]string{"service.name": "checkout-api"}, event.ResourceAttributes)
	assert.Equal(t, map[string]string{"user.id": "12345"}, event.LogAttributes)
	assert.Equal(t, "0af7651916cd43dd8448eb211c80319c", event.TraceID)
	assert.Equal(t, "b7ad6b7169203331", event.SpanID)
	assert.Equal(t, 2026, event.Timestamp.Year())
	assert.Equal(t, 1, event.ObservedTimestamp.Second())
}

func TestLogEventAction_MinimalConfig(t *testing.T) {
	// Unlike the deployment action this one is unopinionated: nothing is
	// defaulted, so an omitted event name really means "no event name".
	event, _, resp := invokeLogEventAction(t, map[string]tftypes.Value{
		"body":    tfString("plain message"),
		"dataset": tfString("default"),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.Equal(t, "plain message", event.Body)
	assert.Empty(t, event.EventName)
	assert.Zero(t, event.SeverityNumber)
	assert.Empty(t, event.SeverityText)
	assert.Nil(t, event.ResourceAttributes)
	assert.Nil(t, event.LogAttributes)
}

func TestLogEventAction_DeliveryFailureWarnsByDefault(t *testing.T) {
	_, _, resp := invokeLogEventAction(t, map[string]tftypes.Value{
		"body":    tfString("plain message"),
		"dataset": tfString("default"),
	}, errors.New("ingress unreachable"))

	assert.False(t, resp.Diagnostics.HasError(), "delivery failure must not error by default")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "ingress unreachable")
}

func TestLogEventAction_DeliveryFailureErrorsWhenOptedIn(t *testing.T) {
	_, _, resp := invokeLogEventAction(t, map[string]tftypes.Value{
		"body":          tfString("plain message"),
		"dataset":       tfString("default"),
		"fail_on_error": tfBool(true),
	}, errors.New("ingress unreachable"))

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "ingress unreachable")
}

func TestLogEventAction_ValidateConfig(t *testing.T) {
	tests := map[string]struct {
		values    map[string]tftypes.Value
		wantError bool
	}{
		"valid": {
			values: map[string]tftypes.Value{
				"body":    tfString("message"),
				"dataset": tfString("default"),
			},
		},
		"trace id without span id": {
			values: map[string]tftypes.Value{
				"body":     tfString("message"),
				"dataset":  tfString("default"),
				"trace_id": tfString("0af7651916cd43dd8448eb211c80319c"),
			},
			wantError: true,
		},
		"span id without trace id": {
			values: map[string]tftypes.Value{
				"body":    tfString("message"),
				"dataset": tfString("default"),
				"span_id": tfString("b7ad6b7169203331"),
			},
			wantError: true,
		},
		"severity out of range": {
			values: map[string]tftypes.Value{
				"body":            tfString("message"),
				"dataset":         tfString("default"),
				"severity_number": tfNumber(0),
			},
			wantError: true,
		},
		"malformed observed_time": {
			values: map[string]tftypes.Value{
				"body":          tfString("message"),
				"dataset":       tfString("default"),
				"observed_time": tfString("not-a-timestamp"),
			},
			wantError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			a := &LogEventAction{}
			cfg := actionTestConfig(t, actionSchemaOf(t, a), tt.values)

			resp := &action.ValidateConfigResponse{}
			a.ValidateConfig(context.Background(), action.ValidateConfigRequest{Config: cfg}, resp)

			assert.Equal(t, tt.wantError, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
		})
	}
}

func TestLogEventAction_InvokeRejectsHalfSpecifiedTraceContext(t *testing.T) {
	// Invoke re-validates rather than trusting that ValidateConfig ran, so a
	// half-specified trace context never reaches the client.
	m := &MockClient{}
	a := &LogEventAction{client: m}
	cfg := actionTestConfig(t, actionSchemaOf(t, a), map[string]tftypes.Value{
		"body":     tfString("message"),
		"dataset":  tfString("default"),
		"trace_id": tfString("0af7651916cd43dd8448eb211c80319c"),
	})

	resp := &action.InvokeResponse{}
	a.Invoke(context.Background(), action.InvokeRequest{Config: cfg}, resp)

	assert.True(t, resp.Diagnostics.HasError())
	m.AssertNotCalled(t, "SendLogEvent", mock.Anything, mock.Anything, mock.Anything)
}
