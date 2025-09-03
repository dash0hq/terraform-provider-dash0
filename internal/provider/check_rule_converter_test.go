package provider

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

//go:embed testdata/check_rule_prom.yaml
var promRuleRaw string

//go:embed testdata/check_rule_dash0.json
var dash0RuleRaw string

func TestConvertCheckRule(t *testing.T) {
	dash0Rule, err := convertPromYAMLToDash0CheckRule(promRuleRaw, "default")
	assert.NotNil(t, dash0Rule)
	assert.NoError(t, err)

	jsonRaw, err := json.Marshal(dash0Rule)
	assert.NoError(t, err)
	assert.JSONEq(t, dash0RuleRaw, string(jsonRaw))
}

func TestConvertToPrometheusRule(t *testing.T) {
	promRules, err := convertDash0JSONtoPrometheusRules(dash0RuleRaw)
	assert.NotNil(t, promRules)
	assert.NoError(t, err)

	yamlRaw, err := yaml.Marshal(promRules)
	assert.NoError(t, err)
	assert.YAMLEq(t, promRuleRaw, string(yamlRaw))
}
