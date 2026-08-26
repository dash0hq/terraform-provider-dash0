package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

// stringOrNull returns a null types.String for an empty input.
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// refreshedYAML renders a document read back from Dash0 as YAML for a `*_yaml`
// attribute, aligned to priorYAML (the value it replaces, "" for none) so a plan
// shows a line-level diff. On failure it keeps the document as it arrived and
// logs, since the resource reverts to the diff this exists to prevent.
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
