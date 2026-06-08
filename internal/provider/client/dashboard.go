package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
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

// ResolveDashboard looks up the server-assigned id and deep-link URL for the
// dashboard with the given origin by matching against the list endpoint.
//
// The web app addresses dashboards by their server-assigned internal id, which
// is NOT returned by the single-dashboard endpoint (that only echoes the
// origin). The id is therefore resolved from the list endpoint, mirroring how
// the dash0 CLI builds dashboard URLs.
//
// It returns empty strings (and no error) when the dashboard is not present in
// the list, so that callers can treat both fields as best-effort metadata
// rather than failing the operation. The URL is additionally empty when the
// app base URL cannot be derived from the API URL.
func (c *dash0Client) ResolveDashboard(ctx context.Context, origin string, dataset string) (string, string, error) {
	items, err := c.inner.ListDashboards(ctx, &dataset)
	if err != nil {
		return "", "", err
	}

	id := matchOriginID(items, origin, func(item *dash0.DashboardApiListItem) (string, *string) {
		return item.Id, item.Origin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Dashboard with origin %q not found in dataset %q; id and URL will be empty", origin, dataset))
		return "", "", nil
	}

	dashboardURL := dash0.DeeplinkURL(c.apiURL, dash0.DeeplinkAssetTypeDashboard, id, &dataset)
	logResolvedURL(ctx, "dashboard", origin, dashboardURL)
	return id, dashboardURL, nil
}
