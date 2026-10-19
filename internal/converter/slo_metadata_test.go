package converter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSLOYAMLEquivalent(t *testing.T) {
	tests := []struct {
		name           string
		labelsA        map[string]interface{}
		annotationsA   map[string]interface{}
		labelsB        map[string]interface{}
		annotationsB   map[string]interface{}
		wantEquivalent bool
	}{
		{
			name:           "same stored metadata",
			labelsA:        map[string]interface{}{"team": "payments"},
			annotationsA:   map[string]interface{}{"dash0.com/display-name": "Checkout availability", "dash0.com/folder-path": "/payments", "owner": "payments-oncall"},
			labelsB:        map[string]interface{}{"team": "payments"},
			annotationsB:   map[string]interface{}{"dash0.com/display-name": "Checkout availability", "dash0.com/folder-path": "/payments", "owner": "payments-oncall"},
			wantEquivalent: true,
		},
		{
			name:           "display name edit",
			annotationsA:   map[string]interface{}{"dash0.com/display-name": "Checkout availability"},
			annotationsB:   map[string]interface{}{"dash0.com/display-name": "Checkout success"},
			wantEquivalent: false,
		},
		{
			name:           "display name removal",
			annotationsA:   map[string]interface{}{"dash0.com/display-name": "Checkout availability"},
			wantEquivalent: false,
		},
		{
			name:           "blank display name",
			annotationsB:   map[string]interface{}{"dash0.com/display-name": "  "},
			wantEquivalent: true,
		},
		{
			name:           "enabled false",
			annotationsB:   map[string]interface{}{"dash0.com/enabled": "false"},
			wantEquivalent: false,
		},
		{
			name:           "enabled true",
			annotationsB:   map[string]interface{}{"dash0.com/enabled": "true"},
			wantEquivalent: true,
		},
		{
			name:           "unparseable enabled",
			annotationsA:   map[string]interface{}{"dash0.com/enabled": "yes"},
			annotationsB:   map[string]interface{}{"dash0.com/enabled": "true"},
			wantEquivalent: true,
		},
		{
			name:           "enabled as a number",
			annotationsA:   map[string]interface{}{"dash0.com/enabled": "0"},
			annotationsB:   map[string]interface{}{"dash0.com/enabled": "false"},
			wantEquivalent: true,
		},
		{
			name:           "unquoted enabled",
			annotationsA:   map[string]interface{}{"dash0.com/enabled": false},
			annotationsB:   map[string]interface{}{"dash0.com/enabled": "false"},
			wantEquivalent: true,
		},
		{
			name:           "folder path edit",
			annotationsA:   map[string]interface{}{"dash0.com/folder-path": "/payments"},
			annotationsB:   map[string]interface{}{"dash0.com/folder-path": "/checkout"},
			wantEquivalent: false,
		},
		{
			name:           "empty folder path",
			annotationsA:   map[string]interface{}{"dash0.com/folder-path": ""},
			wantEquivalent: true,
		},
		{
			name:           "sharing edit",
			annotationsA:   map[string]interface{}{"dash0.com/sharing": "role:basic_member"},
			annotationsB:   map[string]interface{}{"dash0.com/sharing": "role:admin"},
			wantEquivalent: false,
		},
		{
			name:           "user label edit",
			labelsA:        map[string]interface{}{"team": "payments"},
			labelsB:        map[string]interface{}{"team": "checkout"},
			wantEquivalent: false,
		},
		{
			name:           "user label removal",
			labelsA:        map[string]interface{}{"team": "payments"},
			wantEquivalent: false,
		},
		{
			name:           "numeric user label",
			labelsA:        map[string]interface{}{"tier": 1},
			labelsB:        map[string]interface{}{"tier": "1"},
			wantEquivalent: true,
		},
		{
			name:           "user annotation edit",
			annotationsA:   map[string]interface{}{"owner": "payments-oncall"},
			annotationsB:   map[string]interface{}{"owner": "checkout-oncall"},
			wantEquivalent: false,
		},
		{
			name: "server labels",
			labelsB: map[string]interface{}{
				"dash0.com/dataset": "default",
				"dash0.com/id":      "6f1d1c3e-7c43-4f39-9a52-0b7a1d8e1f20",
				"dash0.com/origin":  "tf_0b7a1d8e",
				"dash0.com/source":  "terraform",
				"dash0.com/version": "3",
			},
			wantEquivalent: true,
		},
		{
			name: "server annotations",
			annotationsB: map[string]interface{}{
				"dash0.com/created-at":   "2026-10-01T09:00:00Z",
				"dash0.com/updated-at":   "2026-10-02T09:00:00Z",
				"dash0.com/deleted-at":   "2026-10-03T09:00:00Z",
				"dash0.com/window-start": "2026-10-01T09:00:00Z",
			},
			wantEquivalent: true,
		},
		{
			name:           "unknown dash0 annotation",
			annotationsA:   map[string]interface{}{"dash0.com/owner": "payments-oncall"},
			wantEquivalent: true,
		},
		{
			name:           "dash0 prefix in another case",
			labelsA:        map[string]interface{}{"Dash0.com/Team": "payments"},
			wantEquivalent: true,
		},
		{
			name:           "display name key as a label",
			labelsA:        map[string]interface{}{"dash0.com/display-name": "Checkout availability"},
			wantEquivalent: true,
		},
		{
			name:           "blank user key",
			labelsA:        map[string]interface{}{" ": "payments"},
			wantEquivalent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlA := sloYAMLWithMetadata(t, tt.labelsA, tt.annotationsA)
			yamlB := sloYAMLWithMetadata(t, tt.labelsB, tt.annotationsB)

			equivalent, err := SLOYAMLEquivalent(yamlA, yamlB, nil)

			require.NoError(t, err)
			assert.Equal(t, tt.wantEquivalent, equivalent, "yamlA:\n%s\nyamlB:\n%s", yamlA, yamlB)
		})
	}
}

func TestSLOYAMLEquivalent_SpecChange(t *testing.T) {
	yamlA := sloYAMLWithMetadata(t, nil, nil)
	yamlB := strings.Replace(yamlA, "service: checkout", "service: payments", 1)
	require.NotEqual(t, yamlA, yamlB)

	equivalent, err := SLOYAMLEquivalent(yamlA, yamlB, nil)

	require.NoError(t, err)
	assert.False(t, equivalent)
}

func TestSLOYAMLEquivalent_APIResponse(t *testing.T) {
	config := `apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-availability
  labels:
    team: payments
  annotations:
    dash0.com/display-name: Checkout availability
    dash0.com/folder-path: /payments
    owner: payments-oncall
spec:
  service: checkout
  objectives:
    - displayName: 99% availability
      target: 0.99
`
	apiResponse := `{"apiVersion":"openslo.com/v1","kind":"SLO","metadata":{"name":"checkout-availability",` +
		`"labels":{"dash0.com/dataset":"default","dash0.com/id":"6f1d1c3e-7c43-4f39-9a52-0b7a1d8e1f20","dash0.com/origin":"tf_0b7a1d8e","dash0.com/source":"terraform","dash0.com/version":"3","team":"payments"},` +
		`"annotations":{"dash0.com/created-at":"2026-10-01T09:00:00Z","dash0.com/updated-at":"2026-10-02T09:00:00Z","dash0.com/window-start":"2026-10-01T09:00:00Z","dash0.com/enabled":"true","dash0.com/folder-path":"/payments","dash0.com/display-name":"Checkout availability","owner":"payments-oncall"}},` +
		`"spec":{"service":"checkout","objectives":[{"displayName":"99% availability","target":0.99}]}}`

	equivalent, err := SLOYAMLEquivalent(config, apiResponse, nil)

	require.NoError(t, err)
	assert.True(t, equivalent)
}

func TestSLOYAMLEquivalent_InvalidYAML(t *testing.T) {
	_, err := SLOYAMLEquivalent("invalid: : yaml", sloYAMLWithMetadata(t, nil, nil), nil)

	assert.Error(t, err)
}

func sloYAMLWithMetadata(t *testing.T, labels, annotations map[string]interface{}) string {
	t.Helper()

	metadata := map[string]interface{}{"name": "checkout-availability"}
	if labels != nil {
		metadata["labels"] = labels
	}
	if annotations != nil {
		metadata["annotations"] = annotations
	}

	document, err := yaml.Marshal(map[string]interface{}{
		"apiVersion": "openslo.com/v1",
		"kind":       "SLO",
		"metadata":   metadata,
		"spec":       map[string]interface{}{"service": "checkout"},
	})
	require.NoError(t, err)

	return string(document)
}
