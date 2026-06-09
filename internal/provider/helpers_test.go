package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
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
