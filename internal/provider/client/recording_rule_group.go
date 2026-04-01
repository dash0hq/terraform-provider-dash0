package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

func (c *dash0Client) CreateRecordingRuleGroup(ctx context.Context, origin string, groupYAML string, dataset string) error {
	jsonBody, err := converter.ConvertYAMLToJSON(groupYAML)
	if err != nil {
		return fmt.Errorf("error converting recording rule group YAML to JSON: %w", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal([]byte(jsonBody), &body); err != nil {
		return fmt.Errorf("error parsing recording rule group JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating recording rule group with origin: %s", origin))

	_, err = c.inner.CreateRecordingRuleGroup(ctx, &body, &dataset, origin)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule group created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetRecordingRuleGroup(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetRecordingRuleGroup(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule group retrieved with origin: %s", origin))

	jsonBytes, err := json.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("error marshaling recording rule group: %w", err)
	}
	return string(jsonBytes), nil
}

func (c *dash0Client) UpdateRecordingRuleGroup(ctx context.Context, origin string, groupYAML string, dataset string) error {
	jsonBody, err := converter.ConvertYAMLToJSON(groupYAML)
	if err != nil {
		return fmt.Errorf("error converting recording rule group YAML to JSON: %w", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal([]byte(jsonBody), &body); err != nil {
		return fmt.Errorf("error parsing recording rule group JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating recording rule group with origin: %s", origin))

	// The library handles version fetching and label injection
	_, err = c.inner.UpdateRecordingRuleGroup(ctx, origin, &body, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule group updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteRecordingRuleGroup(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteRecordingRuleGroup(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule group deleted with origin: %s", origin))
	return nil
}
