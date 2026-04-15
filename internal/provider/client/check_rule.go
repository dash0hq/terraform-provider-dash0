package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0yaml "github.com/dash0hq/dash0-api-client-go/yaml"
)

func (c *dash0Client) CreateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error {
	alertRule, err := dash0yaml.UnmarshalPrometheusRule([]byte(ruleYAML))
	if err != nil {
		return fmt.Errorf("error converting check rule YAML to Dash0 format: %w", err)
	}
	alertRule.Dataset = &dataset

	tflog.Debug(ctx, fmt.Sprintf("Creating check rule with origin: %s", origin))

	_, err = c.inner.UpdateCheckRule(ctx, origin, alertRule, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetCheckRule(ctx context.Context, origin string, dataset string) (string, error) {
	alertRule, err := c.inner.GetCheckRule(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule retrieved with origin: %s", origin))

	// Convert Dash0 API format back to Prometheus YAML
	promYAMLBytes, err := dash0yaml.MarshalPrometheusRule(alertRule)
	if err != nil {
		return "", fmt.Errorf("error converting check rule to Prometheus format: %w", err)
	}

	return string(promYAMLBytes), nil
}

func (c *dash0Client) UpdateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error {
	alertRule, err := dash0yaml.UnmarshalPrometheusRule([]byte(ruleYAML))
	if err != nil {
		return fmt.Errorf("error converting check rule YAML to Dash0 format: %w", err)
	}
	alertRule.Dataset = &dataset

	_, err = c.inner.UpdateCheckRule(ctx, origin, alertRule, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteCheckRule(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Check rule deleted with origin: %s", origin))
	return nil
}
