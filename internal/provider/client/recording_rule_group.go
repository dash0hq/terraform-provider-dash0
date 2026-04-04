package client

import (
	"context"
	"encoding/json"
	"fmt"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

func (c *dash0Client) CreateRecordingRuleGroup(ctx context.Context, origin string, groupYAML string, dataset string) error {
	group, err := unmarshalRecordingRuleGroup(groupYAML)
	if err != nil {
		return err
	}

	// Set dataset and origin labels
	ensureRecordingRuleGroupLabels(group)
	group.Metadata.Labels.Dash0Comdataset = &dataset
	group.Metadata.Labels.Dash0Comorigin = &origin

	tflog.Debug(ctx, fmt.Sprintf("Creating recording rule group with origin: %s", origin))

	_, err = c.inner.CreateRecordingRuleGroup(ctx, group)
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
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateRecordingRuleGroup(ctx context.Context, origin string, groupYAML string, dataset string) error {
	group, err := unmarshalRecordingRuleGroup(groupYAML)
	if err != nil {
		return err
	}

	// Set dataset and origin labels
	ensureRecordingRuleGroupLabels(group)
	group.Metadata.Labels.Dash0Comdataset = &dataset
	group.Metadata.Labels.Dash0Comorigin = &origin

	// Fetch current version for optimistic concurrency control
	current, err := c.inner.GetRecordingRuleGroup(ctx, origin, &dataset)
	if err != nil {
		return fmt.Errorf("error fetching current recording rule group for version: %w", err)
	}
	if current.Metadata.Labels != nil && current.Metadata.Labels.Dash0Comversion != nil {
		group.Metadata.Labels.Dash0Comversion = current.Metadata.Labels.Dash0Comversion
	}

	tflog.Debug(ctx, fmt.Sprintf("Updating recording rule group with origin: %s", origin))

	_, err = c.inner.UpdateRecordingRuleGroup(ctx, origin, group)
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

// unmarshalRecordingRuleGroup converts YAML to a RecordingRuleGroupDefinition.
func unmarshalRecordingRuleGroup(yamlStr string) (*dash0.RecordingRuleGroupDefinition, error) {
	jsonBody, err := converter.ConvertYAMLToJSON(yamlStr)
	if err != nil {
		return nil, fmt.Errorf("error converting recording rule group YAML to JSON: %w", err)
	}

	var group dash0.RecordingRuleGroupDefinition
	if err := json.Unmarshal([]byte(jsonBody), &group); err != nil {
		return nil, fmt.Errorf("error parsing recording rule group JSON: %w", err)
	}
	return &group, nil
}

// ensureRecordingRuleGroupLabels ensures the labels field is initialized.
func ensureRecordingRuleGroupLabels(group *dash0.RecordingRuleGroupDefinition) {
	if group.Metadata.Labels == nil {
		group.Metadata.Labels = &dash0.RecordingRuleGroupLabels{}
	}
}
