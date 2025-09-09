package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *dash0Client) CreateDashboard(ctx context.Context, dashboard model.Dashboard) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/dashboards/%s", dashboard.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dashboard.Dataset.ValueString())
	u.RawQuery = q.Encode()

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(dashboard.DashboardYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting dashboard YAML to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating dashboard with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard created. Got API response: %s", resp))

	return nil
}

func (c *dash0Client) GetDashboard(ctx context.Context, dataset string, origin string) (*model.Dashboard, error) {
	apiPath := fmt.Sprintf("/api/dashboards/%s", origin)
	u, err := url.Parse(apiPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	resp, err := c.doRequest(ctx, http.MethodGet, u.String(), "")
	if err != nil {
		return nil, err
	}

	dashboard := &model.Dashboard{
		Origin:        types.StringValue(origin),
		Dataset:       types.StringValue(dataset),
		DashboardYaml: types.StringValue(string(resp)),
	}
	return dashboard, nil
}

func (c *dash0Client) UpdateDashboard(ctx context.Context, dashboard model.Dashboard) error {
	dataset := dashboard.Dataset.ValueString()

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/dashboards/%s", dashboard.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating dashboard in dataset: %s", dataset))

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(dashboard.DashboardYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting dashboard YAML to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating dashboard with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	_, err = c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard updated with origin: %s", dashboard.Origin))

	return nil
}

func (c *dash0Client) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/dashboards/%s", origin)
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Deleting dashboard in dataset: %s", dataset))

	// Make the API request
	_, err = c.doRequest(ctx, http.MethodDelete, u.String(), "")
	if err != nil {
		return err
	}

	return nil
}
