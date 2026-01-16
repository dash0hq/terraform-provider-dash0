package planmodifier

import (
	"context"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// YAMLSemanticEqual returns a plan modifier that preserves state when
// YAML values are semantically equivalent (ignoring formatting differences
// like key ordering and string quoting).
func YAMLSemanticEqual() planmodifier.String {
	return yamlSemanticEqualModifier{}
}

type yamlSemanticEqualModifier struct{}

func (m yamlSemanticEqualModifier) Description(_ context.Context) string {
	return "Preserves state when YAML values are semantically equivalent"
}

func (m yamlSemanticEqualModifier) MarkdownDescription(_ context.Context) string {
	return "Preserves state when YAML values are semantically equivalent"
}

func (m yamlSemanticEqualModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// If config is null or unknown, no modification needed
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	// If state is null (new resource), no comparison needed
	if req.StateValue.IsNull() {
		return
	}

	// Compare the config YAML with the state YAML semantically
	configYAML := req.ConfigValue.ValueString()
	stateYAML := req.StateValue.ValueString()

	equivalent, err := converter.ResourceYAMLEquivalent(configYAML, stateYAML)
	if err != nil {
		// On error, let Terraform use normal comparison
		return
	}

	if equivalent {
		// If semantically equal, use the state value to prevent unnecessary diff
		resp.PlanValue = req.StateValue
	}
}
