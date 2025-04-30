package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertYAMLToJSON(t *testing.T) {
	tests := []struct {
		name     string
		yamlStr  string
		expected map[string]interface{}
	}{
		{
			name: "simple yaml",
			yamlStr: `
kind: Dashboard
metadata:
  name: test
spec:
  title: Test Dashboard
`,
			expected: map[string]interface{}{
				"kind": "Dashboard",
				"metadata": map[string]interface{}{
					"name": "test",
				},
				"spec": map[string]interface{}{
					"title": "Test Dashboard",
				},
			},
		},
		{
			name: "complex yaml",
			yamlStr: `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: home
spec:
  duration: 30m
  display:
    name: Home
  layouts:
    - kind: Grid
      spec:
        items:
          - content:
              $ref: "#/spec/panels/panel1"
            height: 10
            width: 24
            x: 0
            y: 0
  panels:
    panel1:
      kind: Panel
      spec:
        display:
          name: Test Panel
`,
			expected: map[string]interface{}{
				"apiVersion": "perses.dev/v1alpha1",
				"kind":       "PersesDashboard",
				"metadata": map[string]interface{}{
					"name": "home",
				},
				"spec": map[string]interface{}{
					"duration": "30m",
					"display": map[string]interface{}{
						"name": "Home",
					},
					"layouts": []interface{}{
						map[string]interface{}{
							"kind": "Grid",
							"spec": map[string]interface{}{
								"items": []interface{}{
									map[string]interface{}{
										"content": map[string]interface{}{
											"$ref": "#/spec/panels/panel1",
										},
										"height": float64(10),
										"width":  float64(24),
										"x":      float64(0),
										"y":      float64(0),
									},
								},
							},
						},
					},
					"panels": map[string]interface{}{
						"panel1": map[string]interface{}{
							"kind": "Panel",
							"spec": map[string]interface{}{
								"display": map[string]interface{}{
									"name": "Test Panel",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Convert YAML to JSON
			jsonStr, err := ConvertYAMLToJSON(tc.yamlStr)
			require.NoError(t, err)
			
			// Parse the JSON string back into a map for comparison
			var result map[string]interface{}
			err = json.Unmarshal([]byte(jsonStr), &result)
			require.NoError(t, err)
			
			// Compare the result with the expected map
			assert.Equal(t, tc.expected, result)
		})
	}
	
	// Test invalid YAML
	t.Run("invalid yaml", func(t *testing.T) {
		_, err := ConvertYAMLToJSON("invalid: : : yaml")
		assert.Error(t, err)
	})
}