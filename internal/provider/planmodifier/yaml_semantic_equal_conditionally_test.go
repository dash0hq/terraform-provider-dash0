package planmodifier

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

// configOmittingPermissions declares no spec.permissions; the state below has
// it, the way the API enriches it on retrieval. "spec.permissions" is a member
// of the shared converter.ConditionallyIgnoredFields global, so whether these
// two compare equal is decided entirely by which list the modifier consults.
const (
	configOmittingPermissions = `spec:
  display:
    name: Example
`
	stateWithPermissions = `spec:
  display:
    name: Example
  permissions:
    - subject: everyone
      level: read
`
)

func runModifier(m planmodifier.String, config, state string) types.String {
	req := planmodifier.StringRequest{
		ConfigValue: types.StringValue(config),
		StateValue:  types.StringValue(state),
		PlanValue:   types.StringValue(config),
	}
	resp := &planmodifier.StringResponse{PlanValue: types.StringValue(config)}
	m.PlanModifyString(context.Background(), req, resp)
	return resp.PlanValue
}

// TestYAMLSemanticEqualConditionally_NilFallsBackToGlobal pins the branch where
// conditionallyIgnoredFields is nil: the modifier must consult the shared
// converter.ConditionallyIgnoredFields global, which is what every resource
// built on YAMLSemanticEqual relies on.
func TestYAMLSemanticEqualConditionally_NilFallsBackToGlobal(t *testing.T) {
	require.Contains(t, converter.ConditionallyIgnoredFields, "spec.permissions",
		"this test is only meaningful while spec.permissions is in the shared global")

	// YAMLSemanticEqual leaves conditionallyIgnoredFields nil.
	plan := runModifier(YAMLSemanticEqual(), configOmittingPermissions, stateWithPermissions)

	assert.Equal(t, types.StringValue(stateWithPermissions), plan,
		"a nil list must fall back to the global, so an undeclared spec.permissions is ignored and state is preserved")
}

// TestYAMLSemanticEqualConditionally_EmptySliceSuppressesAllIgnoring pins the
// other side of the nil-versus-empty distinction: an explicitly empty, non-nil
// slice means "ignore nothing conditionally", not "use the default". A
// maintainer who changes the guard from `!= nil` to `len(...) > 0` would
// silently turn this case back into the global fallback.
func TestYAMLSemanticEqualConditionally_EmptySliceSuppressesAllIgnoring(t *testing.T) {
	plan := runModifier(YAMLSemanticEqualConditionally([]string{}), configOmittingPermissions, stateWithPermissions)

	assert.Equal(t, types.StringValue(configOmittingPermissions), plan,
		"an empty non-nil list must suppress all conditional ignoring, so the missing spec.permissions is a real diff")
}

// TestYAMLSemanticEqualConditionally_CustomListReplacesGlobal covers the
// intended use: a resource-scoped list that adds fields the global does not
// carry (the time series aggregation resource's spec.enabled / spec.priority).
func TestYAMLSemanticEqualConditionally_CustomListReplacesGlobal(t *testing.T) {
	config := `spec:
  display:
    name: Example
`
	state := `spec:
  display:
    name: Example
  enabled: false
  priority: 2
`

	// With the resource-scoped list, the server defaults are ignored because the
	// config does not declare them.
	scoped := runModifier(
		YAMLSemanticEqualConditionally([]string{"spec.enabled", "spec.priority"}),
		config, state,
	)
	assert.Equal(t, types.StringValue(state), scoped,
		"undeclared server-defaulted fields must not produce a diff")

	// With the global (nil) list they are not ignored, which is exactly why the
	// list has to be scoped per resource rather than appended to the global.
	global := runModifier(YAMLSemanticEqual(), config, state)
	assert.Equal(t, types.StringValue(config), global,
		"the global list does not cover spec.enabled/spec.priority; the scoped list is what makes the difference")
}
