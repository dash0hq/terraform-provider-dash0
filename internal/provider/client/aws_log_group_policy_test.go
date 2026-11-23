package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/dash0-api-client-go/dash0test"
)

func TestGetAwsLogGroupPolicy_StripsMetadataFillsDefaultAndReportsTombstone(t *testing.T) {
	origin, version := "tf_x", "7"
	now := time.Now()
	c := &dash0Client{inner: &dash0test.MockClient{
		GetAwsLogGroupInstrumentationPolicyFunc: func(_ context.Context, _ string) (*dash0.AwsLogGroupInstrumentationPolicy, error) {
			return &dash0.AwsLogGroupInstrumentationPolicy{
				ApiVersion: "dash0.com/v1alpha1",
				Kind:       "Dash0AwsLogGroupInstrumentationPolicy",
				Metadata: dash0.AwsLogGroupPolicyMetadata{
					Labels:      &dash0.AwsLogGroupPolicyLabels{Dash0Comorigin: &origin, Dash0Comversion: &version},
					Annotations: &dash0.AwsLogGroupPolicyAnnotations{Dash0ComdeletedAt: &now},
				},
				Spec: dash0.AwsLogGroupPolicySpec{Rules: []dash0.AwsLogGroupPolicyRule{}},
			}, nil
		},
	}}

	gotJSON, gotOrigin, deleted, err := c.GetAwsLogGroupPolicy(context.Background(), awsScrapeIntegrationID)
	require.NoError(t, err)
	assert.Equal(t, "tf_x", gotOrigin)
	assert.True(t, deleted)
	assert.JSONEq(t, `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsLogGroupInstrumentationPolicy","metadata":{},"spec":{"onNoLongerMatching":"keep","rules":[]}}`, gotJSON)
}

// The write is never fenced: whatever version a pasted export carries is dropped.
func TestUpdateAwsLogGroupPolicy_SendsOriginButNoVersion(t *testing.T) {
	var sent *dash0.AwsLogGroupInstrumentationPolicy
	c := &dash0Client{inner: &dash0test.MockClient{
		UpdateAwsLogGroupInstrumentationPolicyFunc: func(_ context.Context, _ string, p *dash0.AwsLogGroupInstrumentationPolicy) (*dash0.AwsLogGroupInstrumentationPolicy, error) {
			sent = p
			return p, nil
		},
	}}

	body := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsLogGroupInstrumentationPolicy","metadata":{"labels":{"dash0.com/version":"3","dash0.com/origin":"someone-else"}},"spec":{"rules":[]}}`
	require.NoError(t, c.UpdateAwsLogGroupPolicy(context.Background(), awsScrapeIntegrationID, "tf_x", body))
	require.NotNil(t, sent.Metadata.Labels)
	assert.Equal(t, "tf_x", *sent.Metadata.Labels.Dash0Comorigin)
	assert.Nil(t, sent.Metadata.Labels.Dash0Comversion)

	sent = nil
	require.NoError(t, c.UpdateAwsLogGroupPolicy(context.Background(), awsScrapeIntegrationID, "", body))
	assert.Nil(t, sent.Metadata.Labels, "an empty origin sends no labels at all")
}

func TestDeleteAwsLogGroupPolicy_PassesDispositionWithoutRevision(t *testing.T) {
	var gotDisposition dash0.AwsLogGroupPolicyDisposition
	var gotRevision *int64
	c := &dash0Client{inner: &dash0test.MockClient{
		DeleteAwsLogGroupInstrumentationPolicyFunc: func(_ context.Context, _ string, d dash0.AwsLogGroupPolicyDisposition, rev *int64) (*dash0.AwsLogGroupInstrumentationPolicy, error) {
			gotDisposition, gotRevision = d, rev
			return &dash0.AwsLogGroupInstrumentationPolicy{}, nil
		},
	}}

	require.NoError(t, c.DeleteAwsLogGroupPolicy(context.Background(), awsScrapeIntegrationID, "remove"))
	assert.Equal(t, dash0.AwsLogGroupPolicyDispositionRemove, gotDisposition)
	assert.Nil(t, gotRevision)
}

func TestParseAwsLogGroupPolicy_Rejects(t *testing.T) {
	for name, body := range map[string]string{
		"missing rules":   `{"spec":{}}`,
		"unknown field":   `{"spec":{"rules":[],"ruless":[]}}`,
		"bad operator":    `{"spec":{"rules":[{"conditions":[{"key":"dash0.cloud.tags.a","operator":"regex","value":"x"}]}]}}`,
		"bad disposition": `{"spec":{"rules":[],"onNoLongerMatching":"delete"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAwsLogGroupPolicy(body)
			assert.Error(t, err)
		})
	}
	_, err := ParseAwsLogGroupPolicy(`{"metadata":{"whatever":1},"spec":{"rules":[]}}`)
	assert.NoError(t, err, "metadata stays lenient")
}
