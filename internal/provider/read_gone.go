package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"

	dash0 "github.com/dash0hq/dash0-api-client-go"
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
