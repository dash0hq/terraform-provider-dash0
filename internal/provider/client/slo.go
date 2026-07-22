package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateSLO(ctx context.Context, origin string, sloJSON string, dataset string) error {
	def, err := unmarshalSLO(sloJSON)
	if err != nil {
		return fmt.Errorf("error parsing SLO JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating SLO with origin: %s", origin))

	_, err = c.inner.UpdateSLO(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("SLO created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetSLO(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetSLO(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("SLO retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateSLO(ctx context.Context, origin string, sloJSON string, dataset string) error {
	def, err := unmarshalSLO(sloJSON)
	if err != nil {
		return fmt.Errorf("error parsing SLO JSON: %w", err)
	}

	_, err = c.inner.UpdateSLO(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("SLO updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteSLO(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSLO(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("SLO deleted with origin: %s", origin))
	return nil
}

// ResolveSLO looks up the server-assigned id and deep-link URL for the SLO with
// the given origin by matching against the list endpoint (see matchOriginID).
//
// It returns empty strings (and no error) when the SLO is not present in the
// list, so that callers can treat both fields as best-effort metadata rather
// than failing the operation. The URL is additionally empty when the app base
// URL cannot be derived from the API URL.
func (c *dash0Client) ResolveSLO(ctx context.Context, origin string, dataset string) (string, string, error) {
	items, err := c.inner.ListSLOs(ctx, &dataset)
	if err != nil {
		return "", "", err
	}

	id := matchOriginID(items, origin, func(item *dash0.SloDefinition) (string, *string) {
		var originPtr *string
		if item.Metadata.Labels != nil {
			originPtr = item.Metadata.Labels.Dash0Comorigin
		}
		return dash0.GetSLOID(item), originPtr
	})
	if id == "" {
		tflog.Warn(ctx, fmt.Sprintf("SLO with origin %q not found in dataset %q; id and URL will be empty", origin, dataset))
		return "", "", nil
	}

	sloURL := dash0.DeeplinkURL(c.apiURL, dash0.DeeplinkAssetTypeSLO, id, &dataset)
	logResolvedURL(ctx, "SLO", origin, sloURL)
	return id, sloURL, nil
}
