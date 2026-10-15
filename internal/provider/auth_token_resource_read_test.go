package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// authTokenServerResponse is what the API returns for authTokenTestYaml: the
// user's fields plus server-managed and server-defaulted ones.
const authTokenServerResponse = `{
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0AuthToken",
  "metadata": {
    "name": "ci",
    "labels": {"dash0.com/id": "` + authTokenTestID + `", "dash0.com/origin": "tf_abc"},
    "annotations": {"dash0.com/created-at": "2026-01-01T00:00:00Z"}
  },
  "spec": {
    "actions": ["dashboards:read"],
    "ingestion": {"defaultDataset": "default"},
    "scope": {
      "datasets": {"restriction": "unrestricted", "slugs": []},
      "signals": {"restriction": "unrestricted", "types": []}
    }
  }
}`

func authTokenRead(t *testing.T, stateYaml, apiResponse, apiToken string, apiErr error) (*resource.ReadResponse, authTokenModel) {
	t.Helper()
	mockClient := &MockClient{}
	mockClient.On("GetAuthToken", mock.Anything, authTokenTestID).Return(apiResponse, apiToken, apiErr)
	state := authTokenState("tf_abc", authTokenTestID, "old_secret", stateYaml)
	resp := &resource.ReadResponse{State: state}

	(&AuthTokenResource{client: mockClient}).Read(context.Background(), resource.ReadRequest{State: state}, resp)

	var got authTokenModel
	if !resp.State.Raw.IsNull() {
		resp.State.Get(context.Background(), &got)
	}
	return resp, got
}

// The API returns the secret only on create, so a refresh must keep the one in state.
func TestAuthTokenResource_Read_KeepsSecretWhenAPIReturnsNone(t *testing.T) {
	resp, got := authTokenRead(t, authTokenTestYaml, authTokenServerResponse, "", nil)
	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	assert.Equal(t, "old_secret", got.Token.ValueString())
}

func TestAuthTokenResource_Read_404RemovesFromState(t *testing.T) {
	resp, _ := authTokenRead(t, authTokenTestYaml, "", "", errNotFound)
	assert.False(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull())
}

func TestAuthTokenResource_Read_OtherErrorSurfaces(t *testing.T) {
	resp, _ := authTokenRead(t, authTokenTestYaml, "", "", errServer)
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Client Error")
}

// Server-defaulted spec fields the user did not set must not change state.
func TestAuthTokenResource_Read_ServerDefaultsCauseNoDrift(t *testing.T) {
	resp, got := authTokenRead(t, authTokenTestYaml, authTokenServerResponse, "", nil)
	assert.False(t, resp.Diagnostics.HasError())
	assert.Equal(t, authTokenTestYaml, got.AuthTokenYaml.ValueString())
}

// A defaulted field the user set still detects drift.
func TestAuthTokenResource_Read_UserSetDefaultedFieldDetectsDrift(t *testing.T) {
	stateYaml := authTokenTestYaml + "\n  scope:\n    datasets:\n      restriction: unrestricted"
	apiResponse := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci"},"spec":{"actions":["dashboards:read"],"scope":{"datasets":{"restriction":"restricted","slugs":["prod"]}}}}`
	_, got := authTokenRead(t, stateYaml, apiResponse, authTokenTestSecret, nil)
	assert.Equal(t, apiResponse, got.AuthTokenYaml.ValueString())
}

func TestAuthTokenResource_Read_ActionsDriftDetected(t *testing.T) {
	apiResponse := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci"},"spec":{"actions":["*"]}}`
	_, got := authTokenRead(t, authTokenTestYaml, apiResponse, authTokenTestSecret, nil)
	assert.Equal(t, apiResponse, got.AuthTokenYaml.ValueString())
}
