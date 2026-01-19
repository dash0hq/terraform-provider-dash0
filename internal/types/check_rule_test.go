package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDash0CheckRuleThresholds_JSONMarshal(t *testing.T) {
	tests := []struct {
		name       string
		thresholds Dash0CheckRuleThresholds
		expected   string
	}{
		{
			name: "integer values",
			thresholds: Dash0CheckRuleThresholds{
				Degraded: 35,
				Failed:   40,
			},
			expected: `{"degraded":35,"failed":40}`,
		},
		{
			name: "float values with decimals",
			thresholds: Dash0CheckRuleThresholds{
				Degraded: 35.5,
				Failed:   40.75,
			},
			expected: `{"degraded":35.5,"failed":40.75}`,
		},
		{
			name: "zero values",
			thresholds: Dash0CheckRuleThresholds{
				Degraded: 0,
				Failed:   0,
			},
			expected: `{"degraded":0,"failed":0}`,
		},
		{
			name: "small decimal values",
			thresholds: Dash0CheckRuleThresholds{
				Degraded: 0.001,
				Failed:   0.999,
			},
			expected: `{"degraded":0.001,"failed":0.999}`,
		},
		{
			name: "large values",
			thresholds: Dash0CheckRuleThresholds{
				Degraded: 99999.99,
				Failed:   100000.01,
			},
			expected: `{"degraded":99999.99,"failed":100000.01}`,
		},
		{
			name: "mixed integer and float",
			thresholds: Dash0CheckRuleThresholds{
				Degraded: 35,
				Failed:   40.5,
			},
			expected: `{"degraded":35,"failed":40.5}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.thresholds)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(jsonBytes))
		})
	}
}

func TestDash0CheckRuleThresholds_JSONUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		jsonStr  string
		expected Dash0CheckRuleThresholds
	}{
		{
			name:    "integer values",
			jsonStr: `{"degraded":35,"failed":40}`,
			expected: Dash0CheckRuleThresholds{
				Degraded: 35,
				Failed:   40,
			},
		},
		{
			name:    "float values with decimals",
			jsonStr: `{"degraded":35.5,"failed":40.75}`,
			expected: Dash0CheckRuleThresholds{
				Degraded: 35.5,
				Failed:   40.75,
			},
		},
		{
			name:    "zero values",
			jsonStr: `{"degraded":0,"failed":0}`,
			expected: Dash0CheckRuleThresholds{
				Degraded: 0,
				Failed:   0,
			},
		},
		{
			name:    "small decimal values",
			jsonStr: `{"degraded":0.001,"failed":0.999}`,
			expected: Dash0CheckRuleThresholds{
				Degraded: 0.001,
				Failed:   0.999,
			},
		},
		{
			name:    "large values",
			jsonStr: `{"degraded":99999.99,"failed":100000.01}`,
			expected: Dash0CheckRuleThresholds{
				Degraded: 99999.99,
				Failed:   100000.01,
			},
		},
		{
			name:    "scientific notation",
			jsonStr: `{"degraded":1e-3,"failed":1e3}`,
			expected: Dash0CheckRuleThresholds{
				Degraded: 0.001,
				Failed:   1000,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var thresholds Dash0CheckRuleThresholds
			err := json.Unmarshal([]byte(tc.jsonStr), &thresholds)
			require.NoError(t, err)
			assert.Equal(t, tc.expected.Degraded, thresholds.Degraded)
			assert.Equal(t, tc.expected.Failed, thresholds.Failed)
		})
	}
}

func TestDash0CheckRule_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		checkRule Dash0CheckRule
	}{
		{
			name: "with float thresholds",
			checkRule: Dash0CheckRule{
				Dataset:    "test-dataset",
				Name:       "Test Rule",
				Expression: "test_metric > $__threshold",
				Thresholds: Dash0CheckRuleThresholds{
					Degraded: 50.5,
					Failed:   75.25,
				},
				Summary:     "Test summary",
				Description: "Test description",
				Labels:      map[string]string{"severity": "warning"},
				Annotations: map[string]string{"runbook": "http://example.com"},
				Enabled:     true,
			},
		},
		{
			name: "with integer thresholds",
			checkRule: Dash0CheckRule{
				Dataset:    "test-dataset",
				Name:       "Test Rule",
				Expression: "test_metric > $__threshold",
				Thresholds: Dash0CheckRuleThresholds{
					Degraded: 50,
					Failed:   75,
				},
				Summary: "Test summary",
				Labels:  map[string]string{},
				Enabled: true,
			},
		},
		{
			name: "with zero thresholds",
			checkRule: Dash0CheckRule{
				Dataset:    "test-dataset",
				Name:       "Test Rule",
				Expression: "test_metric > 0",
				Thresholds: Dash0CheckRuleThresholds{
					Degraded: 0,
					Failed:   0,
				},
				Summary: "Test summary",
				Labels:  map[string]string{},
				Enabled: true,
			},
		},
		{
			name: "with precise decimal thresholds",
			checkRule: Dash0CheckRule{
				Dataset:    "test-dataset",
				Name:       "Test Rule",
				Expression: "test_metric > $__threshold",
				Thresholds: Dash0CheckRuleThresholds{
					Degraded: 0.123456,
					Failed:   99.987654,
				},
				Summary: "Test summary",
				Labels:  map[string]string{},
				Enabled: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal to JSON
			jsonBytes, err := json.Marshal(tc.checkRule)
			require.NoError(t, err)

			// Unmarshal back
			var result Dash0CheckRule
			err = json.Unmarshal(jsonBytes, &result)
			require.NoError(t, err)

			// Verify thresholds are preserved
			assert.Equal(t, tc.checkRule.Thresholds.Degraded, result.Thresholds.Degraded)
			assert.Equal(t, tc.checkRule.Thresholds.Failed, result.Thresholds.Failed)
			assert.Equal(t, tc.checkRule.Dataset, result.Dataset)
			assert.Equal(t, tc.checkRule.Name, result.Name)
			assert.Equal(t, tc.checkRule.Expression, result.Expression)
			assert.Equal(t, tc.checkRule.Enabled, result.Enabled)
		})
	}
}
