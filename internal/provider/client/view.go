package client

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	def, err := unmarshalView(viewJSON)
	if err != nil {
		return fmt.Errorf("error parsing view JSON: %w", err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating view with origin: %s", origin))

	_, err = c.inner.UpdateView(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("View created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetView(ctx context.Context, origin string, dataset string) (string, error) {
	def, err := c.inner.GetView(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("View retrieved with origin: %s", origin))
	return marshalToJSON(def)
}

func (c *dash0Client) UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	def, err := unmarshalView(viewJSON)
	if err != nil {
		return fmt.Errorf("error parsing view JSON: %w", err)
	}

	_, err = c.inner.UpdateView(ctx, origin, def, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("View updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteView(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteView(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("View deleted with origin: %s", origin))
	return nil
}

// GetViewURL builds a deep link to the Dash0 web app for the view with the
// given origin. The internal id and view type are resolved from the list
// endpoint by matching on origin; the view type selects the correct page (for
// example the traces explorer for span views).
//
// It returns an empty string (and no error) when the app base URL cannot be
// derived, the view is not present in the list, or the view type has no
// associated page, so that callers can treat the URL as best-effort metadata
// rather than failing the operation.
func (c *dash0Client) GetViewURL(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListViews(ctx, &dataset)
	if err != nil {
		return "", err
	}

	var matched *dash0.ViewApiListItem
	for _, item := range items {
		if item != nil && item.Origin != nil && *item.Origin == origin {
			matched = item
			break
		}
	}
	if matched == nil {
		tflog.Warn(ctx, fmt.Sprintf("View with origin %q not found in dataset %q; view URL will be empty", origin, dataset))
		return "", nil
	}

	viewURL := dash0.ViewDeeplinkURL(c.apiURL, matched.Type, matched.Id, &dataset)
	logResolvedURL(ctx, "view", origin, viewURL)
	return viewURL, nil
}
