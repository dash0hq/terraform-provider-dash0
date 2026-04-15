package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *dash0Client) CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	def, err := unmarshalSyntheticCheck(checkJSON)
	if err != nil {
		return fmt.Errorf("error parsing synthetic check JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating synthetic check with origin: %s", origin))

	_, err = c.inner.UpdateSyntheticCheck(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetSyntheticCheck(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	def, err := unmarshalSyntheticCheck(checkJSON)
	if err != nil {
		return fmt.Errorf("error parsing synthetic check JSON: %w", err)
	}

	_, err = c.inner.UpdateSyntheticCheck(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSyntheticCheck(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check deleted with origin: %s", origin))
	return nil
}
