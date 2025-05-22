package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (c *dash0Client) CreateCheckRule(ctx context.Context, checkRule checkRuleResourceModel) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/check-rules/%s", checkRule.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", checkRule.Dataset.ValueString())
	u.RawQuery = q.Encode()

	// Convert YAML to JSON
	jsonBody, err := ConvertYAMLToJSON(checkRule.CheckRuleYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting check rule YAML to JSON: %w", err)
	}
	
	tflog.Debug(ctx, fmt.Sprintf("Creating check rule with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule created. Got API response: %s", resp))

	return nil
}

func (c *dash0Client) GetCheckRule(ctx context.Context, dataset string, origin string) (*checkRuleResourceModel, error) {
	apiPath := fmt.Sprintf("/api/check-rules/%s", origin)
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

	checkRule := &checkRuleResourceModel{
		Origin:        types.StringValue(origin),
		Dataset:       types.StringValue(dataset),
		CheckRuleYaml: types.StringValue(string(resp)),
	}
	return checkRule, nil
}

func (c *dash0Client) UpdateCheckRule(ctx context.Context, checkRule checkRuleResourceModel) error {
	dataset := checkRule.Dataset.ValueString()

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/check-rules/%s", checkRule.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating check rule in dataset: %s", dataset))

	// Convert YAML to JSON
	jsonBody, err := ConvertYAMLToJSON(checkRule.CheckRuleYaml.ValueString())
	if err != nil {
		return fmt.Errorf("error converting check rule YAML to JSON: %w", err)
	}
	
	tflog.Debug(ctx, fmt.Sprintf("Updating check rule with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	_, err = c.doRequest(ctx, http.MethodPut, u.String(), jsonBody)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule updated with origin: %s", checkRule.Origin))

	return nil
}

func (c *dash0Client) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/check-rules/%s", origin)
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Deleting check rule in dataset: %s", dataset))

	// Make the API request
	_, err = c.doRequest(ctx, http.MethodDelete, u.String(), "")
	if err != nil {
		return err
	}

	return nil
}