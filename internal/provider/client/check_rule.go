package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/dash0/terraform-provider-dash0/internal/converter"
	"github.com/dash0/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"
)

func (c *dash0Client) CreateCheckRule(ctx context.Context, checkRule model.CheckRuleResourceModel) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", checkRule.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", checkRule.Dataset.ValueString())
	u.RawQuery = q.Encode()

	dash0CheckRule, err := converter.ConvertPromYAMLToDash0CheckRule(checkRule.CheckRuleYaml.ValueString(), checkRule.Dataset.ValueString())
	if err != nil {
		return err
	}
	jsonBytes, err := json.Marshal(dash0CheckRule)
	if err != nil {
		return fmt.Errorf("error converting check rule to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating check rule with JSON payload: %s", string(jsonBytes)))

	// Make the API request with JSON
	resp, err := c.doRequest(ctx, http.MethodPut, u.String(), string(jsonBytes))
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule created. Got API response: %s", resp))

	return nil
}

func (c *dash0Client) GetCheckRule(ctx context.Context, dataset string, origin string) (*model.CheckRuleResourceModel, error) {
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", origin)
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
	promRule, err := converter.ConvertDash0JSONtoPrometheusRules(string(resp))
	if err != nil {
		return nil, fmt.Errorf("error converting check rule to Prometheus format: %w", err)
	}
	promRuleYaml, err := yaml.Marshal(promRule)
	if err != nil {
		return nil, fmt.Errorf("error converting check rule to YAML: %w", err)
	}
	normalizedYAML, err := converter.NormalizeYAML(string(promRuleYaml))
	if err != nil {
		return nil, fmt.Errorf("error normalizing check rule YAML: %w", err)
	}

	checkRule := &model.CheckRuleResourceModel{
		Origin:        types.StringValue(origin),
		Dataset:       types.StringValue(dataset),
		CheckRuleYaml: types.StringValue(normalizedYAML),
	}
	return checkRule, nil
}

func (c *dash0Client) UpdateCheckRule(ctx context.Context, checkRule model.CheckRuleResourceModel) error {
	dataset := checkRule.Dataset.ValueString()

	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", checkRule.Origin.ValueString())
	u, err := url.Parse(apiPath)
	if err != nil {
		return fmt.Errorf("error parsing API path: %w", err)
	}

	// Add dataset as a query parameter
	q := u.Query()
	q.Set("dataset", dataset)
	u.RawQuery = q.Encode()

	tflog.Debug(ctx, fmt.Sprintf("Updating check rule in dataset: %s", dataset))

	// Convert Prometheus YAML to Dash0 format
	dash0Checkrule, err := converter.ConvertPromYAMLToDash0CheckRule(checkRule.CheckRuleYaml.ValueString(), dataset)
	if err != nil {
		return fmt.Errorf("error converting check rule YAML to Dash0 format: %w", err)
	}
	jsonBody, err := json.Marshal(dash0Checkrule)
	if err != nil {
		return fmt.Errorf("error converting check rule to JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating check rule with JSON payload: %s", jsonBody))

	// Make the API request with JSON
	_, err = c.doRequest(ctx, http.MethodPut, u.String(), string(jsonBody))
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule updated with origin: %s", checkRule.Origin))

	return nil
}

func (c *dash0Client) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", origin)
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
