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

func (c *dash0Client) CreateView(ctx context.Context, check model.ViewResourceModel) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/views/%s", check.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", check.Dataset.ValueString())
	u.RawQuery = q.Encode()

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.ViewYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting view YAML to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating view with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("view created. Got API response: %s", resp))

	return nil
}
func (c *dash0Client) GetView(ctx context.Context, dataset string, origin string) (*model.ViewResourceModel, error) {
	apiPath := fmt.Sprintf("/api/views/%s", origin)
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

	check := &model.ViewResourceModel{
		Origin:   types.StringValue(origin),
		Dataset:  types.StringValue(dataset),
		ViewYaml: types.StringValue(string(resp)),
	}
	return check, nil
}
func (c *dash0Client) UpdateView(ctx context.Context, check model.ViewResourceModel) error {
	dataset := check.Dataset.ValueString()

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/views/%s", check.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating view in dataset: %s", dataset))

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.ViewYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting view YAML to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating view with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	_, err = c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("view updated with origin: %s", check.Origin))

	return nil
}
func (c *dash0Client) DeleteView(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/views/%s", origin)
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Deleting view in dataset: %s", dataset))

	// Make the API request
	_, err = c.doRequest(ctx, http.MethodDelete, u.String(), "")
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("view deleted with origin: %s", origin))

	return nil
}
