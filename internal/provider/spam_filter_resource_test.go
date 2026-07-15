package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"

	customplanmodifier "github.com/dash0hq/terraform-provider-dash0/internal/provider/planmodifier"
)

// TestSpamFilterResource_SharingAnnotationIgnored verifies that spam filters
// do NOT preserve dash0.com/sharing — changes to it should not trigger a replan.
func TestSpamFilterResource_SharingAnnotationIgnored(t *testing.T) {
	modifier := customplanmodifier.YAMLSemanticEqual() // no preserved annotation keys

	configValue := types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  jsonPath: $.body
  matchType: contains
  matchValue: spam
`)
	stateValue := types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: private
spec:
  jsonPath: $.body
  matchType: contains
  matchValue: spam
`)

	req := planmodifier.StringRequest{
		ConfigValue: configValue,
		StateValue:  stateValue,
		PlanValue:   configValue,
	}
	resp := &planmodifier.StringResponse{
		PlanValue: configValue,
	}

	modifier.PlanModifyString(context.Background(), req, resp)

	assert.Equal(t, stateValue, resp.PlanValue,
		"Should use state value when dash0.com/sharing is not in the preserved list (spam filter)")
}
