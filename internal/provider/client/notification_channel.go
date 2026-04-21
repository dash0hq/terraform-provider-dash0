package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateNotificationChannel(ctx context.Context, origin string, channelJSON string) error {
	def, err := unmarshalNotificationChannel(channelJSON)
	if err != nil {
		return fmt.Errorf("error parsing notification channel JSON: %w", err)
	}

	dash0.SetNotificationChannelOrigin(def, origin)

	tflog.Debug(ctx, fmt.Sprintf("Creating notification channel with origin: %s", origin))

	_, err = c.inner.CreateNotificationChannel(ctx, def)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Notification channel created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetNotificationChannel(ctx context.Context, origin string) (string, error) {
	def, err := c.inner.GetNotificationChannel(ctx, origin)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Notification channel retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateNotificationChannel(ctx context.Context, origin string, channelJSON string) error {
	def, err := unmarshalNotificationChannel(channelJSON)
	if err != nil {
		return fmt.Errorf("error parsing notification channel JSON: %w", err)
	}

	dash0.SetNotificationChannelOrigin(def, origin)

	_, err = c.inner.UpdateNotificationChannel(ctx, origin, def)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Notification channel updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteNotificationChannel(ctx context.Context, origin string) error {
	err := c.inner.DeleteNotificationChannel(ctx, origin)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Notification channel deleted with origin: %s", origin))
	return nil
}

// unmarshalNotificationChannel parses a JSON string into a NotificationChannelDefinition.
func unmarshalNotificationChannel(jsonStr string) (*dash0.NotificationChannelDefinition, error) {
	var def dash0.NotificationChannelDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}
