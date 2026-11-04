package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const spamFilterSpecYAML = `
spec:
  context: log
  filter:
    - key: body
      operator: contains
      value: spam
`

func spamFilterDoc(annotations string) string {
	if annotations == "" {
		return "apiVersion: v1alpha2\nkind: Dash0SpamFilter\nmetadata:\n  name: noisy" + spamFilterSpecYAML
	}
	return "apiVersion: v1alpha2\nkind: Dash0SpamFilter\nmetadata:\n  name: noisy\n  annotations:\n" + annotations + spamFilterSpecYAML
}

func TestSpamFilterYAMLEquivalent(t *testing.T) {
	tests := []struct {
		name           string
		annotationsA   string
		annotationsB   string
		wantEquivalent bool
	}{
		{
			name:           "both omit enabled",
			wantEquivalent: true,
		},
		{
			name:           "omitted enabled equals explicit true",
			annotationsB:   `    dash0.com/enabled: "true"`,
			wantEquivalent: true,
		},
		{
			name:           "true to false is a change",
			annotationsA:   `    dash0.com/enabled: "true"`,
			annotationsB:   `    dash0.com/enabled: "false"`,
			wantEquivalent: false,
		},
		{
			name:           "omitted enabled differs from false",
			annotationsB:   `    dash0.com/enabled: "false"`,
			wantEquivalent: false,
		},
		{
			name:           "both false",
			annotationsA:   `    dash0.com/enabled: "false"`,
			annotationsB:   `    dash0.com/enabled: "false"`,
			wantEquivalent: true,
		},
		{
			name:           "unquoted boolean matches quoted string",
			annotationsA:   `    dash0.com/enabled: false`,
			annotationsB:   `    dash0.com/enabled: "false"`,
			wantEquivalent: true,
		},
		{
			name:           "other annotations are still ignored",
			annotationsA:   `    dash0.com/sharing: private`,
			annotationsB:   `    dash0.com/sharing: all-users`,
			wantEquivalent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equivalent, err := SpamFilterYAMLEquivalent(spamFilterDoc(tt.annotationsA), spamFilterDoc(tt.annotationsB), nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEquivalent, equivalent)

			reversed, err := SpamFilterYAMLEquivalent(spamFilterDoc(tt.annotationsB), spamFilterDoc(tt.annotationsA), nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEquivalent, reversed, "equivalence must be symmetric")
		})
	}
}

func TestSpamFilterYAMLEquivalent_SpecChange(t *testing.T) {
	changed := `apiVersion: v1alpha2
kind: Dash0SpamFilter
metadata:
  name: noisy
spec:
  context: span
  filter:
    - key: body
      operator: contains
      value: spam
`
	equivalent, err := SpamFilterYAMLEquivalent(spamFilterDoc(""), changed, nil)
	require.NoError(t, err)
	assert.False(t, equivalent)
}

// The API returns JSON, with the annotation the server always reports.
func TestSpamFilterYAMLEquivalent_APIResponse(t *testing.T) {
	apiResponse := `{"apiVersion":"v1alpha2","kind":"Dash0SpamFilter","metadata":{"name":"noisy","annotations":{"dash0.com/enabled":"false"},"labels":{"dash0.com/origin":"tf_x"}},"spec":{"context":"log","filter":[{"key":"body","operator":"contains","value":"spam"}]}}`

	equivalent, err := SpamFilterYAMLEquivalent(spamFilterDoc(`    dash0.com/enabled: "false"`), apiResponse, nil)
	require.NoError(t, err)
	assert.True(t, equivalent)

	equivalent, err = SpamFilterYAMLEquivalent(spamFilterDoc(""), apiResponse, nil)
	require.NoError(t, err)
	assert.False(t, equivalent, "a filter disabled outside Terraform must be reported as drift")
}

func TestSpamFilterYAMLEquivalent_InvalidYAML(t *testing.T) {
	_, err := SpamFilterYAMLEquivalent("metadata: [", spamFilterDoc(""), nil)
	assert.Error(t, err)
}
