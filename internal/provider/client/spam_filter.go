package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// upsertOp identifies whether a create-or-replace call originated from a
// Create or an Update at the resource layer. It only affects log wording —
// the underlying HTTP call is a PUT either way.
type upsertOp int

const (
	upsertCreate upsertOp = iota
	upsertUpdate
)

func (o upsertOp) pastTense() string {
	switch o {
	case upsertCreate:
		return "created"
	case upsertUpdate:
		return "updated"
	default:
		return "upserted"
	}
}

func (c *dash0Client) CreateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error {
	return c.upsertSpamFilter(ctx, origin, filterJSON, dataset, upsertCreate)
}

func (c *dash0Client) UpdateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error {
	return c.upsertSpamFilter(ctx, origin, filterJSON, dataset, upsertUpdate)
}

func (c *dash0Client) upsertSpamFilter(ctx context.Context, origin, filterJSON, dataset string, op upsertOp) error {
	isV1Alpha2, err := spamFilterIsV1Alpha2(filterJSON)
	if err != nil {
		return fmt.Errorf("error parsing spam filter JSON: %w", err)
	}

	if isV1Alpha2 {
		filter, err := unmarshalSpamFilterV1Alpha2(filterJSON)
		if err != nil {
			return fmt.Errorf("error parsing spam filter JSON: %w", err)
		}
		// Normalize to the bare apiVersion the SDK accepts on the response side
		// (it rejects operator-style prefixes like "operator.dash0.com/v1alpha2"
		// during response decoding).
		filter.ApiVersion = dash0.V1alpha2
		setSpamFilterMetadataOrigin(&filter.Metadata, origin)
		setSpamFilterMetadataDataset(&filter.Metadata, dataset)

		tflog.Debug(ctx, fmt.Sprintf("Upserting v1alpha2 spam filter with origin: %s", origin))
		if _, err := c.inner.UpdateSpamFilterV1Alpha2(ctx, origin, filter, &dataset); err != nil {
			return err
		}
	} else {
		filter, err := unmarshalSpamFilter(filterJSON)
		if err != nil {
			return fmt.Errorf("error parsing spam filter JSON: %w", err)
		}
		// See comment in the v1alpha2 branch above.
		v1alpha1 := dash0.V1alpha1
		filter.ApiVersion = &v1alpha1
		setSpamFilterMetadataOrigin(&filter.Metadata, origin)
		setSpamFilterMetadataDataset(&filter.Metadata, dataset)

		tflog.Debug(ctx, fmt.Sprintf("Upserting v1alpha1 spam filter with origin: %s", origin))
		if _, err := c.inner.UpdateSpamFilter(ctx, origin, filter, &dataset); err != nil {
			return err
		}
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter %s with origin: %s", op.pastTense(), origin))
	return nil
}

func (c *dash0Client) GetSpamFilter(ctx context.Context, origin string, dataset string) (string, error) {
	obj, err := c.inner.GetSpamFilter(ctx, origin, &dataset)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter retrieved with origin: %s", origin))

	switch f := obj.(type) {
	case *dash0.SpamFilter:
		stripSpamFilterMetadataServerFields(&f.Metadata)
		return marshalToJSON(f)
	case *dash0.SpamFilterV1Alpha2:
		stripSpamFilterMetadataServerFields(&f.Metadata)
		return marshalToJSON(f)
	default:
		return "", fmt.Errorf("unsupported spam filter type %T", obj)
	}
}

func (c *dash0Client) DeleteSpamFilter(ctx context.Context, origin string, dataset string) error {
	err := c.inner.DeleteSpamFilter(ctx, origin, &dataset)
	if err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Spam filter deleted with origin: %s", origin))
	return nil
}

// ResolveSpamFilter looks up the server-assigned id of the spam filter with
// the given origin by matching against the list endpoint. Both v1alpha1 and
// v1alpha2 are handled — origin and id labels live on the shared metadata
// shape.
//
// Spam filters are not addressable in the Dash0 web app, so this function
// returns only an id (no deep-link URL). It returns an empty string (and no
// error) when the spam filter is not present in the list, so that callers can
// treat the id as best-effort metadata rather than failing the operation.
func (c *dash0Client) ResolveSpamFilter(ctx context.Context, origin string, dataset string) (string, error) {
	items, err := c.inner.ListSpamFilterObjects(ctx, &dataset)
	if err != nil {
		return "", err
	}

	// Match on origin first, fall back to matching on id — see matchOriginID for
	// the rationale (imports of UI-created filters have no origin label).
	for _, obj := range items {
		var meta *dash0.SpamFilterMetadata
		switch f := obj.(type) {
		case *dash0.SpamFilter:
			if f != nil {
				meta = &f.Metadata
			}
		case *dash0.SpamFilterV1Alpha2:
			if f != nil {
				meta = &f.Metadata
			}
		}
		if meta == nil || meta.Labels == nil {
			continue
		}
		var itemID string
		if meta.Labels.Dash0Comid != nil {
			itemID = *meta.Labels.Dash0Comid
		}
		originMatches := meta.Labels.Dash0Comorigin != nil && *meta.Labels.Dash0Comorigin == origin
		idMatches := itemID != "" && itemID == origin
		if !originMatches && !idMatches {
			continue
		}
		tflog.Debug(ctx, fmt.Sprintf("Resolved spam filter id for origin %s: %s", origin, itemID))
		return itemID, nil
	}

	tflog.Warn(ctx, fmt.Sprintf("Spam filter with origin %q not found in dataset %q; id will be empty", origin, dataset))
	return "", nil
}

// spamFilterIsV1Alpha2 reports whether the JSON document declares apiVersion
// v1alpha2. The apiVersion may be either the bare form ("v1alpha2") or the
// operator-style prefixed form ("operator.dash0.com/v1alpha2"); both are
// accepted.
func spamFilterIsV1Alpha2(jsonStr string) (bool, error) {
	var head struct {
		ApiVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &head); err != nil {
		return false, err
	}
	return strings.HasSuffix(head.ApiVersion, "v1alpha2"), nil
}

// unmarshalSpamFilter parses a JSON string into a v1alpha1 SpamFilter.
func unmarshalSpamFilter(jsonStr string) (*dash0.SpamFilter, error) {
	var filter dash0.SpamFilter
	if err := json.Unmarshal([]byte(jsonStr), &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

// unmarshalSpamFilterV1Alpha2 parses a JSON string into a v1alpha2 SpamFilter.
func unmarshalSpamFilterV1Alpha2(jsonStr string) (*dash0.SpamFilterV1Alpha2, error) {
	var filter dash0.SpamFilterV1Alpha2
	if err := json.Unmarshal([]byte(jsonStr), &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

// setSpamFilterMetadataOrigin sets the dash0.com/origin label on a spam filter's
// metadata. SpamFilterMetadata is shared between v1alpha1 and v1alpha2.
func setSpamFilterMetadataOrigin(meta *dash0.SpamFilterMetadata, origin string) {
	if meta.Labels == nil {
		meta.Labels = &dash0.SpamFilterLabels{}
	}
	meta.Labels.Dash0Comorigin = &origin
}

// setSpamFilterMetadataDataset sets the dash0.com/dataset label on a spam
// filter's metadata. SpamFilterMetadata is shared between v1alpha1 and v1alpha2.
func setSpamFilterMetadataDataset(meta *dash0.SpamFilterMetadata, dataset string) {
	if meta.Labels == nil {
		meta.Labels = &dash0.SpamFilterLabels{}
	}
	meta.Labels.Dash0Comdataset = &dataset
}

// stripSpamFilterMetadataServerFields removes server-generated fields from a
// spam filter's metadata. The SpamFilterMetadata struct is shared between
// v1alpha1 and v1alpha2, so the same helper covers both shapes — unlike
// dash0.StripSpamFilterServerFields which is bound to *SpamFilter (v1alpha1).
func stripSpamFilterMetadataServerFields(meta *dash0.SpamFilterMetadata) {
	if meta == nil {
		return
	}
	if meta.Annotations != nil {
		meta.Annotations.Dash0Comenabled = nil
	}
	if meta.Labels != nil {
		meta.Labels.Dash0Comid = nil
		meta.Labels.Dash0Comsource = nil
	}
}
