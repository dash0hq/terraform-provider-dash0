package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

// stringOrNull returns a null types.String for an empty input and a value-bearing
// types.String otherwise.
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// refreshedYAML renders a document read back from Dash0 as the YAML to store in
// a resource's `*_yaml` attribute, keeping the key order of priorYAML (the value
// it replaces, empty when there is none) so a plan renders a line-level diff
// instead of a full-document replacement.
//
// The client wrappers build the input with json.Marshal, so it always parses.
// If that ever stops holding, keep the document as it arrived rather than
// dropping the refresh, and log it: the resource silently reverts to the
// full-document plan diff this helper exists to prevent.
func refreshedYAML(ctx context.Context, apiResponse, priorYAML string) string {
	converted, err := converter.ConvertAPIResponseToYAML(apiResponse, priorYAML)
	if err != nil {
		tflog.Warn(
			ctx,
			"Unable to render the document returned by the Dash0 API as YAML; storing it unchanged. Plans for this resource will show a full-document replacement instead of a line-level diff.",
			map[string]interface{}{"error": err.Error()},
		)
		return apiResponse
	}
	return converted
}
