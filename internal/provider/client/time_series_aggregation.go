package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateTimeSeriesAggregation creates a time series aggregation under the given
// origin.
//
// Creation goes through PUT, not POST: POST rejects an origin that already
// exists, so PUT-to-origin is the only idempotent path and matches the
// provider's create-or-replace convention.
//
// The request body is passed through untouched — in particular no
// dash0.com/origin label is stamped into it. The server derives the origin
// from the URL path.
func (c *dash0Client) CreateTimeSeriesAggregation(ctx context.Context, origin string, tsaJSON string, dataset string) error {
	def, err := unmarshalTimeSeriesAggregation(tsaJSON)
	if err != nil {
		return fmt.Errorf("error parsing time series aggregation JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating time series aggregation with origin: %s", origin))

	_, err = c.inner.UpdateTimeSeriesAggregation(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Time series aggregation created with origin: %s", origin))
	return nil
}

// GetTimeSeriesAggregation retrieves the time series aggregation with the given
// origin as a JSON document.
//
// Time series aggregation deletes are soft: after a delete, this returns the
// tombstone document (carrying a dash0.com/deleted-at annotation) rather than
// a 404. The tombstone is returned verbatim — the resource layer owns the
// gone-detection branch, so this wrapper stays a thin passthrough.
func (c *dash0Client) GetTimeSeriesAggregation(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetTimeSeriesAggregation(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Time series aggregation retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

// UpdateTimeSeriesAggregation replaces the time series aggregation with the
// given origin. It is identical to CreateTimeSeriesAggregation — PUT is
// create-or-replace.
func (c *dash0Client) UpdateTimeSeriesAggregation(ctx context.Context, origin string, tsaJSON string, dataset string) error {
	def, err := unmarshalTimeSeriesAggregation(tsaJSON)
	if err != nil {
		return fmt.Errorf("error parsing time series aggregation JSON: %w", err)
	}

	_, err = c.inner.UpdateTimeSeriesAggregation(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Time series aggregation updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteTimeSeriesAggregation(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteTimeSeriesAggregation(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Time series aggregation deleted with origin: %s", origin))
	return nil
}

// ResolveTimeSeriesAggregation looks up the server-assigned id of the time
// series aggregation with the given origin by matching against the list
// endpoint.
//
// Time series aggregations are not addressable in the Dash0 web app, so this
// function returns only an id (no deep-link URL). It returns an empty string
// (and no error) when the aggregation is not present in the list, so that
// callers can treat the id as best-effort metadata. An error from the list
// call itself is propagated — the resource layer is what downgrades it to a
// warning.
func (c *dash0Client) ResolveTimeSeriesAggregation(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListTimeSeriesAggregations(ctx, &dataset)
	if err != nil {
		return "", err
	}

	// Match on origin first, fall back to matching on id. Unlike the other asset
	// kinds, dash0.com/origin is mandatory on every time series aggregation and
	// is its only upsert key, so the origin branch is the one that fires in
	// practice; the id fallback exists so an aggregation can also be imported by
	// the id the list endpoint reports.
	id := matchOriginID(items, origin, func(def *dash0.TimeSeriesAggregationDefinition) (string, *string) {
		if def.Metadata.Labels == nil {
			return "", nil
		}
		var itemID string
		if def.Metadata.Labels.Dash0Comid != nil {
			itemID = *def.Metadata.Labels.Dash0Comid
		}
		return itemID, def.Metadata.Labels.Dash0Comorigin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Time series aggregation with origin %q not found in dataset %q; id will be empty", origin, dataset))
		return "", nil
	}

	tflog.Debug(ctx, fmt.Sprintf("Resolved time series aggregation id for origin %s: %s", origin, id))
	return id, nil
}

// unmarshalTimeSeriesAggregation parses a JSON string into a
// TimeSeriesAggregationDefinition.
func unmarshalTimeSeriesAggregation(jsonStr string) (*dash0.TimeSeriesAggregationDefinition, error) {
	var def dash0.TimeSeriesAggregationDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}
