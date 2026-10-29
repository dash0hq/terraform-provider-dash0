package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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

// errorIfEnabledAbsent requires the user's YAML, held in the given attribute,
// to declare spec.enabled. asset names the asset kind in the error message.
//
// Use it for asset kinds whose spec.enabled is a non-pointer bool with no
// omitempty in the API client: every write the provider makes asserts a value,
// and an omitted field is sent as false. Accepting the omission would mean an
// asset someone enabled in the Dash0 UI is silently disabled again by the next
// unrelated apply. Requiring it makes that intent explicit and keeps the field
// in drift detection.
func errorIfEnabledAbsent(parsed map[string]interface{}, attribute string, asset string, diags *diag.Diagnostics) {
	if spec, ok := parsed["spec"].(map[string]interface{}); ok {
		if _, present := spec["enabled"]; present {
			return
		}
	}
	diags.AddAttributeError(
		path.Root(attribute),
		"spec.enabled must be set explicitly",
		fmt.Sprintf("%s must declare `spec.enabled`. The provider always sends a value for this field, so "+
			"omitting it silently creates a disabled %s — and a later apply would switch off a %s that had "+
			"been enabled outside Terraform. Set `spec.enabled: true` to have the %s take effect, or "+
			"`spec.enabled: false` to declare it intentionally disabled.",
			attribute, asset, asset, asset),
	)
}

// warnIfCustomLabelsSet emits a Warning on the given attribute when the user's
// YAML declares metadata.labels.custom. Normalization strips the entire
// metadata.labels subtree before comparison, so changes to these labels are
// invisible to drift detection: the plan stays clean no matter what the server
// stores.
func warnIfCustomLabelsSet(parsed map[string]interface{}, attribute string, diags *diag.Diagnostics) {
	metadata, ok := parsed["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		return
	}
	custom, ok := labels["custom"].(map[string]interface{})
	if !ok || len(custom) == 0 {
		return
	}
	keys := make([]string, 0, len(custom))
	for k := range custom {
		keys = append(keys, fmt.Sprintf("%v", k))
	}
	slices.Sort(keys)
	diags.AddAttributeWarning(
		path.Root(attribute),
		"metadata.labels.custom is not covered by drift detection",
		fmt.Sprintf("metadata.labels.custom is declared (keys: %s), but the provider strips the whole "+
			"metadata.labels subtree before comparing the configuration with the API response. Changes to "+
			"these labels are therefore silently lossy: they are sent on write, but the plan stays clean "+
			"whether or not the server stored them, and an out-of-band change to them is never reported.",
			strings.Join(keys, ", ")),
	)
}
