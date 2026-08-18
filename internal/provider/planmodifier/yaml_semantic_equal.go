package planmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

// YAMLSemanticEqual returns a plan modifier that preserves state when
// YAML values are semantically equivalent (ignoring formatting differences
// like key ordering and string quoting).
// preservedAnnotationKeys lists metadata annotation keys that should
// participate in drift detection (e.g., "dash0.com/sharing"). All other
// metadata annotations are stripped before comparison. If no keys are
// provided, all metadata annotations are stripped.
func YAMLSemanticEqual(preservedAnnotationKeys ...string) planmodifier.String {
	return yamlSemanticEqualModifier{
		preservedAnnotationKeys: preservedAnnotationKeys,
	}
}

// YAMLSemanticEqualWith returns a plan modifier like YAMLSemanticEqual, but
// also strips alwaysIgnoredFields from both YAMLs before comparison. Use this
// when a resource has fields that are fully API-managed (e.g. back-references
// populated by other resources) so the user's config and the API response
// stay equivalent regardless of what the server writes there.
func YAMLSemanticEqualWith(alwaysIgnoredFields []string, preservedAnnotationKeys ...string) planmodifier.String {
	return yamlSemanticEqualModifier{
		preservedAnnotationKeys: preservedAnnotationKeys,
		alwaysIgnoredFields:     alwaysIgnoredFields,
	}
}

// YAMLSemanticEqualNormalizing returns a plan modifier like YAMLSemanticEqual
// that first puts both sides through normalize. Use it when a resource stores
// a document in a different shape than the user writes it, so the two are only
// comparable once that difference is reconciled.
func YAMLSemanticEqualNormalizing(normalize func(string) string, preservedAnnotationKeys ...string) planmodifier.String {
	return yamlSemanticEqualModifier{
		preservedAnnotationKeys: preservedAnnotationKeys,
		normalize:               normalize,
	}
}

type yamlSemanticEqualModifier struct {
	preservedAnnotationKeys []string
	alwaysIgnoredFields     []string

	// normalize, when set, is applied to both sides before comparing. Opt-in
	// per resource: this modifier is shared, and a reconciliation that one
	// resource needs is usually wrong for the others.
	normalize func(string) string
}

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

	if m.normalize != nil {
		configYAML = m.normalize(configYAML)
		stateYAML = m.normalize(stateYAML)
	}

	// Conditionally ignore API-managed fields that the user didn't include in their config.
	// e.g., spec.permissions is enriched by the API on retrieval but users may optionally manage it.
	additionalIgnored := converter.FieldsAbsentFromYAML(configYAML, converter.ConditionallyIgnoredFields)
	additionalIgnored = append(additionalIgnored, m.alwaysIgnoredFields...)
	equivalent, err := converter.ResourceYAMLEquivalent(configYAML, stateYAML, additionalIgnored, m.preservedAnnotationKeys)
	if err != nil {
		// On error, let Terraform use normal comparison
		return
	}

	if equivalent {
		// If semantically equal, use the state value to prevent unnecessary diff
		resp.PlanValue = req.StateValue
	}
}
