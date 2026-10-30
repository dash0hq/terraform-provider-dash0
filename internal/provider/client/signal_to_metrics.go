package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateSignalToMetrics creates a signal-to-metrics rule under the given
// origin.
//
// Creation goes through PUT, not POST, which matches the provider's
// create-or-replace convention: PUT-to-origin is idempotent, so a retried
// create cannot produce a second rule.
func (c *dash0Client) CreateSignalToMetrics(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	tflog.Debug(ctx, fmt.Sprintf("Creating signal-to-metrics rule with origin: %s", origin))

	if err := c.putSignalToMetrics(ctx, origin, ruleJSON, dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Signal-to-metrics rule created with origin: %s", origin))
	return nil
}

// GetSignalToMetrics retrieves the signal-to-metrics rule with the given
// origin as a JSON document.
//
// Signal-to-metrics deletes are soft: after a delete, this returns the
// tombstone document (carrying a dash0.com/deleted-at annotation) rather than
// a 404. The tombstone is returned verbatim — the resource layer owns the
// gone-detection branch, so this wrapper stays a thin passthrough.
func (c *dash0Client) GetSignalToMetrics(ctx context.Context, origin string, dataset string) (string, error) {
	rule, err := c.inner.GetSignalToMetrics(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Signal-to-metrics rule retrieved with origin: %s", origin))
	return marshalToJSON(rule)
}

// UpdateSignalToMetrics replaces the signal-to-metrics rule with the given
// origin. It is identical to CreateSignalToMetrics — PUT is create-or-replace.
func (c *dash0Client) UpdateSignalToMetrics(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	if err := c.putSignalToMetrics(ctx, origin, ruleJSON, dataset); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Signal-to-metrics rule updated with origin: %s", origin))
	return nil
}

// putSignalToMetrics sends the rule with PUT to the given origin.
//
// The server-managed labels and annotations are stripped from the body first,
// because the API rejects two of them when they disagree with the request:
//   - dash0.com/version is an optimistic-concurrency token. A stale value is
//     rejected with 400 ("version has been superseded"), so a document copied
//     from the API (the documented import flow) would fail on every apply
//     after the first.
//   - dash0.com/dataset must match ?dataset=, or the write is rejected with
//     400 ("conflicts with dataset in metadata"). The label is set to the
//     resource's dataset instead, so the `dataset` attribute is authoritative
//     even when the YAML was copied from another dataset.
//
// The write endpoints honor ?dataset= on their own; the label is only set so
// that it cannot contradict the query parameter.
//
// The dash0.com/id label is cleared too. The origin in the URL addresses the
// rule, and an id copied from another rule's document must not be sent along.
func (c *dash0Client) putSignalToMetrics(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	rule, err := unmarshalSignalToMetrics(ruleJSON)
	if err != nil {
		return fmt.Errorf("error parsing signal-to-metrics rule JSON: %w", err)
	}
	dash0.StripSignalToMetricsServerFields(rule)
	dash0.ClearSignalToMetricsID(rule)
	dash0.SetSignalToMetricsDataset(rule, dataset)

	_, err = c.inner.UpdateSignalToMetrics(ctx, origin, rule, &dataset)
	return err
}

// DeleteSignalToMetrics soft-deletes the signal-to-metrics rule with the given
// origin. Deleting an already-deleted rule returns an error that
// dash0.IsNotFound recognizes.
func (c *dash0Client) DeleteSignalToMetrics(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSignalToMetrics(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Signal-to-metrics rule deleted with origin: %s", origin))
	return nil
}

// ResolveSignalToMetrics looks up the server-assigned id of the
// signal-to-metrics rule with the given origin by matching against the list
// endpoint.
//
// Signal-to-metrics rules are not addressable in the Dash0 web app, so this
// function returns only an id (no deep-link URL). It returns an empty string
// (and no error) when the rule is not present in the list, so that callers
// can treat the id as best-effort metadata. An error from the list call itself
// is propagated — the resource layer is what downgrades it to a warning.
func (c *dash0Client) ResolveSignalToMetrics(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListSignalToMetrics(ctx, &dataset)
	if err != nil {
		return "", err
	}

	// Match on origin first, fall back to matching on id, so a rule can also be
	// imported by the id the list endpoint reports.
	id := matchOriginID(items, origin, func(rule *dash0.SignalToMetricsDefinition) (string, *string) {
		if rule.Metadata.Labels == nil {
			return "", nil
		}
		return dash0.GetSignalToMetricsID(rule), rule.Metadata.Labels.Dash0Comorigin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Signal-to-metrics rule with origin %q not found in dataset %q; id will be empty", origin, dataset))
		return "", nil
	}

	tflog.Debug(ctx, fmt.Sprintf("Resolved signal-to-metrics rule id for origin %s: %s", origin, id))
	return id, nil
}

// unmarshalSignalToMetrics parses a JSON string into a
// SignalToMetricsDefinition.
func unmarshalSignalToMetrics(jsonStr string) (*dash0.SignalToMetricsDefinition, error) {
	var rule dash0.SignalToMetricsDefinition
	if err := json.Unmarshal([]byte(jsonStr), &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}
