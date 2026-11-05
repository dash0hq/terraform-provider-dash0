package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const customLabelsSpecYAML = `
spec:
  enabled: true
  sample:
    interval: 5m
`

func customLabelsDoc(labels string) string {
	if labels == "" {
		return "kind: Dash0TimeSeriesAggregation\nmetadata:\n  name: http-duration" + customLabelsSpecYAML
	}
	return "kind: Dash0TimeSeriesAggregation\nmetadata:\n  name: http-duration\n  labels:\n" + labels + customLabelsSpecYAML
}

func TestCustomLabelsYAMLEquivalent(t *testing.T) {
	tests := []struct {
		name           string
		labelsA        string
		labelsB        string
		wantEquivalent bool
	}{
		{
			name:           "both omit labels",
			wantEquivalent: true,
		},
		{
			name:           "same custom labels",
			labelsA:        "    custom:\n      team: platform",
			labelsB:        "    custom:\n      team: platform",
			wantEquivalent: true,
		},
		{
			name:           "changed value is a change",
			labelsA:        "    custom:\n      team: platform",
			labelsB:        "    custom:\n      team: payments",
			wantEquivalent: false,
		},
		{
			name:           "added label is a change",
			labelsA:        "    custom:\n      team: platform",
			labelsB:        "    custom:\n      team: platform\n      tier: gold",
			wantEquivalent: false,
		},
		{
			name:           "removed labels are a change",
			labelsA:        "    custom:\n      team: platform",
			wantEquivalent: false,
		},
		{
			name:           "omitted custom map equals an empty one",
			labelsB:        "    custom: {}",
			wantEquivalent: true,
		},
		{
			name:           "server-managed labels are ignored",
			labelsA:        "    custom:\n      team: platform",
			labelsB:        "    custom:\n      team: platform\n    dash0.com/id: abc\n    dash0.com/origin: tf_123\n    dash0.com/dataset: default\n    dash0.com/version: \"7\"",
			wantEquivalent: true,
		},
		{
			name:           "unquoted value matches quoted string",
			labelsA:        "    custom:\n      replicas: 3",
			labelsB:        "    custom:\n      replicas: \"3\"",
			wantEquivalent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equivalent, err := CustomLabelsYAMLEquivalent(customLabelsDoc(tt.labelsA), customLabelsDoc(tt.labelsB), nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEquivalent, equivalent)

			reversed, err := CustomLabelsYAMLEquivalent(customLabelsDoc(tt.labelsB), customLabelsDoc(tt.labelsA), nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEquivalent, reversed, "comparison must be symmetric")
		})
	}
}

func TestCustomLabelsYAMLEquivalent_SpecDifferenceStillCounts(t *testing.T) {
	a := customLabelsDoc("    custom:\n      team: platform")
	b := customLabelsDoc("    custom:\n      team: platform")
	b = b[:len(b)-len("5m\n")] + "10m\n"

	equivalent, err := CustomLabelsYAMLEquivalent(a, b, nil)
	require.NoError(t, err)
	assert.False(t, equivalent)
}

func TestCustomLabelsYAMLEquivalent_ComparesJSONAPIResponse(t *testing.T) {
	config := customLabelsDoc("    custom:\n      team: platform")
	apiResponse := `{"kind":"Dash0TimeSeriesAggregation","metadata":{"name":"http-duration","labels":{"custom":{"team":"payments"},"dash0.com/id":"abc"}},"spec":{"enabled":true,"sample":{"interval":"5m"}}}`

	equivalent, err := CustomLabelsYAMLEquivalent(config, apiResponse, nil)
	require.NoError(t, err)
	assert.False(t, equivalent, "a label changed outside Terraform must be reported")
}
