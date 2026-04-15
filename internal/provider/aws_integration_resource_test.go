package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

// awsIntegrationSchema returns the attribute layout used for test state construction.
func awsIntegrationSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true},
			"dataset":                  schema.StringAttribute{Required: true},
			"external_id":              schema.StringAttribute{Required: true},
			"aws_account_id":           schema.StringAttribute{Required: true},
			"read_only_role_arn":       schema.StringAttribute{Required: true},
			"instrumentation_role_arn": schema.StringAttribute{Optional: true},
		},
	}
}

// awsIntegrationTftypesObject is the corresponding tftypes.Object used to build empty state.
func awsIntegrationTftypesObject() tftypes.Object {
	return tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"id":                       tftypes.String,
			"dataset":                  tftypes.String,
			"external_id":              tftypes.String,
			"aws_account_id":           tftypes.String,
			"read_only_role_arn":       tftypes.String,
			"instrumentation_role_arn": tftypes.String,
		},
	}
}

func emptyAwsIntegrationState() tftypes.Value {
	return tftypes.NewValue(awsIntegrationTftypesObject(), map[string]tftypes.Value{
		"id":                       tftypes.NewValue(tftypes.String, nil),
		"dataset":                  tftypes.NewValue(tftypes.String, nil),
		"external_id":              tftypes.NewValue(tftypes.String, nil),
		"aws_account_id":           tftypes.NewValue(tftypes.String, nil),
		"read_only_role_arn":       tftypes.NewValue(tftypes.String, nil),
		"instrumentation_role_arn": tftypes.NewValue(tftypes.String, nil),
	})
}

func TestAwsIntegrationResource_Metadata(t *testing.T) {
	r := &AwsIntegrationResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_aws_integration", resp.TypeName)
}

func TestAwsIntegrationResource_Schema(t *testing.T) {
	r := &AwsIntegrationResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)

	// Required
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Required)
	assert.True(t, resp.Schema.Attributes["external_id"].(schema.StringAttribute).Required)
	assert.True(t, resp.Schema.Attributes["aws_account_id"].(schema.StringAttribute).Required)
	assert.True(t, resp.Schema.Attributes["read_only_role_arn"].(schema.StringAttribute).Required)

	// Optional
	assert.True(t, resp.Schema.Attributes["instrumentation_role_arn"].(schema.StringAttribute).Optional)

	// Computed
	assert.True(t, resp.Schema.Attributes["id"].(schema.StringAttribute).Computed)
}

func TestAwsIntegrationResource_Configure(t *testing.T) {
	r := &AwsIntegrationResource{}
	c := &MockClient{}

	// nil provider data — noop
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)
	assert.Nil(t, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// valid provider data
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: c}, resp)
	assert.Equal(t, c, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// invalid provider data
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "invalid"}, resp)
	assert.True(t, resp.Diagnostics.HasError())
}

func TestAwsIntegrationOrigin_Deterministic(t *testing.T) {
	origin1 := model.AwsIntegrationOrigin("default", "123456789012", "ext-id-1")
	origin2 := model.AwsIntegrationOrigin("default", "123456789012", "ext-id-1")
	assert.Equal(t, origin1, origin2)
	assert.Contains(t, origin1, "terraform-")
}

func TestAwsIntegrationOrigin_UniquePerInput(t *testing.T) {
	base := model.AwsIntegrationOrigin("default", "123456789012", "ext-id-1")
	assert.NotEqual(t, base, model.AwsIntegrationOrigin("default", "123456789012", "ext-id-2"))
	assert.NotEqual(t, base, model.AwsIntegrationOrigin("default", "999999999999", "ext-id-1"))
	assert.NotEqual(t, base, model.AwsIntegrationOrigin("other", "123456789012", "ext-id-1"))
}

func TestBuildAwsIntegrationDefinition_ReadOnlyOnly(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:         types.StringValue("default"),
		ExternalID:      types.StringValue("org-tech-id"),
		AwsAccountID:    types.StringValue("123456789012"),
		ReadOnlyRoleArn: types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
	}

	origin := "terraform-test-origin"
	def := model.BuildAwsIntegrationDefinition(integration, origin)

	assert.Equal(t, "Dash0Integration", def.Kind)
	assert.Equal(t, "AWS 123456789012 (terraform)", def.Metadata.Name)
	assert.Equal(t, origin, def.Metadata.Labels.Origin)
	assert.True(t, def.Spec.Enabled)
	assert.Equal(t, "aws", def.Spec.Integration.Kind)
	assert.Equal(t, "default", def.Spec.Integration.Spec.Dataset)
	assert.Equal(t, "123456789012", def.Spec.Integration.Spec.AccountID)

	assert.Len(t, def.Spec.Integration.Spec.Roles, 1)
	assert.Equal(t, model.PermissionTypeReadOnly, def.Spec.Integration.Spec.Roles[0].PermissionType)
	assert.Equal(t, "arn:aws:iam::123456789012:role/dash0-read-only", def.Spec.Integration.Spec.Roles[0].Arn)
	assert.Equal(t, "org-tech-id", def.Spec.Integration.Spec.Roles[0].ExternalID)
}

func TestBuildAwsIntegrationDefinition_WithInstrumentation(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                types.StringValue("default"),
		ExternalID:             types.StringValue("org-tech-id"),
		AwsAccountID:           types.StringValue("123456789012"),
		ReadOnlyRoleArn:        types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		InstrumentationRoleArn: types.StringValue("arn:aws:iam::123456789012:role/dash0-instrumentation"),
	}

	def := model.BuildAwsIntegrationDefinition(integration, "terraform-test-origin")

	assert.Len(t, def.Spec.Integration.Spec.Roles, 2)
	assert.Equal(t, model.PermissionTypeReadOnly, def.Spec.Integration.Spec.Roles[0].PermissionType)
	assert.Equal(t, model.PermissionTypeResourcesInstrumentation, def.Spec.Integration.Spec.Roles[1].PermissionType)
	assert.Equal(t, "arn:aws:iam::123456789012:role/dash0-instrumentation", def.Spec.Integration.Spec.Roles[1].Arn)
}

func TestBuildAwsIntegrationDefinition_InstrumentationEmptyOmitted(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                types.StringValue("default"),
		ExternalID:             types.StringValue("org-tech-id"),
		AwsAccountID:           types.StringValue("123456789012"),
		ReadOnlyRoleArn:        types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		InstrumentationRoleArn: types.StringNull(),
	}

	def := model.BuildAwsIntegrationDefinition(integration, "terraform-test-origin")
	assert.Len(t, def.Spec.Integration.Spec.Roles, 1)
	assert.Equal(t, model.PermissionTypeReadOnly, def.Spec.Integration.Spec.Roles[0].PermissionType)
}

func TestBuildAwsIntegrationDefinition_JSONRoundTrip(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                types.StringValue("default"),
		ExternalID:             types.StringValue("org-tech-id"),
		AwsAccountID:           types.StringValue("123456789012"),
		ReadOnlyRoleArn:        types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		InstrumentationRoleArn: types.StringValue("arn:aws:iam::123456789012:role/dash0-instrumentation"),
	}

	origin := "terraform-test-origin"
	def := model.BuildAwsIntegrationDefinition(integration, origin)

	body, err := json.Marshal(def)
	require.NoError(t, err)

	var parsed model.IntegrationDefinition
	require.NoError(t, json.Unmarshal(body, &parsed))

	assert.Equal(t, def.Kind, parsed.Kind)
	assert.Equal(t, def.Metadata.Name, parsed.Metadata.Name)
	assert.Equal(t, origin, parsed.Metadata.Labels.Origin)
	assert.Equal(t, def.Spec.Integration.Spec.AccountID, parsed.Spec.Integration.Spec.AccountID)
	assert.Len(t, parsed.Spec.Integration.Spec.Roles, 2)
}

// --- CRUD tests using MockClient ---

func buildResourceForCRUDTest(t *testing.T) (*AwsIntegrationResource, *MockClient, schema.Schema) {
	t.Helper()
	r := &AwsIntegrationResource{}
	c := &MockClient{}
	configureResp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: c}, configureResp)
	require.False(t, configureResp.Diagnostics.HasError())
	return r, c, awsIntegrationSchema()
}

func planForTest(t *testing.T, sch schema.Schema, integration model.AwsIntegration) tfsdk.Plan {
	t.Helper()
	p := tfsdk.Plan{Schema: sch, Raw: emptyAwsIntegrationState()}
	diags := p.Set(context.Background(), integration)
	require.False(t, diags.HasError(), "failed to build test plan: %s", diags)
	return p
}

func stateForTest(t *testing.T, sch schema.Schema, integration model.AwsIntegration) tfsdk.State {
	t.Helper()
	s := tfsdk.State{Schema: sch, Raw: emptyAwsIntegrationState()}
	diags := s.Set(context.Background(), integration)
	require.False(t, diags.HasError(), "failed to build test state: %s", diags)
	return s
}

func TestAwsIntegrationResource_Create_Success(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	integration := model.AwsIntegration{
		Dataset:                types.StringValue("default"),
		ExternalID:             types.StringValue("org-1"),
		AwsAccountID:           types.StringValue("123456789012"),
		ReadOnlyRoleArn:        types.StringValue("arn:aws:iam::123456789012:role/readonly"),
		InstrumentationRoleArn: types.StringNull(),
	}

	c.On("CreateOrUpdateAwsIntegration", mock.Anything, mock.MatchedBy(func(i model.AwsIntegration) bool {
		return i.Dataset.ValueString() == "default" &&
			i.ExternalID.ValueString() == "org-1" &&
			i.AwsAccountID.ValueString() == "123456789012"
	})).Return(nil)

	resp := &resource.CreateResponse{
		State: tfsdk.State{Schema: sch, Raw: emptyAwsIntegrationState()},
	}
	r.Create(context.Background(), resource.CreateRequest{Plan: planForTest(t, sch, integration)}, resp)

	assert.False(t, resp.Diagnostics.HasError(), "diags: %s", resp.Diagnostics)

	var result model.AwsIntegration
	resp.State.Get(context.Background(), &result)
	assert.Equal(t, "123456789012-org-1", result.ID.ValueString())
	c.AssertExpectations(t)
}

func TestAwsIntegrationResource_Create_APIError(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	integration := model.AwsIntegration{
		Dataset:         types.StringValue("default"),
		ExternalID:      types.StringValue("org-1"),
		AwsAccountID:    types.StringValue("123456789012"),
		ReadOnlyRoleArn: types.StringValue("arn:aws:iam::123456789012:role/readonly"),
	}

	c.On("CreateOrUpdateAwsIntegration", mock.Anything, mock.Anything).Return(errors.New("boom"))

	resp := &resource.CreateResponse{
		State: tfsdk.State{Schema: sch, Raw: emptyAwsIntegrationState()},
	}
	r.Create(context.Background(), resource.CreateRequest{Plan: planForTest(t, sch, integration)}, resp)

	assert.True(t, resp.Diagnostics.HasError())
}

func TestAwsIntegrationResource_Read_Success(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	state := model.AwsIntegration{
		Dataset:         types.StringValue("default"),
		ExternalID:      types.StringValue("org-1"),
		AwsAccountID:    types.StringValue("123456789012"),
		ReadOnlyRoleArn: types.StringValue("arn:aws:iam::123456789012:role/readonly"),
	}

	apiResp := &model.AwsIntegrationSpec{
		Dataset:   "default",
		AccountID: "123456789012",
		Roles: []model.AwsIntegrationRole{
			{Arn: "arn:aws:iam::123456789012:role/readonly", ExternalID: "org-1", PermissionType: model.PermissionTypeReadOnly},
			{Arn: "arn:aws:iam::123456789012:role/instrumentation", ExternalID: "org-1", PermissionType: model.PermissionTypeResourcesInstrumentation},
		},
	}
	c.On("GetAwsIntegration", mock.Anything, "default", "123456789012", "org-1").Return(apiResp, nil)

	resp := &resource.ReadResponse{
		State: tfsdk.State{Schema: sch, Raw: emptyAwsIntegrationState()},
	}
	r.Read(context.Background(), resource.ReadRequest{State: stateForTest(t, sch, state)}, resp)

	assert.False(t, resp.Diagnostics.HasError(), "diags: %s", resp.Diagnostics)

	var result model.AwsIntegration
	resp.State.Get(context.Background(), &result)
	assert.Equal(t, "arn:aws:iam::123456789012:role/readonly", result.ReadOnlyRoleArn.ValueString())
	assert.Equal(t, "arn:aws:iam::123456789012:role/instrumentation", result.InstrumentationRoleArn.ValueString())
}

func TestAwsIntegrationResource_Read_NotFound_RemovesFromState(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	state := model.AwsIntegration{
		Dataset:         types.StringValue("default"),
		ExternalID:      types.StringValue("org-1"),
		AwsAccountID:    types.StringValue("123456789012"),
		ReadOnlyRoleArn: types.StringValue("arn:aws:iam::123456789012:role/readonly"),
	}

	c.On("GetAwsIntegration", mock.Anything, "default", "123456789012", "org-1").
		Return((*model.AwsIntegrationSpec)(nil), &client.APIError{StatusCode: 404, Body: "not found"})

	resp := &resource.ReadResponse{
		State: stateForTest(t, sch, state),
	}
	r.Read(context.Background(), resource.ReadRequest{State: stateForTest(t, sch, state)}, resp)

	assert.False(t, resp.Diagnostics.HasError())
	// When not found, State.RemoveResource is called which nullifies the raw.
	assert.True(t, resp.State.Raw.IsNull())
}

func TestAwsIntegrationResource_Read_APIError(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	state := model.AwsIntegration{
		Dataset:         types.StringValue("default"),
		ExternalID:      types.StringValue("org-1"),
		AwsAccountID:    types.StringValue("123456789012"),
		ReadOnlyRoleArn: types.StringValue("arn:aws:iam::123456789012:role/readonly"),
	}

	c.On("GetAwsIntegration", mock.Anything, "default", "123456789012", "org-1").
		Return((*model.AwsIntegrationSpec)(nil), errors.New("boom"))

	resp := &resource.ReadResponse{
		State: stateForTest(t, sch, state),
	}
	r.Read(context.Background(), resource.ReadRequest{State: stateForTest(t, sch, state)}, resp)

	assert.True(t, resp.Diagnostics.HasError())
}

func TestAwsIntegrationResource_Update_Success(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	plan := model.AwsIntegration{
		Dataset:                types.StringValue("default"),
		ExternalID:             types.StringValue("org-1"),
		AwsAccountID:           types.StringValue("123456789012"),
		ReadOnlyRoleArn:        types.StringValue("arn:aws:iam::123456789012:role/readonly"),
		InstrumentationRoleArn: types.StringValue("arn:aws:iam::123456789012:role/instrumentation-new"),
	}

	c.On("CreateOrUpdateAwsIntegration", mock.Anything, mock.Anything).Return(nil)

	resp := &resource.UpdateResponse{
		State: tfsdk.State{Schema: sch, Raw: emptyAwsIntegrationState()},
	}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  planForTest(t, sch, plan),
		State: stateForTest(t, sch, plan),
	}, resp)

	assert.False(t, resp.Diagnostics.HasError())
}

func TestAwsIntegrationResource_Delete_Success(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	state := model.AwsIntegration{
		Dataset:      types.StringValue("default"),
		ExternalID:   types.StringValue("org-1"),
		AwsAccountID: types.StringValue("123456789012"),
	}

	c.On("DeleteAwsIntegration", mock.Anything, "default", "123456789012", "org-1").Return(nil)

	resp := &resource.DeleteResponse{
		State: stateForTest(t, sch, state),
	}
	r.Delete(context.Background(), resource.DeleteRequest{State: stateForTest(t, sch, state)}, resp)

	assert.False(t, resp.Diagnostics.HasError())
}

func TestAwsIntegrationResource_Delete_NotFoundTolerated(t *testing.T) {
	r, c, sch := buildResourceForCRUDTest(t)

	state := model.AwsIntegration{
		Dataset:      types.StringValue("default"),
		ExternalID:   types.StringValue("org-1"),
		AwsAccountID: types.StringValue("123456789012"),
	}

	c.On("DeleteAwsIntegration", mock.Anything, "default", "123456789012", "org-1").Return(&client.APIError{StatusCode: 404, Body: "not found"})

	resp := &resource.DeleteResponse{
		State: stateForTest(t, sch, state),
	}
	r.Delete(context.Background(), resource.DeleteRequest{State: stateForTest(t, sch, state)}, resp)

	assert.False(t, resp.Diagnostics.HasError())
}

// --- Import tests ---

func TestImportState(t *testing.T) {
	tests := []struct {
		name          string
		importID      string
		expectDataset string
		expectAccount string
		expectExtID   string
		expectError   bool
	}{
		{
			name:          "valid three-part id",
			importID:      "default,123456789012,org-tech-id",
			expectDataset: "default",
			expectAccount: "123456789012",
			expectExtID:   "org-tech-id",
		},
		{
			name:        "two parts is invalid",
			importID:    "default,org-tech-id",
			expectError: true,
		},
		{
			name:        "one part is invalid",
			importID:    "only-one",
			expectError: true,
		},
		{
			name:        "four parts is invalid",
			importID:    "a,b,c,d",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &AwsIntegrationResource{}
			sch := awsIntegrationSchema()

			resp := &resource.ImportStateResponse{
				State: tfsdk.State{Schema: sch, Raw: emptyAwsIntegrationState()},
			}
			r.ImportState(context.Background(), resource.ImportStateRequest{ID: tc.importID}, resp)

			if tc.expectError {
				assert.True(t, resp.Diagnostics.HasError())
				return
			}

			assert.False(t, resp.Diagnostics.HasError())
			var state model.AwsIntegration
			diags := resp.State.Get(context.Background(), &state)
			require.False(t, diags.HasError())
			assert.Equal(t, tc.expectDataset, state.Dataset.ValueString())
			assert.Equal(t, tc.expectAccount, state.AwsAccountID.ValueString())
			assert.Equal(t, tc.expectExtID, state.ExternalID.ValueString())
			assert.Equal(t, tc.expectAccount+"-"+tc.expectExtID, state.ID.ValueString())
		})
	}
}
