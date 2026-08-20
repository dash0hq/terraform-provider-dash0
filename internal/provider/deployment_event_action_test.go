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

// invokeDeploymentEvent runs the action against a mock client and returns the
// event it tried to send along with the invoke diagnostics.
func invokeDeploymentEvent(
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

	a := &DeploymentEventAction{client: m}
	cfg := actionTestConfig(t, actionSchemaOf(t, a), values)

	resp := &action.InvokeResponse{}
	a.Invoke(context.Background(), action.InvokeRequest{Config: cfg}, resp)

	return captured, capturedDataset, resp
}

func TestDeploymentEventAction_Metadata(t *testing.T) {
	a := &DeploymentEventAction{}
	resp := &action.MetadataResponse{}
	a.Metadata(context.Background(), action.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_deployment_event", resp.TypeName)
}

func TestDeploymentEventAction_Defaults(t *testing.T) {
	event, dataset, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name":    tfString("checkout-api"),
		"service_version": tfString("v2.1.0"),
		"dataset":         tfString("production"),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.Equal(t, "production", dataset)

	// A deployment marker only registers as one if it carries the event name
	// Dash0 recognizes, so that default is load-bearing.
	assert.Equal(t, "dash0.deployment", event.EventName)
	assert.Equal(t, 9, event.SeverityNumber)
	assert.Equal(t, "Deployed checkout-api v2.1.0", event.Body)
	assert.True(t, event.Timestamp.IsZero(), "timestamp should be left to the client to default to now")
}

func TestDeploymentEventAction_DefaultBodyWithoutVersion(t *testing.T) {
	event, _, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name": tfString("checkout-api"),
		"dataset":      tfString("production"),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.Equal(t, "Deployed checkout-api", event.Body)
}

func TestDeploymentEventAction_AttributePlacement(t *testing.T) {
	event, _, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name":                tfString("checkout-api"),
		"service_namespace":           tfString("shop"),
		"service_version":             tfString("v2.1.0"),
		"deployment_environment_name": tfString("production"),
		"deployment_name":             tfString("checkout-rollout"),
		"deployment_id":               tfString("run-1234"),
		"deployment_status":           tfString("succeeded"),
		"vcs_repository_url":          tfString("https://github.com/acme/checkout-api"),
		"vcs_ref_head_revision":       tfString("abc123"),
		"vcs_ref_head_name":           tfString("main"),
		"dataset":                     tfString("production"),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)

	// Everything that describes the deployed entity belongs on the resource.
	// vcs.repository.url.full and vcs.ref.head.revision are identifying
	// attributes of the vcs.repository and vcs.ref entities upstream, so placing
	// them on the log record would stop the entity from being formed at all.
	assert.Equal(t, map[string]string{
		"service.name":                "checkout-api",
		"service.namespace":           "shop",
		"service.version":             "v2.1.0",
		"deployment.environment.name": "production",
		"deployment.name":             "checkout-rollout",
		"deployment.id":               "run-1234",
		"vcs.repository.url.full":     "https://github.com/acme/checkout-api",
		"vcs.ref.head.revision":       "abc123",
		"vcs.ref.head.name":           "main",
	}, event.ResourceAttributes)

	// deployment.status describes this event, not the entity.
	assert.Equal(t, map[string]string{"deployment.status": "succeeded"}, event.LogAttributes)
}

func TestDeploymentEventAction_ExtraAttributesMergeWithoutShadowing(t *testing.T) {
	event, _, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name":      tfString("checkout-api"),
		"deployment_status": tfString("succeeded"),
		"dataset":           tfString("production"),
		"resource_attributes": tfStringMap(map[string]string{
			"service.name":  "should-not-win",
			"custom.tenant": "acme",
		}),
		"log_attributes": tfStringMap(map[string]string{
			"deployment.status": "should-not-win",
			"custom.trigger":    "manual",
		}),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.Equal(t, "checkout-api", event.ResourceAttributes["service.name"])
	assert.Equal(t, "acme", event.ResourceAttributes["custom.tenant"])
	assert.Equal(t, "succeeded", event.LogAttributes["deployment.status"])
	assert.Equal(t, "manual", event.LogAttributes["custom.trigger"])
}

func TestDeploymentEventAction_Overrides(t *testing.T) {
	event, _, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name":    tfString("checkout-api"),
		"dataset":         tfString("production"),
		"body":            tfString("custom message"),
		"event_name":      tfString("acme.custom.deployment"),
		"severity_number": tfNumber(17),
		"time":            tfString("2026-03-15T10:30:00Z"),
	}, nil)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.Equal(t, "custom message", event.Body)
	assert.Equal(t, "acme.custom.deployment", event.EventName)
	assert.Equal(t, 17, event.SeverityNumber)
	assert.Equal(t, 2026, event.Timestamp.Year())
	assert.Equal(t, 30, event.Timestamp.Minute())
}

func TestDeploymentEventAction_DeliveryFailureWarnsByDefault(t *testing.T) {
	_, _, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name": tfString("checkout-api"),
		"dataset":      tfString("production"),
	}, errors.New("ingress unreachable"))

	// A marker that could not be delivered must not fail the apply: wired to
	// before_create it would otherwise block the resource it annotates.
	assert.False(t, resp.Diagnostics.HasError(), "delivery failure must not error by default")
	require.Equal(t, 1, resp.Diagnostics.WarningsCount())
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "ingress unreachable")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Detail(), "fail_on_error")
}

func TestDeploymentEventAction_DeliveryFailureErrorsWhenOptedIn(t *testing.T) {
	_, _, resp := invokeDeploymentEvent(t, map[string]tftypes.Value{
		"service_name":  tfString("checkout-api"),
		"dataset":       tfString("production"),
		"fail_on_error": tfBool(true),
	}, errors.New("ingress unreachable"))

	require.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "ingress unreachable")
}

func TestDeploymentEventAction_ValidateConfig(t *testing.T) {
	tests := map[string]struct {
		values    map[string]tftypes.Value
		wantError bool
	}{
		"valid": {
			values: map[string]tftypes.Value{
				"service_name": tfString("checkout-api"),
				"dataset":      tfString("production"),
			},
		},
		"severity out of range": {
			values: map[string]tftypes.Value{
				"service_name":    tfString("checkout-api"),
				"dataset":         tfString("production"),
				"severity_number": tfNumber(99),
			},
			wantError: true,
		},
		"malformed time": {
			values: map[string]tftypes.Value{
				"service_name": tfString("checkout-api"),
				"dataset":      tfString("production"),
				"time":         tfString("yesterday"),
			},
			wantError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			a := &DeploymentEventAction{}
			cfg := actionTestConfig(t, actionSchemaOf(t, a), tt.values)

			resp := &action.ValidateConfigResponse{}
			a.ValidateConfig(context.Background(), action.ValidateConfigRequest{Config: cfg}, resp)

			assert.Equal(t, tt.wantError, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
		})
	}
}

func TestDeploymentEventAction_ConfigureRejectsWrongProviderData(t *testing.T) {
	a := &DeploymentEventAction{}

	resp := &action.ConfigureResponse{}
	a.Configure(context.Background(), action.ConfigureRequest{ProviderData: "not a client"}, resp)
	assert.True(t, resp.Diagnostics.HasError())
}

func TestDeploymentEventAction_ConfigureToleratesNilProviderData(t *testing.T) {
	// Terraform calls Configure before the provider itself is configured; that
	// must not produce a diagnostic.
	a := &DeploymentEventAction{}

	resp := &action.ConfigureResponse{}
	a.Configure(context.Background(), action.ConfigureRequest{ProviderData: nil}, resp)
	assert.False(t, resp.Diagnostics.HasError())
	assert.Nil(t, a.client)
}

func TestDeploymentEventAction_UnconfiguredClientErrors(t *testing.T) {
	a := &DeploymentEventAction{}
	cfg := actionTestConfig(t, actionSchemaOf(t, a), map[string]tftypes.Value{
		"service_name": tfString("checkout-api"),
		"dataset":      tfString("production"),
	})

	resp := &action.InvokeResponse{}
	a.Invoke(context.Background(), action.InvokeRequest{Config: cfg}, resp)
	assert.True(t, resp.Diagnostics.HasError())
}
