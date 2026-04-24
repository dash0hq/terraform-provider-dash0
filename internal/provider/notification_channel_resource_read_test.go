package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Custom mock client implementation for notification channel read tests
type testNotificationChannelClient struct {
	client.Client
	getResponse string
	getError    error
}

func (c *testNotificationChannelClient) GetNotificationChannel(_ context.Context, _ string) (string, error) {
	return c.getResponse, c.getError
}

func TestNotificationChannelResource_ReadWithDiffs(t *testing.T) {
	testOrigin := "test-notification-channel"

	// Original notification channel YAML in state (user's config)
	originalYaml := `
kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts
spec:
  type: webhook
  config:
    url: https://example.com/webhook/test
`

	tests := []struct {
		name              string
		apiResponseYaml   string
		expectYamlUpdated bool
		expectWarning     bool
	}{
		{
			name: "metadata changes only - no significant diff",
			apiResponseYaml: `
kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts
  labels:
    dash0.com/origin: test-notification-channel
    dash0.com/id: "some-uuid"
  annotations:
    dash0.com/created-at: "2026-04-14T10:00:00Z"
    dash0.com/updated-at: "2026-04-14T10:00:00Z"
spec:
  type: webhook
  config:
    url: https://example.com/webhook/test
`,
			expectYamlUpdated: false,
			expectWarning:     false,
		},
		{
			name: "significant content change - should update state",
			apiResponseYaml: `
kind: Dash0NotificationChannel
metadata:
  labels:
    dash0.com/origin: test-notification-channel
spec:
  type: webhook
  config:
    url: https://example.com/webhook/different
`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
		{
			name:              "invalid YAML response - should update and warn",
			apiResponseYaml:   `not valid yaml {`,
			expectYamlUpdated: true,
			expectWarning:     true,
		},
		{
			name: "metadata.name change - should update state",
			apiResponseYaml: `
kind: Dash0NotificationChannel
metadata:
  name: Renamed Webhook Alerts
spec:
  type: webhook
  config:
    url: https://example.com/webhook/test
`,
			expectYamlUpdated: true,
			expectWarning:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testSchema := schema.Schema{
				Attributes: map[string]schema.Attribute{
					"origin": schema.StringAttribute{
						Computed: true,
					},
					"notification_channel_yaml": schema.StringAttribute{
						Required: true,
					},
				},
			}

			testClient := &testNotificationChannelClient{
				getResponse: tc.apiResponseYaml,
			}

			r := &NotificationChannelResource{client: testClient}

			raw := tftypes.NewValue(
				tftypes.Object{
					AttributeTypes: map[string]tftypes.Type{
						"origin":                    tftypes.String,
						"notification_channel_yaml": tftypes.String,
					},
				},
				map[string]tftypes.Value{
					"origin":                    tftypes.NewValue(tftypes.String, testOrigin),
					"notification_channel_yaml": tftypes.NewValue(tftypes.String, originalYaml),
				},
			)

			state := tfsdk.State{
				Raw:    raw,
				Schema: testSchema,
			}

			req := resource.ReadRequest{
				State: state,
			}

			resp := resource.ReadResponse{
				State: state,
			}

			ctx := context.Background()
			r.Read(ctx, req, &resp)

			var resultState notificationChannelModel
			resp.State.Get(ctx, &resultState)

			if tc.expectYamlUpdated {
				assert.Equal(t, tc.apiResponseYaml, resultState.NotificationChannelYaml.ValueString())
			} else {
				assert.Equal(t, originalYaml, resultState.NotificationChannelYaml.ValueString())
			}

			hasWarnings := resp.Diagnostics.WarningsCount() > 0
			assert.Equal(t, tc.expectWarning, hasWarnings)
		})
	}
}
