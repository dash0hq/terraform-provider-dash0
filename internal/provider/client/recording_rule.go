package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	rule, err := unmarshalRecordingRule(ruleJSON)
	if err != nil {
		return fmt.Errorf("error parsing recording rule JSON: %w", err)
	}

	setRecordingRuleOrigin(rule, origin)
	dash0.SetRecordingRuleDataset(rule, dataset)

	tflog.Debug(ctx, fmt.Sprintf("Creating recording rule with origin: %s", origin))

	_, err = c.inner.UpdateRecordingRule(ctx, origin, rule, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetRecordingRule(ctx context.Context, origin string, dataset string) (string, error) {
	rule, err := c.inner.GetRecordingRule(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule retrieved with origin: %s", origin))

	dash0.StripRecordingRuleServerFields(rule)
	return marshalToJSON(rule)
}

func (c *dash0Client) UpdateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	rule, err := unmarshalRecordingRule(ruleJSON)
	if err != nil {
		return fmt.Errorf("error parsing recording rule JSON: %w", err)
	}

	setRecordingRuleOrigin(rule, origin)
	dash0.SetRecordingRuleDataset(rule, dataset)

	_, err = c.inner.UpdateRecordingRule(ctx, origin, rule, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteRecordingRule(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteRecordingRule(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Recording rule deleted with origin: %s", origin))
	return nil
}

// ResolveRecordingRule looks up the server-assigned id of the recording rule
// with the given origin by matching against the list endpoint.
//
// Recording rules are not addressable in the Dash0 web app, so this function
// returns only an id (no deep-link URL). It returns an empty string (and no
// error) when the recording rule is not present in the list, so that callers
// can treat the id as best-effort metadata rather than failing the operation.
func (c *dash0Client) ResolveRecordingRule(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListRecordingRules(ctx, &dataset)
	if err != nil {
		return "", err
	}

	for _, rule := range items {
		if rule == nil || rule.Metadata.Labels == nil {
			continue
		}
		if (*rule.Metadata.Labels)[dash0.LabelOrigin] == origin {
			id := dash0.GetRecordingRuleID(rule)
			tflog.Debug(ctx, fmt.Sprintf("Resolved recording rule id for origin %s: %s", origin, id))
			return id, nil
		}
	}

	tflog.Warn(ctx, fmt.Sprintf("Recording rule with origin %q not found in dataset %q; id will be empty", origin, dataset))
	return "", nil
}

// unmarshalRecordingRule parses a JSON string into a RecordingRule.
func unmarshalRecordingRule(jsonStr string) (*dash0.RecordingRule, error) {
	var rule dash0.RecordingRule
	if err := json.Unmarshal([]byte(jsonStr), &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// setRecordingRuleOrigin sets the origin label on a recording rule.
func setRecordingRuleOrigin(rule *dash0.RecordingRule, origin string) {
	if rule.Metadata.Labels == nil {
		m := map[string]string{}
		rule.Metadata.Labels = &m
	}
	(*rule.Metadata.Labels)[dash0.LabelOrigin] = origin
}
