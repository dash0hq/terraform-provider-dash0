package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *dash0Client) CreateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error {
	def, err := unmarshalDashboard(dashboardJSON)
	if err != nil {
		return fmt.Errorf("error parsing dashboard JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating dashboard with origin: %s", origin))

	// Use PUT (update) for upsert-by-origin behavior
	_, err = c.inner.UpdateDashboard(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetDashboard(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetDashboard(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error {
	def, err := unmarshalDashboard(dashboardJSON)
	if err != nil {
		return fmt.Errorf("error parsing dashboard JSON: %w", err)
	}

	_, err = c.inner.UpdateDashboard(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteDashboard(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard deleted with origin: %s", origin))
	return nil
}
