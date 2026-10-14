package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"

	dash0 "github.com/dash0hq/dash0-api-client-go"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// annotationDeletedAt marks a soft-deleted asset. For most asset kinds a
// DELETE is soft: a later GET returns 200 with this annotation set rather
// than a 404.
const annotationDeletedAt = "dash0.com/deleted-at"

// removedOutOfBand drops the resource from state and returns true when the
// asset no longer exists on the server: the Get call returned a 404, or it
// returned a soft-delete tombstone in body. Callers return right away when it
// reports true.
//
// The Plugin Framework contract for "gone from the underlying system" is to
// clear state so the next plan re-creates the resource. Surfacing an error
// would force the user to `terraform state rm` by hand.
//
// Any other err is left for the caller, which keeps its own error message.
func removedOutOfBand(ctx context.Context, resp *resource.ReadResponse, kind, id, body string, err error) bool {
	switch {
	case err != nil && dash0.IsNotFound(err):
		tflog.Debug(ctx, fmt.Sprintf("%s %s no longer exists on the server; removing from state", kind, id))
	case err == nil && isSoftDeleted(body):
		tflog.Debug(ctx, fmt.Sprintf("%s %s is soft-deleted on the server; removing from state", kind, id))
	default:
		return false
	}
	resp.State.RemoveResource(ctx)
	return true
}

// removedOutOfBandInDataset is removedOutOfBand for dataset-scoped assets.
// Soft-deleting a dataset keeps its assets alive but makes asset GETs return
// 404. Dropping them from state would make the next apply create a second copy
// of every asset, so on a 404 it checks the dataset first:
//   - dataset exists: the asset is really gone, drop it from state.
//   - dataset is gone too: keep state untouched and warn.
//   - dataset check fails: keep state untouched and error.
//
// It returns true when the caller must return right away.
func removedOutOfBandInDataset(ctx context.Context, resp *resource.ReadResponse, c client.Client, dataset, kind, id, body string, err error) bool {
	if err == nil || !dash0.IsNotFound(err) {
		return removedOutOfBand(ctx, resp, kind, id, body, err)
	}
	_, dsErr := c.GetDataset(ctx, dataset)
	switch {
	case dsErr == nil:
		return removedOutOfBand(ctx, resp, kind, id, body, err)
	case dash0.IsNotFound(dsErr):
		resp.Diagnostics.AddWarning(
			"Dataset is deleted",
			fmt.Sprintf("dataset %s is deleted; keeping %s %s in state, it comes back when the dataset is restored", dataset, kind, id),
		)
	default:
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("%s %s returned 404 and the dataset %s cannot be checked, so it is kept in state: %s", kind, id, dataset, dsErr),
		)
	}
	return true
}

// isSoftDeleted reports whether the given API response is a soft-delete
// tombstone: metadata.annotations["dash0.com/deleted-at"] is set.
func isSoftDeleted(apiResponseJSON string) bool {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(apiResponseJSON), &parsed); err != nil {
		return false
	}
	metadata, ok := parsed["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	annotations, ok := metadata["annotations"].(map[string]interface{})
	if !ok {
		return false
	}
	deletedAt, ok := annotations[annotationDeletedAt].(string)
	return ok && deletedAt != ""
}
