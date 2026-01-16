package planmodifier

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

func TestYAMLSemanticEqual_Description(t *testing.T) {
	modifier := YAMLSemanticEqual()
	assert.Equal(t, "Preserves state when YAML values are semantically equivalent", modifier.Description(context.Background()))
	assert.Equal(t, "Preserves state when YAML values are semantically equivalent", modifier.MarkdownDescription(context.Background()))
}

func TestYAMLSemanticEqual_PlanModifyString(t *testing.T) {
	tests := []struct {
		name         string
		configValue  types.String
		stateValue   types.String
		expectedPlan types.String
		description  string
	}{
		{
			name: "different key ordering - should use state",
			configValue: types.StringValue(`
spec:
  annotations:
    summary: test
    description: desc
`),
			stateValue: types.StringValue(`
spec:
  annotations:
    description: desc
    summary: test
`),
			expectedPlan: types.StringValue(`
spec:
  annotations:
    description: desc
    summary: test
`),
			description: "Should use state value when key ordering differs",
		},
		{
			name: "different quoting styles - should use state",
			configValue: types.StringValue(`
spec:
  labels:
    severity: critical
`),
			stateValue: types.StringValue(`
spec:
  labels:
    severity: "critical"
`),
			expectedPlan: types.StringValue(`
spec:
  labels:
    severity: "critical"
`),
			description: "Should use state value when quoting styles differ",
		},
		{
			name: "actual content difference - should use config",
			configValue: types.StringValue(`
spec:
  labels:
    severity: critical
`),
			stateValue: types.StringValue(`
spec:
  labels:
    severity: warning
`),
			expectedPlan: types.StringValue(`
spec:
  labels:
    severity: critical
`),
			description: "Should use config value when content actually differs",
		},
		{
			name:         "null config - no modification",
			configValue:  types.StringNull(),
			stateValue:   types.StringValue("spec: {}"),
			expectedPlan: types.StringNull(),
			description:  "Should not modify when config is null",
		},
		{
			name:         "unknown config - no modification",
			configValue:  types.StringUnknown(),
			stateValue:   types.StringValue("spec: {}"),
			expectedPlan: types.StringUnknown(),
			description:  "Should not modify when config is unknown",
		},
		{
			name:         "null state (new resource) - use config",
			configValue:  types.StringValue("spec: {}"),
			stateValue:   types.StringNull(),
			expectedPlan: types.StringValue("spec: {}"),
			description:  "Should use config value for new resources",
		},
		{
			name:         "invalid YAML in config - use config (let Terraform handle)",
			configValue:  types.StringValue("invalid: : yaml"),
			stateValue:   types.StringValue("spec: {}"),
			expectedPlan: types.StringValue("invalid: : yaml"),
			description:  "Should use config value when YAML parsing fails",
		},
		{
			name: "complex nested structure with ordering differences - should use state",
			configValue: types.StringValue(`
spec:
  groups:
    - name: test
      rules:
        - alert: TestAlert
          annotations:
            summary: "{{ $labels.reason }}"
            dash0-threshold-critical: "0"
          labels:
            severity: critical
`),
			stateValue: types.StringValue(`
spec:
  groups:
    - name: test
      rules:
        - alert: TestAlert
          annotations:
            dash0-threshold-critical: "0"
            summary: '{{ $labels.reason }}'
          labels:
            severity: "critical"
`),
			expectedPlan: types.StringValue(`
spec:
  groups:
    - name: test
      rules:
        - alert: TestAlert
          annotations:
            dash0-threshold-critical: "0"
            summary: '{{ $labels.reason }}'
          labels:
            severity: "critical"
`),
			description: "Should use state value for complex nested structures with formatting differences",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := YAMLSemanticEqual()

			req := planmodifier.StringRequest{
				ConfigValue: tt.configValue,
				StateValue:  tt.stateValue,
				PlanValue:   tt.configValue, // Plan starts as config value
			}
			resp := &planmodifier.StringResponse{
				PlanValue: tt.configValue, // Initialize with config value
			}

			modifier.PlanModifyString(context.Background(), req, resp)

			assert.Equal(t, tt.expectedPlan, resp.PlanValue, tt.description)
		})
	}
}
