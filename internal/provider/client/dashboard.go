package client

import (
	"context"
	"fmt"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (c *dash0Client) CreateDashboard(ctx context.Context, dashboard model.Dashboard) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/dashboards/%s", dashboard.Origin.ValueString())

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(dashboard.DashboardYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting dashboard YAML to JSON: %w", err)
	}
	return c.create(ctx, dashboard.Dataset.ValueString(), apiPath, jsonBody, "Dashboard")
}

func (c *dash0Client) GetDashboard(ctx context.Context, dataset string, origin string) (*model.Dashboard, error) {
	apiPath := fmt.Sprintf("/api/dashboards/%s", origin)
	resp, err := c.get(ctx, origin, dataset, apiPath, "Dashboard")
	if err != nil {
		return nil, err
	}

	return &model.Dashboard{
		Origin:        types.StringValue(origin),
		Dataset:       types.StringValue(dataset),
		DashboardYaml: types.StringValue(string(resp)),
	}, nil
}

func (c *dash0Client) UpdateDashboard(ctx context.Context, dashboard model.Dashboard) error {
	apiPath := fmt.Sprintf("/api/dashboards/%s", dashboard.Origin.ValueString())

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(dashboard.DashboardYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting dashboard YAML to JSON: %w", err)
	}

	return c.update(ctx, dashboard.Origin.ValueString(), dashboard.Dataset.ValueString(), apiPath, jsonBody, "Dashboard")
}

func (c *dash0Client) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	apiPath := fmt.Sprintf("/api/dashboards/%s", origin)
	return c.delete(ctx, origin, dataset, apiPath, "Dashboard")
}
