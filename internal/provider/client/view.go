package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *dash0Client) CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	def, err := unmarshalView(viewJSON)
	if err != nil {
		return fmt.Errorf("error parsing view JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating view with origin: %s", origin))

	_, err = c.inner.UpdateView(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("View created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetView(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetView(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("View retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	def, err := unmarshalView(viewJSON)
	if err != nil {
		return fmt.Errorf("error parsing view JSON: %w", err)
	}

	_, err = c.inner.UpdateView(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("View updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteView(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteView(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("View deleted with origin: %s", origin))
	return nil
}
