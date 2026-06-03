package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
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

// GetCheckRuleURL builds a deep link to the Dash0 web app for the check rule
// with the given origin. The internal id is resolved from the list endpoint by
// matching on origin (see matchOriginID).
//
// It returns an empty string (and no error) when the app base URL cannot be
// derived or the check rule is not present in the list, so that callers can
// treat the URL as best-effort metadata rather than failing the operation.
func (c *dash0Client) GetCheckRuleURL(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListCheckRules(ctx, &dataset)
	if err != nil {
		return "", err
	}

	id := matchOriginID(items, origin, func(item *dash0.PrometheusAlertRuleApiListItem) (string, *string) {
		return item.Id, item.Origin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Check rule with origin %q not found in dataset %q; check rule URL will be empty", origin, dataset))
		return "", nil
	}

	checkRuleURL := dash0.DeeplinkURL(c.apiURL, dash0.DeeplinkAssetTypeCheckRule, id, &dataset)
	logResolvedURL(ctx, "check rule", origin, checkRuleURL)
	return checkRuleURL, nil
}
