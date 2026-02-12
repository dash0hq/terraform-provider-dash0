package converter

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

//go:embed testdata/check_rule_prom.yaml
var promRuleRaw string

//go:embed testdata/check_rule_dash0.json
var dash0RuleRaw string

func TestConvertCheckRule(t *testing.T) {
	dash0Rule, err := ConvertPromYAMLToDash0CheckRule(promRuleRaw, "default")
	assert.NotNil(t, dash0Rule)
	assert.NoError(t, err)

	jsonRaw, err := json.Marshal(dash0Rule)
	assert.NoError(t, err)
	assert.JSONEq(t, dash0RuleRaw, string(jsonRaw))
}

func TestConvertToPrometheusRule(t *testing.T) {
	promRules, err := ConvertDash0JSONtoPrometheusRules(dash0RuleRaw)
	assert.NotNil(t, promRules)
	assert.NoError(t, err)

	yamlRaw, err := yaml.Marshal(promRules)
	assert.NoError(t, err)
	assert.YAMLEq(t, promRuleRaw, string(yamlRaw))
}

func TestConvertPromYAMLToDash0CheckRule_FloatThresholds(t *testing.T) {
	tests := []struct {
		name              string
		thresholdCrit     string
		thresholdDegraded string
		expectedFailed    float64
		expectedDegraded  float64
	}{
		{
			name:              "integer thresholds",
			thresholdCrit:     "100",
			thresholdDegraded: "50",
			expectedFailed:    100,
			expectedDegraded:  50,
		},
		{
			name:              "float thresholds with decimals",
			thresholdCrit:     "99.99",
			thresholdDegraded: "50.5",
			expectedFailed:    99.99,
			expectedDegraded:  50.5,
		},
		{
			name:              "small decimal thresholds",
			thresholdCrit:     "0.001",
			thresholdDegraded: "0.0001",
			expectedFailed:    0.001,
			expectedDegraded:  0.0001,
		},
		{
			name:              "large thresholds",
			thresholdCrit:     "99999.99",
			thresholdDegraded: "10000.01",
			expectedFailed:    99999.99,
			expectedDegraded:  10000.01,
		},
		{
			name:              "zero thresholds",
			thresholdCrit:     "0",
			thresholdDegraded: "0",
			expectedFailed:    0,
			expectedDegraded:  0,
		},
		{
			name:              "precise decimal thresholds",
			thresholdCrit:     "0.123456789",
			thresholdDegraded: "0.987654321",
			expectedFailed:    0.123456789,
			expectedDegraded:  0.987654321,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlInput := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: test_metric > $__threshold
          for: 5m
          annotations:
            summary: Test alert
            dash0-threshold-critical: "` + tc.thresholdCrit + `"
            dash0-threshold-degraded: "` + tc.thresholdDegraded + `"
          labels:
            severity: warning`

			dash0Rule, err := ConvertPromYAMLToDash0CheckRule(yamlInput, "default")
			assert.NoError(t, err)
			assert.NotNil(t, dash0Rule)
			assert.Equal(t, tc.expectedFailed, dash0Rule.Thresholds.Failed)
			assert.Equal(t, tc.expectedDegraded, dash0Rule.Thresholds.Degraded)
		})
	}
}

func TestConvertPromYAMLToDash0CheckRule_InvalidThresholds(t *testing.T) {
	tests := []struct {
		name          string
		thresholdCrit string
		thresholdDeg  string
		expectedError string
	}{
		{
			name:          "invalid critical threshold - text",
			thresholdCrit: "abc",
			thresholdDeg:  "50",
			expectedError: "invalid value for dash0-threshold-critical",
		},
		{
			name:          "invalid degraded threshold - text",
			thresholdCrit: "100",
			thresholdDeg:  "xyz",
			expectedError: "invalid value for dash0-threshold-degraded",
		},
		{
			name:          "invalid critical threshold - multiple dots",
			thresholdCrit: "1.2.3",
			thresholdDeg:  "50",
			expectedError: "invalid value for dash0-threshold-critical",
		},
		{
			name:          "invalid degraded threshold - multiple dots",
			thresholdCrit: "100",
			thresholdDeg:  "4.5.6",
			expectedError: "invalid value for dash0-threshold-degraded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlInput := `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - alert: TestAlert
          expr: test_metric > $__threshold
          for: 5m
          annotations:
            summary: Test alert
            dash0-threshold-critical: "` + tc.thresholdCrit + `"
            dash0-threshold-degraded: "` + tc.thresholdDeg + `"
          labels:
            severity: warning`

			dash0Rule, err := ConvertPromYAMLToDash0CheckRule(yamlInput, "default")
			assert.Error(t, err)
			assert.Nil(t, dash0Rule)
			assert.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

func TestConvertDash0JSONtoPrometheusRules_FloatThresholds(t *testing.T) {
	tests := []struct {
		name             string
		degraded         float64
		failed           float64
		expectedCrit     string
		expectedDegraded string
	}{
		{
			name:             "integer-like floats",
			degraded:         50,
			failed:           100,
			expectedCrit:     "100",
			expectedDegraded: "50",
		},
		{
			name:             "float with decimals",
			degraded:         50.5,
			failed:           99.99,
			expectedCrit:     "99.99",
			expectedDegraded: "50.5",
		},
		{
			name:             "small decimals",
			degraded:         0.001,
			failed:           0.999,
			expectedCrit:     "0.999",
			expectedDegraded: "0.001",
		},
		{
			name:             "precise decimals",
			degraded:         0.123456789,
			failed:           0.987654321,
			expectedCrit:     "0.987654321",
			expectedDegraded: "0.123456789",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonInput := `{
				"dataset": "default",
				"name": "TestGroup - TestAlert",
				"expression": "test_metric > $__threshold",
				"thresholds": {
					"degraded": ` + formatFloat(tc.degraded) + `,
					"failed": ` + formatFloat(tc.failed) + `
				},
				"summary": "Test alert",
				"description": "",
				"interval": "1m0s",
				"for": "5m0s",
				"keepFiringFor": "0s",
				"labels": {},
				"annotations": {},
				"enabled": true
			}`

			promRules, err := ConvertDash0JSONtoPrometheusRules(jsonInput)
			assert.NoError(t, err)
			assert.NotNil(t, promRules)

			rule := promRules.Spec.Groups[0].Rules[0]
			assert.Equal(t, tc.expectedCrit, rule.Annotations["dash0-threshold-critical"])
			assert.Equal(t, tc.expectedDegraded, rule.Annotations["dash0-threshold-degraded"])
		})
	}
}

func TestConvertDash0JSONtoPrometheusRules_ZeroThresholds(t *testing.T) {
	jsonInput := `{
		"dataset": "default",
		"name": "TestGroup - TestAlert",
		"expression": "test_metric > 0",
		"thresholds": {
			"degraded": 0,
			"failed": 0
		},
		"summary": "Test alert",
		"description": "",
		"interval": "1m0s",
		"for": "5m0s",
		"keepFiringFor": "0s",
		"labels": {},
		"annotations": {},
		"enabled": true
	}`

	promRules, err := ConvertDash0JSONtoPrometheusRules(jsonInput)
	assert.NoError(t, err)
	assert.NotNil(t, promRules)

	rule := promRules.Spec.Groups[0].Rules[0]
	// Zero thresholds should NOT be in annotations (they're treated as default/absent)
	// The normalizer handles zero-value threshold annotations as semantically equivalent
	_, hasCritical := rule.Annotations["dash0-threshold-critical"]
	_, hasDegraded := rule.Annotations["dash0-threshold-degraded"]
	assert.False(t, hasCritical, "zero threshold-critical should not be in annotations")
	assert.False(t, hasDegraded, "zero threshold-degraded should not be in annotations")
}

// TestCheckRuleRoundTripEquivalence verifies that user YAML survives the full
// round-trip (YAML → Dash0 JSON → Prometheus YAML) and is still considered
// equivalent by the normalizer despite formatting differences.
func TestCheckRuleRoundTripEquivalence(t *testing.T) {
	tests := []struct {
		name     string
		userYAML string
	}{
		{
			name: "short-form durations (2m vs 2m0s)",
			userYAML: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: TestGroup
      interval: 1m
      rules:
        - alert: TestAlert
          expr: test_metric > $__threshold
          for: 2m
          keep_firing_for: 30s
          annotations:
            summary: Test alert
            dash0-threshold-critical: "5000"
            dash0-threshold-degraded: "1000"
          labels:
            severity: warning`,
		},
		{
			name: "keep_firing_for: 0s (dropped by omitempty)",
			userYAML: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: TestGroup
      interval: 1m
      rules:
        - alert: TestAlert
          expr: test_metric > $__threshold
          for: 2m
          keep_firing_for: 0s
          annotations:
            summary: Test alert
            dash0-threshold-critical: "5000"
            dash0-threshold-degraded: "1000"
          labels:
            severity: warning`,
		},
		{
			name: "unquoted numeric values in annotations and labels",
			userYAML: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: TestGroup
      interval: 1m
      rules:
        - alert: TestAlert
          expr: test_metric > $__threshold
          for: 2m
          annotations:
            summary: Test alert
            dash0-threshold-critical: 5000
            dash0-threshold-degraded: "1000"
          labels:
            severity: warning
            port: 8080`,
		},
		{
			name: "mixed formatting issues (durations + unquoted numbers + defaults + zero-duration)",
			userYAML: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata: {}
spec:
  groups:
    - name: TestGroup
      interval: 2m
      rules:
        - alert: TestAlert
          expr: test_metric > $__threshold
          for: 5m
          keep_firing_for: 0s
          annotations:
            summary: Test alert
            dash0-threshold-critical: 5000
            dash0-enabled: "true"
          labels:
            severity: warning
            port: 8080`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dash0Rule, err := ConvertPromYAMLToDash0CheckRule(tc.userYAML, "default")
			assert.NoError(t, err)

			jsonBytes, err := json.Marshal(dash0Rule)
			assert.NoError(t, err)

			promRules, err := ConvertDash0JSONtoPrometheusRules(string(jsonBytes))
			assert.NoError(t, err)

			roundTrippedYAMLBytes, err := yaml.Marshal(promRules)
			assert.NoError(t, err)

			equivalent, err := ResourceYAMLEquivalent(tc.userYAML, string(roundTrippedYAMLBytes))
			assert.NoError(t, err)
			assert.True(t, equivalent, "user YAML and round-tripped YAML should be equivalent")
		})
	}
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
