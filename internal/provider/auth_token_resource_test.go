package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const (
	authTokenTestID     = "11111111-1111-1111-1111-111111111111"
	authTokenTestSecret = "auth_secret"
	authTokenTestYaml   = `apiVersion: dash0.com/v1alpha1
kind: Dash0AuthToken
metadata:
  name: ci
spec:
  actions:
    - dashboards:read`
)

func authTokenTestSchema() resource.SchemaResponse {
	resp := resource.SchemaResponse{}
	(&AuthTokenResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp
}

// authTokenRaw builds an auth token object value. nil becomes null; use
// tftypes.UnknownValue to build an unknown.
func authTokenRaw(origin, id, token interface{}, authTokenYaml string) tftypes.Value {
	return tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"origin": tftypes.String, "id": tftypes.String, "token": tftypes.String, "auth_token_yaml": tftypes.String,
		}},
		map[string]tftypes.Value{
			"origin":          tftypes.NewValue(tftypes.String, origin),
			"id":              tftypes.NewValue(tftypes.String, id),
			"token":           tftypes.NewValue(tftypes.String, token),
			"auth_token_yaml": tftypes.NewValue(tftypes.String, authTokenYaml),
		},
	)
}

func authTokenState(origin, id, token interface{}, authTokenYaml string) tfsdk.State {
	return tfsdk.State{Raw: authTokenRaw(origin, id, token, authTokenYaml), Schema: authTokenTestSchema().Schema}
}

func TestAuthTokenResource_Schema(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewAuthTokenResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_auth_token", resp.TypeName)

	s := authTokenTestSchema().Schema
	assert.True(t, s.Attributes["auth_token_yaml"].IsRequired())
	assert.True(t, s.Attributes["token"].IsComputed())
	assert.True(t, s.Attributes["token"].IsSensitive(), "the secret must not show in plans or logs")
	assert.False(t, s.Attributes["id"].IsSensitive())
}

func TestAuthTokenResource_ValidateConfig(t *testing.T) {
	validate := func(y string) *resource.ValidateConfigResponse {
		resp := &resource.ValidateConfigResponse{}
		(&AuthTokenResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
			Config: tfsdk.Config{Raw: authTokenRaw(nil, nil, nil, y), Schema: authTokenTestSchema().Schema},
		}, resp)
		return resp
	}

	assert.False(t, validate(authTokenTestYaml).Diagnostics.HasError())
	assert.True(t, validate("kind: [").Diagnostics.HasError())
	assert.True(t, validate("apiVersion: dash0.com/v1alpha1\nkind: Dash0Team\nmetadata:\n  name: x").Diagnostics.HasError())
	assert.True(t, validate("kind: Dash0AuthToken\nmetadata:\n  name: x").Diagnostics.HasError(), "missing apiVersion")

	withSecret := validate(authTokenTestYaml + "\n  token: auth_x").Diagnostics
	assert.True(t, withSecret.HasError())
	assert.Contains(t, withSecret.Errors()[0].Summary(), "spec.token")
}

func TestAuthTokenResource_Create_StoresIDAndToken(t *testing.T) {
	mockClient := &MockClient{}
	mockClient.On("CreateAuthToken", mock.Anything,
		mock.MatchedBy(func(o string) bool { return strings.HasPrefix(o, "tf_") }),
		mock.AnythingOfType("string")).Return(authTokenTestID, authTokenTestSecret, nil)

	sch := authTokenTestSchema().Schema
	req := resource.CreateRequest{Plan: tfsdk.Plan{
		Raw:    authTokenRaw(tftypes.UnknownValue, tftypes.UnknownValue, tftypes.UnknownValue, authTokenTestYaml),
		Schema: sch,
	}}
	resp := &resource.CreateResponse{State: authTokenState(nil, nil, nil, "")}

	(&AuthTokenResource{client: mockClient}).Create(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	var got authTokenModel
	resp.State.Get(context.Background(), &got)
	assert.True(t, strings.HasPrefix(got.Origin.ValueString(), "tf_"))
	assert.Equal(t, authTokenTestID, got.ID.ValueString())
	assert.Equal(t, authTokenTestSecret, got.Token.ValueString())
	assert.Equal(t, authTokenTestYaml, got.AuthTokenYaml.ValueString())
	mockClient.AssertExpectations(t)
}

func TestAuthTokenResource_Create_ClientError(t *testing.T) {
	mockClient := &MockClient{}
	mockClient.On("CreateAuthToken", mock.Anything, mock.Anything, mock.Anything).Return("", "", errServer)

	req := resource.CreateRequest{Plan: tfsdk.Plan{
		Raw:    authTokenRaw(tftypes.UnknownValue, tftypes.UnknownValue, tftypes.UnknownValue, authTokenTestYaml),
		Schema: authTokenTestSchema().Schema,
	}}
	resp := &resource.CreateResponse{State: authTokenState(nil, nil, nil, "")}

	(&AuthTokenResource{client: mockClient}).Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.Equal(authTokenRaw(nil, nil, nil, "")), "no state is saved on failure")
}

func TestAuthTokenResource_Update_KeepsIDOriginAndToken(t *testing.T) {
	mockClient := &MockClient{}
	mockClient.On("UpdateAuthToken", mock.Anything, authTokenTestID, "tf_abc", mock.AnythingOfType("string")).Return(nil)

	newYaml := strings.Replace(authTokenTestYaml, "name: ci", "name: ci-renamed", 1)
	req := resource.UpdateRequest{
		State: authTokenState("tf_abc", authTokenTestID, authTokenTestSecret, authTokenTestYaml),
		Plan: tfsdk.Plan{
			Raw:    authTokenRaw(tftypes.UnknownValue, tftypes.UnknownValue, tftypes.UnknownValue, newYaml),
			Schema: authTokenTestSchema().Schema,
		},
	}
	resp := &resource.UpdateResponse{State: authTokenState("tf_abc", authTokenTestID, authTokenTestSecret, authTokenTestYaml)}

	(&AuthTokenResource{client: mockClient}).Update(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	var got authTokenModel
	resp.State.Get(context.Background(), &got)
	assert.Equal(t, "tf_abc", got.Origin.ValueString())
	assert.Equal(t, authTokenTestID, got.ID.ValueString())
	assert.Equal(t, authTokenTestSecret, got.Token.ValueString())
	assert.Equal(t, newYaml, got.AuthTokenYaml.ValueString())
	mockClient.AssertExpectations(t)
}

func TestAuthTokenResource_Delete(t *testing.T) {
	del := func(err error) *resource.DeleteResponse {
		mockClient := &MockClient{}
		mockClient.On("DeleteAuthToken", mock.Anything, authTokenTestID).Return(err)
		resp := &resource.DeleteResponse{}
		(&AuthTokenResource{client: mockClient}).Delete(context.Background(),
			resource.DeleteRequest{State: authTokenState("tf_abc", authTokenTestID, authTokenTestSecret, authTokenTestYaml)}, resp)
		mockClient.AssertExpectations(t)
		return resp
	}

	assert.False(t, del(nil).Diagnostics.HasError())
	assert.False(t, del(errNotFound).Diagnostics.HasError(), "404 means already gone")

	// The API refuses to delete the last auth token of the organization.
	lastToken := &dash0.APIError{StatusCode: 400, Status: "400 Bad Request", Message: "cannot delete the last auth token"}
	resp := del(lastToken)
	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "cannot delete the last auth token")
}

func TestAuthTokenResource_ImportState(t *testing.T) {
	importState := func(id string, apiResponse string, err error) (*resource.ImportStateResponse, authTokenModel) {
		mockClient := &MockClient{}
		// The API returns the secret only on create, so import finds none.
		mockClient.On("GetAuthToken", mock.Anything, id).Return(apiResponse, "", err)
		resp := &resource.ImportStateResponse{State: authTokenState(nil, nil, nil, "")}
		(&AuthTokenResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, resp)
		var got authTokenModel
		if !resp.Diagnostics.HasError() {
			resp.State.Get(context.Background(), &got)
		}
		return resp, got
	}

	body := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci","labels":{"dash0.com/id":"` + authTokenTestID + `","dash0.com/origin":"tf_abc"}}}`

	t.Run("by origin resolves the id from labels", func(t *testing.T) {
		resp, got := importState("tf_abc", body, nil)
		assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.Equal(t, authTokenTestID, got.ID.ValueString())
		assert.Equal(t, "tf_abc", got.Origin.ValueString())
		assert.True(t, got.Token.IsNull(), "an imported token has no secret")
		assert.Equal(t, body, got.AuthTokenYaml.ValueString())
	})
	t.Run("without origin label leaves origin null", func(t *testing.T) {
		resp, got := importState(authTokenTestID,
			`{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci","labels":{"dash0.com/id":"`+authTokenTestID+`"}}}`, nil)
		assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
		assert.True(t, got.Origin.IsNull())
	})
	t.Run("response without id label errors", func(t *testing.T) {
		resp, _ := importState("tf_abc", `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0AuthToken","metadata":{"name":"ci"}}`, nil)
		assert.True(t, resp.Diagnostics.HasError())
	})
	t.Run("unknown id errors", func(t *testing.T) {
		resp, _ := importState("nope", "", errNotFound)
		assert.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "nope")
	})
}
