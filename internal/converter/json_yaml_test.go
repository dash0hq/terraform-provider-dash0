package converter

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConvertAPIResponseToYAML(t *testing.T) {
	tests := []struct {
		name          string
		apiResponse   string
		referenceYAML string
		expected      string
	}{
		{
			// Regression for #170: storing the API's JSON verbatim made a
			// changed label render as a full-document replacement.
			name:        "renders JSON as block YAML",
			apiResponse: `{"kind":"View","metadata":{"name":"web"},"spec":{"title":"Web"}}`,
			expected: `kind: View
metadata:
  name: web
spec:
  title: Web
`,
		},
		{
			name: "inherits the key order of the reference",
			apiResponse: `{"spec":{"title":"Web overview","filter":"a"},` +
				`"metadata":{"annotations":{"dash0.com/folder-path":"/team"},"name":"web"},` +
				`"kind":"View","apiVersion":"dash0.com/v1alpha1"}`,
			referenceYAML: `kind: View
metadata:
  name: web
  annotations:
    dash0.com/folder-path: /team
spec:
  filter: a
  title: Web
`,
			expected: `kind: View
metadata:
  name: web
  annotations:
    dash0.com/folder-path: /team
spec:
  filter: a
  title: Web overview
apiVersion: dash0.com/v1alpha1
`,
		},
		{
			name:        "keys the reference does not carry sort last, in API order",
			apiResponse: `{"version":3,"kind":"View","createdAt":"2026-01-15T10:00:00Z","metadata":{"name":"web"}}`,
			referenceYAML: `kind: View
metadata:
  name: web
`,
			expected: `kind: View
metadata:
  name: web
version: 3
createdAt: "2026-01-15T10:00:00Z"
`,
		},
		{
			name:        "aligns sequence elements by position",
			apiResponse: `{"spec":{"groups":[{"interval":"1m0s","name":"TestGroup","rules":[{"expr":"up","record":"m"}]}]}}`,
			referenceYAML: `spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - record: m
          expr: up
`,
			expected: `spec:
  groups:
    - name: TestGroup
      interval: 1m0s
      rules:
        - record: m
          expr: up
`,
		},
		{
			// The documented limitation: with the head element gone, "b" aligns
			// against the reference's "a" and loses its quoting.
			name:        "aligns a shortened sequence by position",
			apiResponse: `{"spec":{"actions":["b","c"]}}`,
			referenceYAML: `spec:
  actions:
    - "a"
    - "b"
    - "c"
`,
			expected: `spec:
  actions:
    - b
    - c
`,
		},
		{
			name:        "handles a sequence longer than the reference",
			apiResponse: `{"spec":{"actions":["a","b","c"]}}`,
			referenceYAML: `spec:
  actions:
    - "a"
`,
			expected: `spec:
  actions:
    - "a"
    - b
    - c
`,
		},
		{
			name:        "keeps the reference spelling for an unchanged scalar",
			apiResponse: `{"spec":{"actions":["views:read","views:delete"],"title":"Web overview"}}`,
			referenceYAML: `spec:
  actions:
    - "views:read"
    - "views:delete"
  title: "Web"
`,
			expected: `spec:
  actions:
    - "views:read"
    - "views:delete"
  title: Web overview
`,
		},
		{
			name:        "keeps a numeric-looking string quoted and a number plain",
			apiResponse: `{"version":"3","threshold":3,"ratio":0.5,"epochMillis":1758000000000}`,
			expected: `version: "3"
threshold: 3
ratio: 0.5
epochMillis: 1758000000000
`,
		},
		{
			name:        "renders a multi-line string as a literal block",
			apiResponse: `{"expr":"sum(rate(x[5m]))\n/ sum(rate(y[5m]))"}`,
			expected: `expr: |-
  sum(rate(x[5m]))
  / sum(rate(y[5m]))
`,
		},
		{
			name:        "keeps an empty string, a null, and an empty collection",
			apiResponse: `{"note":"","owner":null,"assets":[],"config":{}}`,
			expected: `note: ""
owner: null
assets: []
config: {}
`,
		},
		{
			name: "accepts a response that is already YAML",
			apiResponse: `kind: View
spec:
  title: Web
`,
			referenceYAML: `spec:
  title: Web
kind: View
`,
			expected: `spec:
  title: Web
kind: View
`,
		},
		{
			name:          "falls back to the API key order when the reference does not parse",
			apiResponse:   `{"kind":"View","spec":{"title":"Web"}}`,
			referenceYAML: "invalid: : yaml: that: will: fail",
			expected: `kind: View
spec:
  title: Web
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ConvertAPIResponseToYAML(tc.apiResponse, tc.referenceYAML)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestConvertAPIResponseToYAMLRejectsUnreadableResponses(t *testing.T) {
	tests := []struct {
		name        string
		apiResponse string
	}{
		{name: "unparseable document", apiResponse: "invalid: : yaml: that: will: fail"},
		{name: "empty document", apiResponse: ""},
		{name: "whitespace only", apiResponse: "   \n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConvertAPIResponseToYAML(tc.apiResponse, "kind: View\n")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "error parsing API response")
		})
	}
}

// Drift no apply can settle, if this ever stops holding.
func TestConvertAPIResponseToYAMLPreservesEquivalence(t *testing.T) {
	apiResponse := `{"kind":"View","metadata":{"labels":{"dash0.com/id":"abc"},"name":"web"},` +
		`"spec":{"enabled":true,"retries":0,"title":"Web","thresholds":[1,2.5]}}`

	converted, err := ConvertAPIResponseToYAML(apiResponse, "spec:\n  title: Web\nkind: View\n")
	require.NoError(t, err)

	equivalent, err := ResourceYAMLEquivalent(apiResponse, converted, nil, nil)
	require.NoError(t, err)
	assert.True(t, equivalent, "converted YAML must stay equivalent to the API response: %s", converted)
}

// Legacy state holds raw JSON, where every scalar is double-quoted. Inheriting
// that stuck, since the stored value is the next refresh's reference.
func TestConvertAPIResponseToYAMLIgnoresTheSpellingOfAJSONReference(t *testing.T) {
	jsonState := `{"kind":"Dash0View","metadata":{"name":"web"},"spec":{"title":"Old","type":"logs"}}`
	apiResponse := `{"kind":"Dash0View","metadata":{"name":"web"},"spec":{"title":"New","type":"logs"}}`

	first, err := ConvertAPIResponseToYAML(apiResponse, jsonState)
	require.NoError(t, err)
	assert.Equal(t, "kind: Dash0View\nmetadata:\n  name: web\nspec:\n  title: New\n  type: logs\n", first)

	second, err := ConvertAPIResponseToYAML(apiResponse, first)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

// No in-contract input is known to reach the guard that uses this: keys spelled
// `true`, `null`, `- x`, `#c`, and number-, date- or anchor-looking values are
// all quoted correctly. The guard is for future changes, so test the predicate.
func TestSameDocument(t *testing.T) {
	tests := []struct {
		name  string
		a, b  string
		equal bool
	}{
		{name: "identical", a: "a: 1\n", b: "a: 1\n", equal: true},
		{name: "same data, different spelling", a: `{"a":1}`, b: "a: 1\n", equal: true},
		{name: "same data, different key order", a: "a: 1\nb: 2\n", b: "b: 2\na: 1\n", equal: true},
		{name: "same data, different sequence indent", a: "a:\n  - 1\n", b: "a:\n- 1\n", equal: true},
		{name: "a dropped field", a: "a: 1\nb: 2\n", b: "a: 1\n", equal: false},
		{name: "a changed value", a: "a: 1\n", b: "a: 2\n", equal: false},
		{name: "a quoted number is not the number", a: `{"a":"1"}`, b: "a: 1\n", equal: false},
		{name: "reordered sequence elements", a: "a: [1, 2]\n", b: "a: [2, 1]\n", equal: false},
		{name: "left side does not parse", a: "invalid: : yaml", b: "a: 1\n", equal: false},
		{name: "right side does not parse", a: "a: 1\n", b: "invalid: : yaml", equal: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.equal, sameDocument(tc.a, tc.b))
		})
	}
}

// The invariant the conversion rests on: the stored value carries the same data
// the API returned. Order, quoting, and indentation may change; nothing else.
// Otherwise a bug in the key walk or indent rewrite changes the next apply.
func FuzzConvertAPIResponseToYAML(f *testing.F) {
	seeds := []struct {
		apiResponse string
		reference   string
	}{
		{`{"kind":"View","spec":{"title":"a"}}`, "kind: View\nspec:\n  title: b\n"},
		{`{"spec":{"filter":[{"key":"a"},{"key":"b"}]}}`, "spec:\n  filter:\n  - key: a\n"},
		{`{"spec":{"filter":[{"key":"a"}]}}`, "spec:\n  filter:\n    - key: a\n"},
		{`{"spec":{"groups":[{"name":"g","rules":[{"expr":"up","record":"r"}]}]}}`, "spec:\n  groups:\n  - name: g\n    rules:\n    - record: r\n      expr: up\n"},
		{`{"spec":{"routing":{"filters":[[{"key":"team"}]]}}}`, "spec:\n  routing:\n    filters:\n    - - key: team\n"},
		{`{"a":"","b":null,"c":[],"d":{},"e":5,"f":1.5,"g":"5","h":true}`, "a: \"\"\nb: null\n"},
		{`{"expr":"sum(rate(x[5m]))\n- not an item"}`, "expr: |-\n  old\n  text\n"},
		{`{"n":1758000000000,"s":"yes","t":"2026-01-15T10:00:00Z"}`, "n: 1\ns: no\n"},
		{`{"deep":{"a":{"b":{"c":[{"d":[1,2]}]}}}}`, "deep:\n  a:\n    b:\n      c:\n      - d:\n        - 9\n"},
	}
	for _, s := range seeds {
		f.Add(s.apiResponse, s.reference)
	}

	f.Fuzz(func(t *testing.T, apiResponse, reference string) {
		// Only valid JSON is in contract: the wrappers use json.Marshal. Wider
		// inputs report YAML-only cases the API cannot produce.
		if !json.Valid([]byte(apiResponse)) {
			return
		}

		// One parser on both sides, so this compares data, not integer spelling.
		var want interface{}
		if err := yaml.Unmarshal([]byte(apiResponse), &want); err != nil {
			return
		}

		got, err := ConvertAPIResponseToYAML(apiResponse, reference)
		if err != nil {
			return
		}

		var have interface{}
		if err := yaml.Unmarshal([]byte(got), &have); err != nil {
			t.Fatalf("output does not parse as YAML: %v\ninput: %q\nreference: %q\noutput: %q",
				err, apiResponse, reference, got)
		}

		if !reflect.DeepEqual(want, have) {
			t.Fatalf("conversion changed the data\ninput: %q\nreference: %q\noutput: %q\nwant: %#v\ngot:  %#v",
				apiResponse, reference, got, want, have)
		}
	})
}
