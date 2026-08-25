package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"

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
// Should that ever stop holding, keep the document as it arrived rather than
// dropping the refresh.
func refreshedYAML(apiResponse, priorYAML string) string {
	converted, err := converter.ConvertAPIResponseToYAML(apiResponse, priorYAML)
	if err != nil {
		return apiResponse
	}
	return converted
}
