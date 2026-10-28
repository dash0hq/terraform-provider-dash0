package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// awsScrapeRead runs Read against a state holding awsScrapeTestYaml with the
// given GET response (as returned by the client wrapper: no labels, no
// annotations, origin separately).
func awsScrapeRead(t *testing.T, stateOrigin interface{}, apiJSON, apiOrigin string, apiErr error) *resource.ReadResponse {
	t.Helper()
	m := &MockClient{}
	m.On("GetAwsMetricScrapeConfig", mock.Anything, awsScrapeTestIntegrationID).Return(apiJSON, apiOrigin, apiErr)

	state := awsScrapeState(awsScrapeTestIntegrationID, stateOrigin, awsScrapeTestYaml)
	resp := &resource.ReadResponse{State: state}
	(&AwsMetricScrapeConfigResource{client: m}).Read(context.Background(), resource.ReadRequest{State: state}, resp)
	m.AssertExpectations(t)
	return resp
}

func TestAwsMetricScrapeConfigResource_Read(t *testing.T) {
	// The server sorts namespaces and metrics, and returns the integration's name.
	const sortedJSON = `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","metadata":{"name":"dash0-integration-test"},` +
		`"spec":{"namespaces":[{"namespace":"AWS/Lambda","metrics":["Errors","Invocations"]},{"namespace":"AWS/SQS","metrics":["ApproximateAgeOfOldestMessage","NumberOfMessagesSent"]}]}}`
	const driftedJSON = `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","metadata":{"name":"dash0-integration-test"},` +
		`"spec":{"namespaces":[{"namespace":"AWS/Lambda","metrics":["Errors"]}]}}`
	const clearedJSON = `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AwsMetricScrapeConfig","metadata":{"name":"dash0-integration-test"},"spec":{"namespaces":[]}}`

	t.Run("equivalent config keeps the state YAML", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, sortedJSON, awsScrapeTestOrigin, nil)
		assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		got := awsScrapeGetModel(t, resp.State)
		assert.Equal(t, awsScrapeTestYaml, got.AwsMetricScrapeConfigYaml.ValueString())
		assert.Equal(t, awsScrapeTestOrigin, got.Origin.ValueString())
	})

	t.Run("drift replaces the state YAML", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, driftedJSON, awsScrapeTestOrigin, nil)
		assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.Equal(t, driftedJSON, awsScrapeGetModel(t, resp.State).AwsMetricScrapeConfigYaml.ValueString())
	})

	t.Run("origin is refreshed from the server", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, sortedJSON, "api-other", nil)
		assert.Equal(t, "api-other", awsScrapeGetModel(t, resp.State).Origin.ValueString())
	})

	t.Run("config cleared outside Terraform is removed from state", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, clearedJSON, "", nil)
		assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, resp.State.Raw.IsNull())
	})

	t.Run("empty namespaces written by Terraform stay in state", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, clearedJSON, awsScrapeTestOrigin, nil)
		assert.False(t, resp.State.Raw.IsNull())
		assert.Equal(t, clearedJSON, awsScrapeGetModel(t, resp.State).AwsMetricScrapeConfigYaml.ValueString())
	})

	t.Run("config without origin but with namespaces stays in state", func(t *testing.T) {
		resp := awsScrapeRead(t, nil, sortedJSON, "", nil)
		assert.False(t, resp.State.Raw.IsNull())
		assert.True(t, awsScrapeGetModel(t, resp.State).Origin.IsNull())
	})

	t.Run("integration gone is removed from state", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, "", "", errNotFound)
		assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, resp.State.Raw.IsNull())
	})

	t.Run("server error surfaces", func(t *testing.T) {
		resp := awsScrapeRead(t, awsScrapeTestOrigin, "", "", errServer)
		assert.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
	})
}
