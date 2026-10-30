package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateSamplingRule creates a sampling rule under the given origin.
//
// Creation goes through PUT, not POST, which matches the provider's
// create-or-replace convention: PUT-to-origin is idempotent, so a retried
// create cannot produce a second rule.
//
// No dash0.com/origin label is stamped into the body; the server derives the
// origin from the URL path. The dataset label is different, see
// putSamplingRule.
func (c *dash0Client) CreateSamplingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	tflog.Debug(ctx, fmt.Sprintf("Creating sampling rule with origin: %s", origin))

	if err := c.putSamplingRule(ctx, origin, ruleJSON, dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Sampling rule created with origin: %s", origin))
	return nil
}

// GetSamplingRule retrieves the sampling rule with the given origin as a JSON
// document.
//
// Sampling rule deletes are hard: after a delete, this returns an error that
// dash0.IsNotFound recognizes.
func (c *dash0Client) GetSamplingRule(ctx context.Context, origin string, dataset string) (string, error) {
	rule, err := c.inner.GetSamplingRule(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Sampling rule retrieved with origin: %s", origin))
	return marshalToJSON(rule)
}

// UpdateSamplingRule replaces the sampling rule with the given origin. It is
// identical to CreateSamplingRule — PUT is create-or-replace.
func (c *dash0Client) UpdateSamplingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	if err := c.putSamplingRule(ctx, origin, ruleJSON, dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Sampling rule updated with origin: %s", origin))
	return nil
}

// putSamplingRule sends the rule with PUT, passing the dataset both as ?dataset=
// and as the dash0.com/dataset label (overriding the YAML) so they always match:
// the API will reject a mismatch, and older servers read only the label.
func (c *dash0Client) putSamplingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	rule, err := unmarshalSamplingRule(ruleJSON)
	if err != nil {
		return fmt.Errorf("error parsing sampling rule JSON: %w", err)
	}
	dash0.SetSamplingRuleDataset(rule, dataset)

	_, err = c.inner.UpdateSamplingRule(ctx, origin, rule, &dataset)
	return err
}

func (c *dash0Client) DeleteSamplingRule(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSamplingRule(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Sampling rule deleted with origin: %s", origin))
	return nil
}

// ResolveSamplingRule looks up the server-assigned id of the sampling rule
// with the given origin by matching against the list endpoint.
//
// Sampling rules are not addressable in the Dash0 web app, so this function
// returns only an id (no deep-link URL). It returns an empty string (and no
// error) when the rule is not present in the list, so that callers can treat
// the id as best-effort metadata. An error from the list call itself is
// propagated — the resource layer is what downgrades it to a warning.
func (c *dash0Client) ResolveSamplingRule(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListSamplingRules(ctx, &dataset)
	if err != nil {
		return "", err
	}

	// Match on origin first, fall back to matching on id, so a rule can also be
	// imported by the id the list endpoint reports.
	id := matchOriginID(items, origin, func(rule *dash0.SamplingDefinition) (string, *string) {
		if rule.Metadata.Labels == nil {
			return "", nil
		}
		return dash0.GetSamplingRuleID(rule), rule.Metadata.Labels.Dash0Comorigin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Sampling rule with origin %q not found in dataset %q; id will be empty", origin, dataset))
		return "", nil
	}

	tflog.Debug(ctx, fmt.Sprintf("Resolved sampling rule id for origin %s: %s", origin, id))
	return id, nil
}

// unmarshalSamplingRule parses a JSON string into a SamplingDefinition.
func unmarshalSamplingRule(jsonStr string) (*dash0.SamplingDefinition, error) {
	var rule dash0.SamplingDefinition
	if err := json.Unmarshal([]byte(jsonStr), &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}
