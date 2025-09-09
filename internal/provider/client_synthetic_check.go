package provider

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

func (c *dash0Client) CreateSyntheticCheck(ctx context.Context, check model.SyntheticCheckResourceModel) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", check.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", check.Dataset.ValueString())
	u.RawQuery = q.Encode()

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.SyntheticCheckYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting synthetic check YAML to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating synthetic check with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check created. Got API response: %s", resp))

	return nil
}

func (c *dash0Client) GetSyntheticCheck(ctx context.Context, dataset string, origin string) (*model.SyntheticCheckResourceModel, error) {
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", origin)
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

	check := &model.SyntheticCheckResourceModel{
		Origin:             types.StringValue(origin),
		Dataset:            types.StringValue(dataset),
		SyntheticCheckYaml: types.StringValue(string(resp)),
	}
	return check, nil
}

func (c *dash0Client) UpdateSyntheticCheck(ctx context.Context, check model.SyntheticCheckResourceModel) error {
	dataset := check.Dataset.ValueString()

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", check.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating synthetic check in dataset: %s", dataset))

	// Convert YAML to JSON
	jsonBody, err := converter.ConvertYAMLToJSON(check.SyntheticCheckYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting synthetic check YAML to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating synthetic check with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	_, err = c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check updated with origin: %s", check.Origin))

	return nil
}

func (c *dash0Client) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/synthetic-checks/%s", origin)
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Deleting synthetic check in dataset: %s", dataset))

	// Make the API request
	_, err = c.doRequest(ctx, http.MethodDelete, u.String(), "")
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check deleted with origin: %s", origin))

	return nil
}
