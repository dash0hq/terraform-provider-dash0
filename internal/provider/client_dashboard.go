package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *dash0Client) CreateDashboard(ctx context.Context, dashboard dashboardResourceModel) (string, error) {
	// Build URL with dataset query parameter
	apiPath := "/api/dashboards"
	u, err := url.Parse(apiPath)
	if err != nil {
		return "", fmt.Errorf("error parsing API path: %w", err)
	}

	// Get dataset value, default to "default" if not specified
	dataset := "default"
	if !dashboard.Dataset.IsNull() && dashboard.Dataset.ValueString() != "" {
		dataset = dashboard.Dataset.ValueString()
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Creating dashboard in dataset: %s", dataset))

	// Make the API request
	resp, err := c.doRequest(ctx, http.MethodPost, u.String(), dashboard.DashboardDefinitionYaml.ValueString())
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard created. Got API response: %s", resp))

	// Extract the dashboard ID from response metadata
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("error unmarshaling response: %w", err)
	}

	// Get metadata.dash0Extensions["dash0.com/id"] from response
	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("metadata not found in response or has unexpected format")
	}

	dash0Extensions, ok := metadata["dash0Extensions"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("dash0Extensions not found in response metadata or has unexpected format")
	}

	id, ok := dash0Extensions["id"].(string)
	if !ok {
		return "", fmt.Errorf("id not found in dash0Extensions or has unexpected format")
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard created with ID: %s", id))

	return id, nil
}

func (c *dash0Client) GetDashboard(ctx context.Context, id string) (*dashboardResourceModel, error) {
	// Build URL with id
	apiPath := fmt.Sprintf("/api/dashboards/%s", id)
	u, err := url.Parse(apiPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing API path: %w", err)
	}

	// For GET requests, we don't need to specify dataset as we get it from the response
	resp, err := c.doRequest(ctx, http.MethodGet, u.String(), "")
	if err != nil {
		return nil, err
	}

	var result struct {
		Dataset string `json:"dataset"`
		Yaml    string `json:"yaml"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling response: %w", err)
	}

	// If dataset is empty in response, use the default value
	dataset := result.Dataset
	if dataset == "" {
		dataset = "default"
	}

	dashboard := &dashboardResourceModel{
		ID:                      types.StringValue(id),
		Dataset:                 types.StringValue(dataset),
		DashboardDefinitionYaml: types.StringValue(result.Yaml),
	}
	return dashboard, nil
}

func (c *dash0Client) UpdateDashboard(ctx context.Context, dashboard dashboardResourceModel) (string, error) {
	// Get dataset value, default to "default" if not specified
	dataset := "default"
	if !dashboard.Dataset.IsNull() && dashboard.Dataset.ValueString() != "" {
		dataset = dashboard.Dataset.ValueString()
	}

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/dashboards/%s", dashboard.ID.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return "", fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating dashboard in dataset: %s", dataset))

	// Make the API request
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), dashboard.DashboardDefinitionYaml.ValueString())
	if err != nil {
		return "", err
	}

	// Extract the dashboard ID from response metadata
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("error unmarshaling response: %w", err)
	}

	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("metadata not found in response or has unexpected format")
	}

	dash0Extensions, ok := metadata["dash0Extensions"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("dash0Extensions not found in response metadata or has unexpected format")
	}

	id, ok := dash0Extensions["id"].(string)
	if !ok {
		return "", fmt.Errorf("id not found in dash0Extensions or has unexpected format")
	}

	tflog.Debug(ctx, fmt.Sprintf("Dashboard updated with ID: %s", id))

	return id, nil
}

func (c *dash0Client) DeleteDashboard(ctx context.Context, id string) (string, error) {
	// For delete operations, we still need to specify the dataset
	// First, try to get the dashboard to determine the dataset
	dashboard, err := c.GetDashboard(ctx, id)
	if err != nil {
		return "", fmt.Errorf("error retrieving dashboard for deletion: %w", err)
	}

	// Get dataset from the dashboard object
	dataset := "default"
	if !dashboard.Dataset.IsNull() && dashboard.Dataset.ValueString() != "" {
		dataset = dashboard.Dataset.ValueString()
	}

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/dashboards/%s", id)
	u, err := url.Parse(apiPath)
	if err != nil {
		return "", fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Deleting dashboard in dataset: %s", dataset))

	// Make the API request
	_, err = c.doRequest(ctx, http.MethodDelete, u.String(), "")
	if err != nil {
		return "", err
	}

	return id, nil
}
