package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// GetAwsMetricScrapeConfig returns the scrape config of the AWS integration
// as JSON, with all labels and annotations stripped, plus its
// dash0.com/origin label ("" when absent).
func (c *dash0Client) GetAwsMetricScrapeConfig(ctx context.Context, integrationID string) (string, string, error) {
	cfg, err := c.inner.GetAwsMetricScrapeConfig(ctx, integrationID)
	if err != nil {
		return "", "", err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS metric scrape config retrieved for integration: %s", integrationID))

	origin := dash0.GetAwsMetricScrapeConfigOrigin(cfg)
	dash0.StripAwsMetricScrapeConfigServerFields(cfg)
	cfg.Metadata.Labels = nil
	cfgJSON, err := marshalToJSON(cfg)
	if err != nil {
		return "", "", err
	}
	return cfgJSON, origin, nil
}

// UpdateAwsMetricScrapeConfig replaces the scrape config of the AWS
// integration with cfgJSON, stamping origin on dash0.com/origin.
func (c *dash0Client) UpdateAwsMetricScrapeConfig(ctx context.Context, integrationID string, origin string, cfgJSON string) error {
	var cfg dash0.AwsMetricScrapeConfigDefinition
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return fmt.Errorf("error parsing AWS metric scrape config JSON: %w", err)
	}
	dash0.SetAwsMetricScrapeConfigOrigin(&cfg, origin)

	if _, err := c.inner.UpdateAwsMetricScrapeConfig(ctx, integrationID, &cfg); err != nil {
		return err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS metric scrape config updated for integration: %s", integrationID))
	return nil
}

// DeleteAwsMetricScrapeConfig clears the scrape config of the AWS
// integration. The integration itself stays.
func (c *dash0Client) DeleteAwsMetricScrapeConfig(ctx context.Context, integrationID string) error {
	if err := c.inner.DeleteAwsMetricScrapeConfig(ctx, integrationID); err != nil {
		return err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS metric scrape config cleared for integration: %s", integrationID))
	return nil
}
