package planmodifier

import (
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

func TestYAMLSemanticEqualComparing_PlanModifyString(t *testing.T) {
	config := "spec:\n  name: config\n"
	state := "spec:\n  name: state\n"

	tests := []struct {
		name         string
		equivalent   bool
		err          error
		expectedPlan types.String
	}{
		{
			name:         "equivalent documents",
			equivalent:   true,
			expectedPlan: types.StringValue(state),
		},
		{
			name:         "different documents",
			equivalent:   false,
			expectedPlan: types.StringValue(config),
		},
		{
			name:         "comparison error",
			equivalent:   true,
			err:          errors.New("comparison failed"),
			expectedPlan: types.StringValue(config),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotA, gotB string
			modifier := YAMLSemanticEqualComparing(func(yamlA, yamlB string, _ []string) (bool, error) {
				gotA, gotB = yamlA, yamlB
				return tt.equivalent, tt.err
			})

			plan := runModifier(modifier, config, state)

			assert.Equal(t, tt.expectedPlan, plan)
			assert.Equal(t, config, gotA, "first document passed to the comparison")
			assert.Equal(t, state, gotB, "second document passed to the comparison")
		})
	}
}

func TestYAMLSemanticEqualComparing_ConditionallyIgnoredFields(t *testing.T) {
	var gotIgnored []string
	modifier := YAMLSemanticEqualComparing(func(_, _ string, additionalIgnoredFields []string) (bool, error) {
		gotIgnored = additionalIgnoredFields
		return true, nil
	})

	runModifier(modifier, configOmittingPermissions, stateWithPermissions)

	assert.Equal(t, converter.FieldsAbsentFromYAML(configOmittingPermissions, converter.ConditionallyIgnoredFields), gotIgnored)
}
