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

const awsScrapeIntegrationID = "27755ed0-0009-422d-8c11-19c6dd72df98"

func TestGetAwsMetricScrapeConfig_StripsMetadataAndReturnsOrigin(t *testing.T) {
	origin, id, dataset := "tf_x", awsScrapeIntegrationID, "default"
	source := dash0.CrdSource("terraform")
	now := time.Now()
	name := "dash0-integration-test"
	c := &dash0Client{inner: &dash0test.MockClient{
		GetAwsMetricScrapeConfigFunc: func(_ context.Context, integrationID string) (*dash0.AwsMetricScrapeConfigDefinition, error) {
			assert.Equal(t, awsScrapeIntegrationID, integrationID)
			return &dash0.AwsMetricScrapeConfigDefinition{
				ApiVersion: "dash0.com/v1alpha1",
				Kind:       "Dash0AwsMetricScrapeConfig",
				Metadata: dash0.AwsMetricScrapeConfigMetadata{
					Name:        &name,
					Labels:      &dash0.AwsMetricScrapeConfigLabels{Dash0Comorigin: &origin, Dash0Comid: &id, Dash0Comdataset: &dataset, Dash0Comsource: &source},
					Annotations: &dash0.AwsMetricScrapeConfigAnnotations{Dash0ComcreatedAt: &now},
				},
				Spec: dash0.AwsMetricScrapeConfigSpec{Namespaces: []dash0.AwsMetricScrapeConfigNamespace{{Namespace: "AWS/Lambda", Metrics: []string{"Errors"}}}},
			}, nil
		},
	}}

	gotJSON, gotOrigin, err := c.GetAwsMetricScrapeConfig(context.Background(), awsScrapeIntegrationID)
	require.NoError(t, err)
	assert.Equal(t, "tf_x", gotOrigin)
	assert.JSONEq(t, `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","metadata":{},"spec":{"namespaces":[{"namespace":"AWS/Lambda","metrics":["Errors"]}]}}`, gotJSON)
}

func TestUpdateAwsMetricScrapeConfig_SetsOrigin(t *testing.T) {
	var sent *dash0.AwsMetricScrapeConfigDefinition
	c := &dash0Client{inner: &dash0test.MockClient{
		UpdateAwsMetricScrapeConfigFunc: func(_ context.Context, _ string, cfg *dash0.AwsMetricScrapeConfigDefinition) (*dash0.AwsMetricScrapeConfigDefinition, error) {
			sent = cfg
			return cfg, nil
		},
	}}

	err := c.UpdateAwsMetricScrapeConfig(context.Background(), awsScrapeIntegrationID, "tf_x",
		`{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","spec":{"namespaces":[]}}`)
	require.NoError(t, err)
	assert.Equal(t, "tf_x", dash0.GetAwsMetricScrapeConfigOrigin(sent))
	assert.NotNil(t, sent.Spec.Namespaces, "an empty list must be sent as [], not null")

	assert.Error(t, c.UpdateAwsMetricScrapeConfig(context.Background(), awsScrapeIntegrationID, "tf_x", "not json"))
}
