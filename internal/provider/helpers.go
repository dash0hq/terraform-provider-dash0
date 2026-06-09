package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// stringOrNull returns a null types.String for an empty input and a value-bearing
// types.String otherwise.
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
