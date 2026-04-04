// SPDX-FileCopyrightText: Copyright 2023-2026 Dash0 Inc.

package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDash0Client_RecordingRuleGroup(t *testing.T) {
	// Verify client creation works (recording rule group methods are available on the client)
	c, err := NewDash0Client("https://api.example.com", "auth_test-token", "test")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestUnmarshalRecordingRuleGroup(t *testing.T) {
	yamlInput := `
kind: RecordingRuleGroup
metadata:
  name: test-group
spec:
  interval: "60s"
  rules:
    - record: "job:http_requests:rate5m"
      expr: "rate(http_requests_total[5m])"
`
	group, err := unmarshalRecordingRuleGroup(yamlInput)
	require.NoError(t, err)
	assert.Equal(t, "test-group", group.Metadata.Name)
	assert.NotEmpty(t, group.Spec.Rules)
	assert.Equal(t, "job:http_requests:rate5m", group.Spec.Rules[0].Record)
}

func TestEnsureRecordingRuleGroupLabels(t *testing.T) {
	yamlInput := `
kind: RecordingRuleGroup
metadata:
  name: test-group
spec:
  interval: "60s"
  rules:
    - record: "job:http_requests:rate5m"
      expr: "rate(http_requests_total[5m])"
`
	group, err := unmarshalRecordingRuleGroup(yamlInput)
	require.NoError(t, err)

	// Labels should be nil before initialization
	assert.Nil(t, group.Metadata.Labels)

	ensureRecordingRuleGroupLabels(group)

	// Labels should be initialized (non-nil) after the call
	assert.NotNil(t, group.Metadata.Labels)
}

func TestRecordingRuleGroupLabelsAndVersionInJSON(t *testing.T) {
	yamlInput := `
kind: RecordingRuleGroup
metadata:
  name: test-group
spec:
  interval: "60s"
  rules:
    - record: "job:http_requests:rate5m"
      expr: "rate(http_requests_total[5m])"
`
	group, err := unmarshalRecordingRuleGroup(yamlInput)
	require.NoError(t, err)

	ensureRecordingRuleGroupLabels(group)

	dataset := "my-dataset"
	origin := "tf_test-origin"
	version := "42"
	group.Metadata.Labels.Dash0Comdataset = &dataset
	group.Metadata.Labels.Dash0Comorigin = &origin
	group.Metadata.Labels.Dash0Comversion = &version

	jsonBytes, err := json.Marshal(group)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"dash0.com/dataset":"my-dataset"`)
	assert.Contains(t, jsonStr, `"dash0.com/origin":"tf_test-origin"`)
	assert.Contains(t, jsonStr, `"dash0.com/version":"42"`)
}
