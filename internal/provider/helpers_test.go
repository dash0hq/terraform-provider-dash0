package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"

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
			context.Background(),
			`{"spec":{"title":"Updated"},"kind":"View","metadata":{"name":"web"}}`,
			"kind: View\nmetadata:\n  name: web\nspec:\n  title: Original\n",
		)
		assert.Equal(t, "kind: View\nmetadata:\n  name: web\nspec:\n  title: Updated\n", refreshed)
	})

	t.Run("returns the response unchanged when it does not parse", func(t *testing.T) {
		assert.Equal(t, "invalid: : yaml", refreshedYAML(context.Background(), "invalid: : yaml", "kind: View\n"))
	})
}

// Pins the Read wiring, not the rendering. Equivalence is too weak: a call site
// passing the wrong reference still stores an equivalent document with its key
// order scrambled. Only exact equality catches that.
func assertYAMLStateRefreshed(t *testing.T, apiResponse, priorYAML, stateValue string) {
	t.Helper()

	expected, err := converter.ConvertAPIResponseToYAML(apiResponse, priorYAML)
	if err != nil {
		// An unrenderable response goes into state as it arrived.
		assert.Equal(t, apiResponse, stateValue)
		return
	}

	assert.Equal(t, expected, stateValue)
	assert.False(t, strings.HasPrefix(strings.TrimSpace(stateValue), "{"),
		"state must hold block YAML, not a JSON string: %s", stateValue)
}
