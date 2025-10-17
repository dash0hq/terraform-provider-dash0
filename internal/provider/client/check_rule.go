package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"gopkg.in/yaml.v3"
)

func (c *dash0Client) CreateCheckRule(ctx context.Context, checkRule model.CheckRule) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", checkRule.Origin.ValueString())

	dash0CheckRule, err := converter.ConvertPromYAMLToDash0CheckRule(checkRule.CheckRuleYaml.ValueString(), checkRule.Dataset.ValueString())
	if err != nil {
		return err
	}
	jsonBytes, err := json.Marshal(dash0CheckRule)
	if err != nil {
		return fmt.Errorf("error converting check rule to JSON: %w", err)
	}

	return c.create(ctx, checkRule.Dataset.ValueString(), apiPath, string(jsonBytes), "Check Rule")
}

func (c *dash0Client) GetCheckRule(ctx context.Context, dataset string, origin string) (*model.CheckRule, error) {
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", origin)
	resp, err := c.get(ctx, origin, dataset, apiPath, "Check Rule")
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

	checkRule := &model.CheckRule{
		Origin:        types.StringValue(origin),
		Dataset:       types.StringValue(dataset),
		CheckRuleYaml: types.StringValue(normalizedYAML),
	}
	return checkRule, nil
}

func (c *dash0Client) UpdateCheckRule(ctx context.Context, checkRule model.CheckRule) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", checkRule.Origin.ValueString())

	// Convert Prometheus YAML to Dash0 format
	dash0Checkrule, err := converter.ConvertPromYAMLToDash0CheckRule(checkRule.CheckRuleYaml.ValueString(), checkRule.Dataset.ValueString())
	if err != nil {
		return fmt.Errorf("error converting check rule YAML to Dash0 format: %w", err)
	}
	jsonBody, err := json.Marshal(dash0Checkrule)
	if err != nil {
		return fmt.Errorf("error converting check rule to JSON: %w", err)
	}

	return c.update(ctx, checkRule.Origin.ValueString(), checkRule.Dataset.ValueString(), apiPath, string(jsonBody), "Check Rule")
}

func (c *dash0Client) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	// Build URL with dataset query parameter
	apiPath := fmt.Sprintf("/api/alerting/check-rules/%s", origin)
	return c.delete(ctx, origin, dataset, apiPath, "Check Rule")
}
