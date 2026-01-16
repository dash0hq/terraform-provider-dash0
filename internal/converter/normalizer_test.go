package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name: "removes metadata fields",
			input: `
apiVersion: v1
kind: Dash0SyntheticCheck
metadata:
  name: examplecom
  createdAt: "2024-01-01T00:00:00Z"
  updatedAt: "2024-01-02T00:00:00Z"
  version: 1
  dash0Extensions:
    something: value
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`,
			expected: `spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com`,
			wantErr: false,
		},
		{
			name: "handles missing metadata fields",
			input: `
kind: Dash0SyntheticCheck
metadata:
  name: test
spec:
  enabled: false
`,
			expected: `spec:
  enabled: false`,
			wantErr: false,
		},
		{
			name: "handles complex structure",
			input: `
apiVersion: v1
kind: Dash0SyntheticCheck
metadata:
  name: complex
  createdAt: "2024-01-01T00:00:00Z"
  updatedAt: "2024-01-02T00:00:00Z"
  version: 2
spec:
  enabled: true
  notifications:
    channels:
      - id: channel1
      - id: channel2
  plugin:
    display:
      name: example.com
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
      request:
        method: get
        url: https://www.example.com
        headers:
          - key: User-Agent
            value: Mozilla/5.0
  retries:
    kind: fixed
    spec:
      attempts: 3
      delay: 1s
  schedule:
    interval: 1m
    locations:
      - gcp-europe-west3
`,
			expected: `spec:
  enabled: true
  notifications:
    channels:
      - id: channel1
      - id: channel2
  plugin:
    display:
      name: example.com
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              operator: is
              value: "200"
      request:
        headers:
          - key: User-Agent
            value: Mozilla/5.0
        method: get
        url: https://www.example.com
  retries:
    kind: fixed
    spec:
      attempts: 3
      delay: 1s
  schedule:
    interval: 1m
    locations:
      - gcp-europe-west3`,
			wantErr: false,
		},
		{
			name: "removes empty arrays and empty maps",
			input: `
kind: Dash0View
metadata:
  name: test
  annotations: {}
spec:
  display:
    name: Test View
    folder: []
  type: spans
`,
			expected: `spec:
  display:
    name: Test View
  type: spans`,
			wantErr: false,
		},
		{
			name:     "handles invalid YAML",
			input:    "invalid: : : yaml",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizeYAML(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestResourceYAMLEquivalent(t *testing.T) {
	tests := []struct {
		name       string
		yaml1      string
		yaml2      string
		equivalent bool
		wantErr    bool
	}{
		{
			name: "identical checks",
			yaml1: `
kind: Dash0SyntheticCheck
metadata:
  name: test
spec:
  enabled: true
`,
			yaml2: `
kind: Dash0SyntheticCheck
metadata:
  name: test
spec:
  enabled: true
`,
			equivalent: true,
			wantErr:    false,
		},
		{
			name: "equivalent checks with different metadata",
			yaml1: `
apiVersion: v1
kind: Dash0SyntheticCheck
metadata:
  name: test
  createdAt: "2024-01-01T00:00:00Z"
  updatedAt: "2024-01-01T00:00:00Z"
  version: 1
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`,
			yaml2: `
apiVersion: v2
kind: SomeOtherKind
metadata:
  name: test
  createdAt: "2024-02-02T00:00:00Z"
  updatedAt: "2024-02-02T00:00:00Z"
  version: 2
  dash0Extensions:
    extra: field
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`,
			equivalent: true,
			wantErr:    false,
		},
		{
			name: "different checks",
			yaml1: `
metadata:
  name: check1
spec:
  enabled: true
`,
			yaml2: `
metadata:
  name: check2
spec:
  enabled: false
`,
			equivalent: false,
			wantErr:    false,
		},
		{
			name: "different spec content",
			yaml1: `
metadata:
  name: test
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
`,
			yaml2: `
metadata:
  name: test
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.different.com
`,
			equivalent: false,
			wantErr:    false,
		},
		{
			name: "equivalent with different order",
			yaml1: `
metadata:
  name: test
spec:
  schedule:
    interval: 1m
    locations:
      - gcp-us-west1
      - gcp-europe-west3
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com
        method: get
`,
			yaml2: `
metadata:
  name: test
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        method: get
        url: https://www.example.com
  schedule:
    locations:
      - gcp-us-west1
      - gcp-europe-west3
    interval: 1m
`,
			equivalent: true,
			wantErr:    false,
		},
		{
			name:       "invalid YAML in first",
			yaml1:      "invalid: : : yaml",
			yaml2:      "metadata:\n  name: test",
			equivalent: false,
			wantErr:    true,
		},
		{
			name:       "invalid YAML in second",
			yaml1:      "metadata:\n  name: test",
			yaml2:      "invalid: : : yaml",
			equivalent: false,
			wantErr:    true,
		},
		{
			name: "ignore different order in slices",
			yaml1: `
kind: Dash0SyntheticCheck
spec:
  permissions:
    - actions:
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
`,
			yaml2: `
kind: Dash0SyntheticCheck
spec:
  permissions:
    - actions:
        - "views:read"
      role: "basic_member"
    - actions:
        - "views:delete"
        - "views:read"
      role: "admin"
`,
			equivalent: true,
			wantErr:    false,
		},
		{
			name: "equivalent with different annotation ordering and quoting styles",
			yaml1: `
spec:
  groups:
    - interval: 1m0s
      name: test-group
      rules:
        - alert: test-alert
          annotations:
            summary: "{{ $labels.reason }} event detected"
            description: "Events exceeded threshold"
            dash0-threshold-critical: "0"
            dash0-threshold-degraded: "0"
          labels:
            severity: critical
            team: "{{ $labels.team_name }}"
`,
			yaml2: `
spec:
  groups:
    - interval: 1m0s
      name: test-group
      rules:
        - alert: test-alert
          annotations:
            dash0-threshold-critical: "0"
            dash0-threshold-degraded: "0"
            description: Events exceeded threshold
            summary: '{{ $labels.reason }} event detected'
          labels:
            severity: "critical"
            team: '{{ $labels.team_name }}'
`,
			equivalent: true,
			wantErr:    false,
		},
		{
			name: "equivalent when one has empty arrays and other omits them",
			yaml1: `
kind: Dash0View
metadata:
  name: test
  annotations: {}
spec:
  display:
    name: Test View
    folder: []
  type: spans
`,
			yaml2: `
kind: Dash0View
metadata:
  name: test
spec:
  display:
    name: Test View
  type: spans
`,
			equivalent: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResourceYAMLEquivalent(tt.yaml1, tt.yaml2)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.equivalent, result)
			}
		})
	}
}

func TestRemoveYAMLField(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		path     string
		expected map[string]interface{}
	}{
		{
			name: "remove top-level field",
			input: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Dash0SyntheticCheck",
				"metadata":   map[string]interface{}{"name": "test"},
			},
			path: "apiVersion",
			expected: map[string]interface{}{
				"kind":     "Dash0SyntheticCheck",
				"metadata": map[string]interface{}{"name": "test"},
			},
		},
		{
			name: "remove nested field",
			input: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test",
					"createdAt": "2024-01-01",
					"updatedAt": "2024-01-02",
				},
			},
			path: "metadata.createdAt",
			expected: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test",
					"updatedAt": "2024-01-02",
				},
			},
		},
		{
			name: "path doesn't exist",
			input: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test",
				},
			},
			path: "metadata.nonexistent",
			expected: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "test",
				},
			},
		},
		{
			name: "intermediate path doesn't exist",
			input: map[string]interface{}{
				"spec": map[string]interface{}{
					"enabled": true,
				},
			},
			path: "metadata.createdAt",
			expected: map[string]interface{}{
				"spec": map[string]interface{}{
					"enabled": true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanupMap(tt.input, []string{tt.path})
			assert.Equal(t, tt.expected, tt.input)
		})
	}
}
