package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNormalizeDashboardYAML(t *testing.T) {
	// Test dashboard with all the fields we want to ignore
	yamlWithMetadata := `
kind: Dashboard
metadata:
  name: test-dashboard
  createdAt: "2023-01-01T00:00:00Z"
  updatedAt: "2023-01-02T00:00:00Z"
  version: 3
  dash0Extensions:
    projectId: project123
spec:
  title: Test Dashboard
  description: A test dashboard
`

	// Expected YAML after normalization
	expectedNormalizedYAML := `metadata:
    name: test-dashboard
spec:
    description: A test dashboard
    title: Test Dashboard
`

	// Normalize the YAML
	normalizedYAML, err := NormalizeDashboardYAML(yamlWithMetadata)
	require.NoError(t, err)

	// Parse both to compare structure rather than exact string
	var parsed, expected map[string]interface{}
	err = yaml.Unmarshal([]byte(normalizedYAML), &parsed)
	require.NoError(t, err)
	err = yaml.Unmarshal([]byte(expectedNormalizedYAML), &expected)
	require.NoError(t, err)

	// Make sure the ignored fields are removed
	assert.NotContains(t, parsed, "kind")
	assert.NotContains(t, parsed["metadata"].(map[string]interface{}), "createdAt")
	assert.NotContains(t, parsed["metadata"].(map[string]interface{}), "updatedAt")
	assert.NotContains(t, parsed["metadata"].(map[string]interface{}), "version")
	assert.NotContains(t, parsed["metadata"].(map[string]interface{}), "dash0Extensions")

	// Make sure other fields are preserved
	assert.Contains(t, parsed["metadata"].(map[string]interface{}), "name")
	assert.Contains(t, parsed["spec"].(map[string]interface{}), "title")
	assert.Contains(t, parsed["spec"].(map[string]interface{}), "description")
}

func TestDashboardsEquivalent(t *testing.T) {
	// Base dashboard
	baseDashboard := `
kind: Dashboard
metadata:
  name: test-dashboard
  createdAt: "2023-01-01T00:00:00Z"
  updatedAt: "2023-01-02T00:00:00Z"
  version: 3
  dash0Extensions:
    projectId: project123
spec:
  title: Test Dashboard
  description: A test dashboard
`

	// Same dashboard with different metadata values (should be equivalent)
	equivalentDashboard := `
kind: AnotherKind
metadata:
  name: test-dashboard
  createdAt: "2023-02-01T00:00:00Z"
  updatedAt: "2023-02-02T00:00:00Z"
  version: 5
  dash0Extensions:
    projectId: project456
spec:
  title: Test Dashboard
  description: A test dashboard
`

	// Dashboard with actual content differences (should not be equivalent)
	differentDashboard := `
kind: Dashboard
metadata:
  name: test-dashboard
  createdAt: "2023-01-01T00:00:00Z"
  updatedAt: "2023-01-02T00:00:00Z"
  version: 3
spec:
  title: Changed Title
  description: A test dashboard
`

	// Test equivalent dashboards
	equivalent, err := DashboardsEquivalent(baseDashboard, equivalentDashboard)
	require.NoError(t, err)
	assert.True(t, equivalent, "Dashboards should be equivalent")

	// Test non-equivalent dashboards
	equivalent, err = DashboardsEquivalent(baseDashboard, differentDashboard)
	require.NoError(t, err)
	assert.False(t, equivalent, "Dashboards should not be equivalent")

	// Test with invalid YAML
	_, err = DashboardsEquivalent(baseDashboard, "invalid: : yaml")
	assert.Error(t, err)
}