package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// datasetServerResponse is what the API returns for datasetTestYaml: the
// user's fields plus server-managed and server-defaulted ones.
const datasetServerResponse = `{
  "kind": "Dash0Dataset",
  "metadata": {
    "name": "Production",
    "labels": {"dash0.com/id": "production", "dash0.com/origin": "tf_abc", "dash0.com/source": "terraform", "dash0.com/version": "7"},
    "annotations": {"dash0.com/created-at": "2026-01-01T00:00:00Z", "dash0.com/updated-at": "2026-02-01T00:00:00Z"}
  },
  "spec": {
    "tracing": {"retention": "30d", "convertSpanEventsToLogRecords": true},
    "logging": {"retention": "30d"},
    "metrics": {"retention": "13m"},
    "profiling": {"retention": "7d"},
    "webEvents": {"retention": "30d"},
    "geoLocation": {"endUserGeoLocationStorageStrategy": "full"},
    "ipAddresses": {"endUserIpAddressStorageStrategy": "full"},
    "genAiAttributeRedaction": {"conversationContent": "none"},
    "darkplane": {"enabled": true},
    "cardholderDataRedaction": {"enabled": false},
    "semanticConventionUpgrades": "latest",
    "preferred": true,
    "permissions": [{"actions": ["dataset:read"], "role": "basic_member"}],
    "permittedActions": ["dataset:read", "dataset:update"],
    "telemetryFilters": [{"name": "drop-health"}]
  }
}`

func datasetRead(t *testing.T, stateYaml, apiResponse string, apiErr error) (*resource.ReadResponse, datasetModel) {
	t.Helper()
	mockClient := &MockClient{}
	mockClient.On("GetDataset", mock.Anything, "production").Return(apiResponse, apiErr)
	state := datasetState("tf_abc", "production", stateYaml)
	resp := &resource.ReadResponse{State: state}

	(&DatasetResource{client: mockClient}).Read(context.Background(), resource.ReadRequest{State: state}, resp)

	var got datasetModel
	if !resp.State.Raw.IsNull() {
		resp.State.Get(context.Background(), &got)
	}
	return resp, got
}

func TestDatasetResource_Read_404RemovesFromState(t *testing.T) {
	resp, _ := datasetRead(t, datasetTestYaml, "", errNotFound)
	assert.False(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull())
}

func TestDatasetResource_Read_SoftDeletedRemovesFromState(t *testing.T) {
	resp, _ := datasetRead(t, datasetTestYaml, softDeletedJSON, nil)
	assert.True(t, resp.State.Raw.IsNull())
}

func TestDatasetResource_Read_OtherErrorSurfaces(t *testing.T) {
	resp, _ := datasetRead(t, datasetTestYaml, "", errServer)
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
}

// Server-defaulted spec fields and server-managed fields the user did not
// author must not change state.
func TestDatasetResource_Read_ServerDefaultsCauseNoDrift(t *testing.T) {
	resp, got := datasetRead(t, datasetTestYaml, datasetServerResponse, nil)
	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, datasetTestYaml, got.DatasetYaml.ValueString())
}

// permissions and telemetryFilters are ignored even when the user sets them.
func TestDatasetResource_Read_PermissionsNeverDrift(t *testing.T) {
	stateYaml := datasetTestYaml + "\n  permissions:\n    - role: admin\n      actions: [dataset:update]"
	_, got := datasetRead(t, stateYaml, datasetServerResponse, nil)
	assert.Equal(t, stateYaml, got.DatasetYaml.ValueString())
}

// A field the user set still detects drift.
func TestDatasetResource_Read_UserSetFieldDetectsDrift(t *testing.T) {
	apiResponse := `{"kind":"Dash0Dataset","metadata":{"name":"Production","labels":{"dash0.com/id":"production"}},"spec":{"tracing":{"retention":"7d"}}}`
	_, got := datasetRead(t, datasetTestYaml, apiResponse, nil)
	assert.Equal(t, apiResponse, got.DatasetYaml.ValueString())
}

func TestDatasetResource_Read_NameDriftDetected(t *testing.T) {
	apiResponse := `{"kind":"Dash0Dataset","metadata":{"name":"Renamed"},"spec":{"tracing":{"retention":"30d"}}}`
	_, got := datasetRead(t, datasetTestYaml, apiResponse, nil)
	assert.Equal(t, apiResponse, got.DatasetYaml.ValueString())
}
