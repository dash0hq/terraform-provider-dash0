package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// CreateDataset creates a dataset with POST /api/datasets. The slug is read
// from the dash0.com/id label of the provided JSON and is required: it is
// immutable and the only identifier a dataset has. When origin is non-empty it
// is stamped onto dash0.com/origin.
//
// A create with the slug of a soft-deleted dataset inside the grace period
// restores that dataset. The server ignores spec.permissions on create and no
// follow-up update is made, so permissions are not applied.
func (c *dash0Client) CreateDataset(ctx context.Context, origin string, datasetJSON string) error {
	def, err := unmarshalDataset(datasetJSON)
	if err != nil {
		return fmt.Errorf("error parsing dataset JSON: %w", err)
	}

	slug := dash0.GetDatasetID(def)
	if slug == "" {
		return fmt.Errorf("dataset JSON must set the dash0.com/id label (the dataset slug)")
	}

	setDatasetOrigin(def, origin)

	tflog.Debug(ctx, fmt.Sprintf("Creating dataset with slug: %s", slug))

	if _, err = c.inner.CreateDataset(ctx, def); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dataset created with slug: %s", slug))
	return nil
}

// GetDataset retrieves the dataset with the given slug. Server-managed fields
// are stripped so drift detection ignores them. spec.permissions is also
// cleared: this provider version treats it as read-only, so it must never
// cause drift. dash0.com/id and dash0.com/origin are kept.
func (c *dash0Client) GetDataset(ctx context.Context, slug string) (string, error) {
	def, err := c.inner.GetDataset(ctx, slug)
	if err != nil {
		return "", err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dataset retrieved with slug: %s", slug))

	dash0.StripDatasetServerFields(def)
	def.Spec.Permissions = nil

	return marshalToJSON(def)
}

// UpdateDataset updates the dataset with the given slug using PUT. The slug is
// written into dash0.com/id so the body matches the path. When origin is
// non-empty it is stamped onto dash0.com/origin.
//
// Settings the caller omits keep their stored value on the server.
func (c *dash0Client) UpdateDataset(ctx context.Context, slug string, origin string, datasetJSON string) error {
	def, err := unmarshalDataset(datasetJSON)
	if err != nil {
		return fmt.Errorf("error parsing dataset JSON: %w", err)
	}

	dash0.SetDatasetID(def, slug)
	setDatasetOrigin(def, origin)

	if _, err = c.inner.UpdateDataset(ctx, slug, def); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dataset updated with slug: %s", slug))
	return nil
}

// DeleteDataset deletes the dataset with the given slug. The delete is soft:
// the dataset can be restored by creating one with the same slug during the
// 30-day grace period, after which the dataset, its permissions and its assets
// are removed. Telemetry is never deleted here: it follows each signal's
// retention. The server also
// answers 200 for an unknown or already-deleted dataset, so a nil error does
// not prove the dataset existed.
func (c *dash0Client) DeleteDataset(ctx context.Context, slug string) error {
	if err := c.inner.DeleteDataset(ctx, slug); err != nil {
		return err
	}

	tflog.Debug(ctx, fmt.Sprintf("Dataset deleted with slug: %s", slug))
	return nil
}

// unmarshalDataset parses a JSON string into a DatasetDefinition.
func unmarshalDataset(jsonStr string) (*dash0.DatasetDefinition, error) {
	var def dash0.DatasetDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// setDatasetOrigin stamps origin into metadata.labels["dash0.com/origin"],
// initializing labels as needed. An empty origin leaves the definition as is.
func setDatasetOrigin(def *dash0.DatasetDefinition, origin string) {
	if origin == "" {
		return
	}
	if def.Metadata.Labels == nil {
		def.Metadata.Labels = &dash0.DatasetLabels{}
	}
	def.Metadata.Labels.Dash0Comorigin = &origin
}
