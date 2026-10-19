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
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0Dataset",
  "metadata": {
    "name": "Production",
    "labels": {"dash0.com/id": "production", "dash0.com/origin": "tf_abc", "dash0.com/source": "terraform", "dash0.com/version": "7"},
    "annotations": {"dash0.com/created-at": "2026-01-01T00:00:00Z", "dash0.com/updated-at": "2026-02-01T00:00:00Z"}
  },
  "spec": {
    "preferred": true,
    "retention": {"traces": "30d", "logs": "30d", "metrics": "13m", "profiles": "7d", "webEvents": "30d"},
    "privacy": {
      "ipAddresses": {"endUser": {"storage": "anonymize"}},
      "geoLocation": {"endUser": {"storage": "up_to_country"}},
      "redaction": {
        "cardholderData": {"enabled": false},
        "genAi": {"conversation": "none", "toolCalls": "none", "errors": "none"}
      }
    },
    "processing": {"spanEventsToLogs": true, "semanticConventionUpgrades": "latest", "darkplane": {"enabled": true}},
    "permissions": [{"actions": ["dataset:read"], "role": "basic_member"}],
    "permittedActions": ["dataset:read", "dataset:update"]
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

// permissions are ignored even when the user sets them.
func TestDatasetResource_Read_PermissionsNeverDrift(t *testing.T) {
	stateYaml := datasetTestYaml + "\n  permissions:\n    - role: admin\n      actions: [dataset:update]"
	_, got := datasetRead(t, stateYaml, datasetServerResponse, nil)
	assert.Equal(t, stateYaml, got.DatasetYaml.ValueString())
}

// spec.slug mirrors metadata.labels["dash0.com/id"] and is dropped by
// GetDataset's round-trip through the typed DatasetSpec struct, which has no
// Slug field. A state YAML that still carries it (e.g. from an out-of-band
// API response) must not show drift against an API response without it.
func TestDatasetResource_Read_SpecSlugNeverDrifts(t *testing.T) {
	stateYaml := datasetTestYaml + "\n  slug: production"
	_, got := datasetRead(t, stateYaml, datasetServerResponse, nil)
	assert.Equal(t, stateYaml, got.DatasetYaml.ValueString())
}

// A field the user set still detects drift.
func TestDatasetResource_Read_UserSetFieldDetectsDrift(t *testing.T) {
	apiResponse := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0Dataset","metadata":{"name":"Production","labels":{"dash0.com/id":"production"}},"spec":{"retention":{"traces":"7d"}}}`
	_, got := datasetRead(t, datasetTestYaml, apiResponse, nil)
	assert.Equal(t, apiResponse, got.DatasetYaml.ValueString())
}

func TestDatasetResource_Read_NameDriftDetected(t *testing.T) {
	apiResponse := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0Dataset","metadata":{"name":"Renamed"},"spec":{"retention":{"traces":"30d"}}}`
	_, got := datasetRead(t, datasetTestYaml, apiResponse, nil)
	assert.Equal(t, apiResponse, got.DatasetYaml.ValueString())
}

// Setting one leaf must not make the server-filled siblings drift.
func TestDatasetResource_Read_PartialRetentionCausesNoDrift(t *testing.T) {
	stateYaml := `apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: Production
  labels:
    dash0.com/id: production
spec:
  retention:
    logs: 30d`
	resp, got := datasetRead(t, stateYaml, datasetServerResponse, nil)
	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, stateYaml, got.DatasetYaml.ValueString())
}

// A user-set nested privacy field detects drift while untouched siblings do not.
func TestDatasetResource_Read_NestedPrivacyDriftDetected(t *testing.T) {
	stateYaml := `apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: Production
  labels:
    dash0.com/id: production
spec:
  privacy:
    ipAddresses:
      endUser:
        storage: full`
	_, got := datasetRead(t, stateYaml, datasetServerResponse, nil)
	assert.Equal(t, datasetServerResponse, got.DatasetYaml.ValueString())

	inSync := `apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: Production
  labels:
    dash0.com/id: production
spec:
  privacy:
    ipAddresses:
      endUser:
        storage: anonymize`
	_, got = datasetRead(t, inSync, datasetServerResponse, nil)
	assert.Equal(t, inSync, got.DatasetYaml.ValueString())
}
