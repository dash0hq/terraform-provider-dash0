package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	cfg, err := ParseAwsMetricScrapeConfig(cfgJSON)
	if err != nil {
		return err
	}
	dash0.SetAwsMetricScrapeConfigOrigin(cfg, origin)

	if _, err := c.inner.UpdateAwsMetricScrapeConfig(ctx, integrationID, cfg); err != nil {
		return err
	}
	tflog.Debug(ctx, fmt.Sprintf("AWS metric scrape config updated for integration: %s", integrationID))
	return nil
}

// ParseAwsMetricScrapeConfig parses cfgJSON and rejects a missing
// spec.namespaces or an unknown field in spec. Both would otherwise reach the
// API as an empty namespaces list, which stops all scraping.
func ParseAwsMetricScrapeConfig(cfgJSON string) (*dash0.AwsMetricScrapeConfigDefinition, error) {
	var cfg dash0.AwsMetricScrapeConfigDefinition
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return nil, fmt.Errorf("error parsing AWS metric scrape config JSON: %w", err)
	}
	if cfg.Spec.Namespaces == nil {
		return nil, errors.New("spec.namespaces is required; use an empty list to stop all scraping")
	}

	// metadata stays lenient: Dash0 manages it and the provider ignores it.
	var raw struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal([]byte(cfgJSON), &raw); err != nil {
		return nil, fmt.Errorf("error parsing AWS metric scrape config JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Spec))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dash0.AwsMetricScrapeConfigSpec{}); err != nil {
		return nil, fmt.Errorf("invalid AWS metric scrape config spec: %w", err)
	}
	return &cfg, nil
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
