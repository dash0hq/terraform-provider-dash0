package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
)

func TestStringOrNull(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected types.String
	}{
		{
			name:     "empty string yields null",
			input:    "",
			expected: types.StringNull(),
		},
		{
			name:     "non-empty string yields value",
			input:    "abc",
			expected: types.StringValue("abc"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, stringOrNull(tc.input))
		})
	}
}

func TestRefreshedYAML(t *testing.T) {
	t.Run("renders the response as YAML ordered like the prior value", func(t *testing.T) {
		refreshed := refreshedYAML(
			`{"spec":{"title":"Updated"},"kind":"View","metadata":{"name":"web"}}`,
			"kind: View\nmetadata:\n  name: web\nspec:\n  title: Original\n",
		)
		assert.Equal(t, "kind: View\nmetadata:\n  name: web\nspec:\n  title: Updated\n", refreshed)
	})

	t.Run("returns the response unchanged when it does not parse", func(t *testing.T) {
		assert.Equal(t, "invalid: : yaml", refreshedYAML("invalid: : yaml", "kind: View\n"))
	})
}

// assertYAMLStateRefreshed asserts that a refreshed `*_yaml` state value carries
// the document that was read back, rendered as block YAML rather than as the
// JSON string the client wrappers produce. The exact rendering is pinned by the
// converter's own tests; this only checks the contract every Read has to honor.
//
// A response that does not parse is stored as it arrived, so for that input the
// assertion is pass-through.
func assertYAMLStateRefreshed(t *testing.T, apiResponse, stateValue string) {
	t.Helper()

	var parsed map[string]interface{}
	if yaml.Unmarshal([]byte(apiResponse), &parsed) != nil {
		assert.Equal(t, apiResponse, stateValue)
		return
	}

	require.NotEmpty(t, stateValue)
	assert.False(t, strings.HasPrefix(strings.TrimSpace(stateValue), "{"),
		"state must hold block YAML, not a JSON string: %s", stateValue)

	equivalent, err := converter.ResourceYAMLEquivalent(apiResponse, stateValue, nil, nil)
	require.NoError(t, err)
	assert.True(t, equivalent, "state must carry the document that was read back, got: %s", stateValue)
}
