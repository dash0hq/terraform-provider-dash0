package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	def, err := unmarshalSyntheticCheck(checkJSON)
	if err != nil {
		return fmt.Errorf("error parsing synthetic check JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating synthetic check with origin: %s", origin))

	_, err = c.inner.UpdateSyntheticCheck(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetSyntheticCheck(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	def, err := unmarshalSyntheticCheck(checkJSON)
	if err != nil {
		return fmt.Errorf("error parsing synthetic check JSON: %w", err)
	}

	_, err = c.inner.UpdateSyntheticCheck(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSyntheticCheck(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Synthetic check deleted with origin: %s", origin))
	return nil
}

// GetSyntheticCheckURL builds a deep link to the Dash0 web app for the synthetic
// check with the given origin. The internal id is resolved from the list
// endpoint by matching on origin (see matchOriginID).
//
// It returns an empty string (and no error) when the app base URL cannot be
// derived or the synthetic check is not present in the list, so that callers
// can treat the URL as best-effort metadata rather than failing the operation.
func (c *dash0Client) GetSyntheticCheckURL(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListSyntheticChecks(ctx, &dataset)
	if err != nil {
		return "", err
	}

	id := matchOriginID(items, origin, func(item *dash0.SyntheticChecksApiListItem) (string, *string) {
		return item.Id, item.Origin
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("Synthetic check with origin %q not found in dataset %q; synthetic check URL will be empty", origin, dataset))
		return "", nil
	}

	syntheticCheckURL := dash0.DeeplinkURL(c.apiURL, dash0.DeeplinkAssetTypeSyntheticCheck, id, &dataset)
	logResolvedURL(ctx, "synthetic check", origin, syntheticCheckURL)
	return syntheticCheckURL, nil
}
