package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNotificationChannelResourceModel(t *testing.T) {
	origin := "test-origin"
	notificationChannelYaml := `kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts
spec:
  type: webhook
  config:
    url: https://example.com/webhook/test`

	m := notificationChannelModel{
		Origin:                  types.StringValue(origin),
		NotificationChannelYaml: types.StringValue(notificationChannelYaml),
	}

	assert.Equal(t, origin, m.Origin.ValueString())
	assert.Equal(t, notificationChannelYaml, m.NotificationChannelYaml.ValueString())
}

func TestNewNotificationChannelResource(t *testing.T) {
	resource := NewNotificationChannelResource()
	assert.NotNil(t, resource)

	// Check that it's the correct type
	_, ok := resource.(*NotificationChannelResource)
	assert.True(t, ok)
}

func TestNotificationChannelResource_Metadata(t *testing.T) {
	r := &NotificationChannelResource{}
	resp := &resource.MetadataResponse{}
	req := resource.MetadataRequest{
		ProviderTypeName: "dash0",
	}

	r.Metadata(context.Background(), req, resp)

	assert.Equal(t, "dash0_notification_channel", resp.TypeName)
}

func TestNotificationChannelResource_Schema(t *testing.T) {
	r := &NotificationChannelResource{}
	resp := &resource.SchemaResponse{}
	req := resource.SchemaRequest{}

	r.Schema(context.Background(), req, resp)

	// Verify schema has the expected attributes
	assert.Contains(t, resp.Schema.Attributes, "origin")
	assert.Contains(t, resp.Schema.Attributes, "notification_channel_yaml")

	// Verify there is no dataset attribute
	assert.NotContains(t, resp.Schema.Attributes, "dataset")

	// Verify origin is computed
	originAttr := resp.Schema.Attributes["origin"]
	assert.True(t, originAttr.IsComputed())
	assert.False(t, originAttr.IsRequired())

	// Verify notification_channel_yaml is required
	yamlAttr := resp.Schema.Attributes["notification_channel_yaml"]
	assert.True(t, yamlAttr.IsRequired())
	assert.False(t, yamlAttr.IsComputed())
}

func TestNotificationChannelResource_Configure(t *testing.T) {
	tests := []struct {
		name         string
		providerData interface{}
		expectError  bool
		errorMessage string
	}{
		{
			name:         "valid client interface",
			providerData: &MockClient{},
			expectError:  false,
		},
		{
			name:         "nil provider data",
			providerData: nil,
			expectError:  false,
		},
		{
			name:         "invalid provider data type",
			providerData: "invalid",
			expectError:  true,
			errorMessage: "Unexpected Data Source Configure Type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &NotificationChannelResource{}
			resp := &resource.ConfigureResponse{}
			req := resource.ConfigureRequest{
				ProviderData: tc.providerData,
			}

			r.Configure(context.Background(), req, resp)

			if tc.expectError {
				assert.True(t, resp.Diagnostics.HasError())
				if tc.errorMessage != "" {
					assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tc.errorMessage)
				}
			} else {
				assert.False(t, resp.Diagnostics.HasError())
			}
		})
	}
}

func TestNotificationChannelResource_Create_InvalidYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &NotificationChannelResource{client: mockClient}

	// Create request with invalid YAML
	req := resource.CreateRequest{}
	resp := &resource.CreateResponse{}

	// Set up the request state with invalid YAML
	req.Plan = tfsdk.Plan{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":                    tftypes.String,
					"notification_channel_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":                    tftypes.NewValue(tftypes.String, "test-origin"),
				"notification_channel_yaml": tftypes.NewValue(tftypes.String, "invalid: yaml: content: ["),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin": schema.StringAttribute{
					Computed: true,
				},
				"notification_channel_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	r.Create(context.Background(), req, resp)

	// Should have error due to invalid YAML
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Invalid YAML")
}

func TestNotificationChannelResource_ReadError(t *testing.T) {
	mockClient := &MockClient{}
	r := &NotificationChannelResource{client: mockClient}

	// Mock client to return error - GetNotificationChannel(ctx, origin)
	mockClient.On("GetNotificationChannel", mock.Anything, "test-origin").Return(
		"", errors.New("not found"))

	req := resource.ReadRequest{}
	resp := &resource.ReadResponse{}

	// Create mock state
	req.State = tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":                    tftypes.String,
					"notification_channel_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":                    tftypes.NewValue(tftypes.String, "test-origin"),
				"notification_channel_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin": schema.StringAttribute{
					Computed: true,
				},
				"notification_channel_yaml": schema.StringAttribute{
					Required: true,
				},
			},
		},
	}

	r.Read(context.Background(), req, resp)

	// Should have error from client
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")

	mockClient.AssertExpectations(t)
}
