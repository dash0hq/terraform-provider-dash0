package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertAPIResponseToYAML(t *testing.T) {
	tests := []struct {
		name          string
		apiResponse   string
		referenceYAML string
		expected      string
	}{
		{
			// Regression for https://github.com/dash0hq/terraform-provider-dash0/issues/170.
			// Storing the API's JSON verbatim made `terraform plan` render a
			// changed label as a full-document replacement.
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

// The result has to survive being fed back into drift detection: a refreshed
// state value that is not equivalent to the response it came from would make
// every subsequent plan report drift that no apply can settle.
func TestConvertAPIResponseToYAMLPreservesEquivalence(t *testing.T) {
	apiResponse := `{"kind":"View","metadata":{"labels":{"dash0.com/id":"abc"},"name":"web"},` +
		`"spec":{"enabled":true,"retries":0,"title":"Web","thresholds":[1,2.5]}}`

	converted, err := ConvertAPIResponseToYAML(apiResponse, "spec:\n  title: Web\nkind: View\n")
	require.NoError(t, err)

	equivalent, err := ResourceYAMLEquivalent(apiResponse, converted, nil, nil)
	require.NoError(t, err)
	assert.True(t, equivalent, "converted YAML must stay equivalent to the API response: %s", converted)
}
