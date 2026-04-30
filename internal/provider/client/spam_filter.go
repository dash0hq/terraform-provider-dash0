package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

func (c *dash0Client) CreateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error {
	filter, err := unmarshalSpamFilter(filterJSON)
	if err != nil {
		return fmt.Errorf("error parsing spam filter JSON: %w", err)
	}

	setSpamFilterOrigin(filter, origin)
	dash0.SetSpamFilterDataset(filter, dataset)

	tflog.Debug(ctx, fmt.Sprintf("Creating spam filter with origin: %s", origin))

	_, err = c.inner.UpdateSpamFilter(ctx, origin, filter, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter created with origin: %s", origin))
	return nil
}

func (c *dash0Client) GetSpamFilter(ctx context.Context, origin string, dataset string) (string, error) {
	filter, err := c.inner.GetSpamFilter(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter retrieved with origin: %s", origin))

	dash0.StripSpamFilterServerFields(filter)
	return marshalToJSON(filter)
}

func (c *dash0Client) UpdateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error {
	filter, err := unmarshalSpamFilter(filterJSON)
	if err != nil {
		return fmt.Errorf("error parsing spam filter JSON: %w", err)
	}

	setSpamFilterOrigin(filter, origin)
	dash0.SetSpamFilterDataset(filter, dataset)

	_, err = c.inner.UpdateSpamFilter(ctx, origin, filter, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter updated with origin: %s", origin))
	return nil
}

func (c *dash0Client) DeleteSpamFilter(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSpamFilter(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter deleted with origin: %s", origin))
	return nil
}

// unmarshalSpamFilter parses a JSON string into a SpamFilter.
func unmarshalSpamFilter(jsonStr string) (*dash0.SpamFilter, error) {
	var filter dash0.SpamFilter
	if err := json.Unmarshal([]byte(jsonStr), &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

// setSpamFilterOrigin sets the origin label on a spam filter.
func setSpamFilterOrigin(filter *dash0.SpamFilter, origin string) {
	if filter.Metadata.Labels == nil {
		filter.Metadata.Labels = &dash0.SpamFilterLabels{}
	}
	filter.Metadata.Labels.Dash0Comorigin = &origin
}
