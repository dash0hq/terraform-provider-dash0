package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceUsesFlushSequences(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		flush     bool
	}{
		{
			name: "yamlencode style puts items flush with the key",
			reference: `"spec":
  "filter":
  - "key": a
`,
			flush: true,
		},
		{
			name: "encoder style indents items under the key",
			reference: `spec:
  filter:
    - key: a
`,
			flush: false,
		},
		{
			name:      "no block sequence to learn from",
			reference: "kind: View\nspec:\n  title: a\n",
			flush:     false,
		},
		{
			name:      "flow sequence teaches nothing",
			reference: "spec:\n  filter: [a, b]\n",
			flush:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.flush, referenceUsesFlushSequences(tc.reference))
		})
	}
}

func TestFlushSequences(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		expected string
		ok       bool
	}{
		{
			name: "single sequence under a key",
			encoded: `spec:
  filter:
    - key: a
      operator: is
`,
			expected: `spec:
  filter:
  - key: a
    operator: is
`,
			ok: true,
		},
		{
			name: "sequence nested inside a sequence item",
			encoded: `spec:
  groups:
    - name: g
      rules:
        - record: r
          expr: up
`,
			expected: `spec:
  groups:
  - name: g
    rules:
    - record: r
      expr: up
`,
			ok: true,
		},
		{
			name: "sibling items after a nested sequence closes",
			encoded: `spec:
  groups:
    - name: g
      rules:
        - record: r
    - name: h
`,
			expected: `spec:
  groups:
  - name: g
    rules:
    - record: r
  - name: h
`,
			ok: true,
		},
		{
			name: "a root sequence is already flush and must not move",
			encoded: `- name: a
  value: 1
- name: b
`,
			expected: `- name: a
  value: 1
- name: b
`,
			ok: true,
		},
		{
			// A dash inside a block scalar is not an item: no key above it.
			name: "block scalar content shifts with its owner",
			encoded: `spec:
  rules:
    - expr: |-
        sum(rate(x[5m]))
        - not a sequence item
      record: r
`,
			expected: `spec:
  rules:
  - expr: |-
      sum(rate(x[5m]))
      - not a sequence item
    record: r
`,
			ok: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flushed, ok := flushSequences(tc.encoded)
			require.Equal(t, tc.ok, ok)
			if !tc.ok {
				return
			}
			assert.Equal(t, tc.expected, flushed)
			assert.True(t, sameDocument(tc.encoded, flushed), "the rewrite must not change the data")
		})
	}
}

func TestConvertAPIResponseToYAMLMatchesReferenceSequenceIndent(t *testing.T) {
	// The shape reported on #170: a yamlencode state value, and an API
	// response whose permissions the platform rewrote.
	reference := `"kind": "Dash0View"
"metadata":
  "name": "request-logs"
"spec":
  "filter":
  - "key": "service.name"
    "operator": "is"
    "value": "checkout-service"
  "permissions":
  - "actions":
    - "views:read"
    - "views:write"
    "role": "basic_member"
  "type": "logs"
`

	apiResponse := `{"kind":"Dash0View","metadata":{"name":"request-logs"},"spec":{"filter":[{"key":"service.name","operator":"is","value":"checkout-service"}],"permissions":[{"actions":["views:read"],"role":"basic_member"}],"type":"logs"}}`

	got, err := ConvertAPIResponseToYAML(apiResponse, reference)
	require.NoError(t, err)

	expected := `"kind": "Dash0View"
"metadata":
  "name": "request-logs"
"spec":
  "filter":
  - "key": "service.name"
    "operator": "is"
    "value": "checkout-service"
  "permissions":
  - "actions":
    - "views:read"
    "role": "basic_member"
  "type": "logs"
`
	assert.Equal(t, expected, got)
}

func TestConvertAPIResponseToYAMLKeepsEncoderIndentWithoutAFlushReference(t *testing.T) {
	apiResponse := `{"spec":{"filter":[{"key":"a"}]}}`

	got, err := ConvertAPIResponseToYAML(apiResponse, "spec:\n  filter:\n    - key: a\n")
	require.NoError(t, err)
	assert.Equal(t, "spec:\n  filter:\n    - key: a\n", got)
}

// The inner mapping starts after both dashes, so shifting the whole subtree
// stays valid. This is notification channels' spec.routing.filters shape.
func TestConvertAPIResponseToYAMLFlushesListsOfLists(t *testing.T) {
	apiResponse := `{"spec":{"filters":[[{"key":"team","operator":"is"}]]}}`
	reference := `"spec":
  "filters":
  - - "key": "team"
      "operator": "is"
`

	got, err := ConvertAPIResponseToYAML(apiResponse, reference)
	require.NoError(t, err)
	assert.Equal(t, `"spec":
  "filters":
  - - "key": "team"
      "operator": "is"
`, got)
}
