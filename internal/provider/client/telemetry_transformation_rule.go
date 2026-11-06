package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateTelemetryTransformationRule(ctx context.Context, origin, ruleJSON, dataset string) (string, error) {
	rule, err := unmarshalTelemetryTransformationRule(ruleJSON)
	if err != nil {
		return "", err
	}
	dash0.StripTelemetryTransformationRuleServerFields(rule)
	dash0.ClearTelemetryTransformationRuleID(rule)
	dash0.SetTelemetryTransformationRuleDataset(rule, dataset)
	rule.Metadata.Labels.Dash0Comorigin = &origin

	created, err := c.inner.CreateTelemetryTransformationRule(ctx, rule, &dataset)
	if err != nil {
		return "", err
	}
	return dash0.GetTelemetryTransformationRuleID(created), nil
}

func (c *dash0Client) GetTelemetryTransformationRule(ctx context.Context, originOrID, dataset string) (string, error) {
	rule, err := c.inner.GetTelemetryTransformationRule(ctx, originOrID, &dataset)
	if err != nil {
		return "", err
	}
	return marshalToJSON(rule)
}

func (c *dash0Client) UpdateTelemetryTransformationRule(ctx context.Context, originOrID, ruleJSON, dataset string) error {
	rule, err := unmarshalTelemetryTransformationRule(ruleJSON)
	if err != nil {
		return err
	}
	dash0.StripTelemetryTransformationRuleServerFields(rule)
	dash0.ClearTelemetryTransformationRuleID(rule)
	dash0.SetTelemetryTransformationRuleDataset(rule, dataset)

	_, err = c.inner.UpdateTelemetryTransformationRule(ctx, originOrID, rule, &dataset)
	return err
}

func (c *dash0Client) DeleteTelemetryTransformationRule(ctx context.Context, originOrID, dataset string) error {
	return c.inner.DeleteTelemetryTransformationRule(ctx, originOrID, &dataset)
}

func unmarshalTelemetryTransformationRule(ruleJSON string) (*dash0.TelemetryTransformationRuleDefinition, error) {
	var rule dash0.TelemetryTransformationRuleDefinition
	decoder := json.NewDecoder(strings.NewReader(ruleJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rule); err != nil {
		return nil, fmt.Errorf("error parsing telemetry transformation rule JSON: %w", err)
	}
	return &rule, nil
}
