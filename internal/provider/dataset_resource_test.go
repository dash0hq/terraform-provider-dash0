package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const datasetTestYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: Production
  labels:
    dash0.com/id: production
spec:
  retention:
    traces: 30d`

func datasetTestSchema() resource.SchemaResponse {
	resp := resource.SchemaResponse{}
	(&DatasetResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp
}

// datasetRaw builds a dataset object value. nil origin/id become null; use
// tftypes.UnknownValue to build an unknown.
func datasetRaw(origin, id interface{}, datasetYaml string) tftypes.Value {
	return tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"origin": tftypes.String, "id": tftypes.String, "dataset_yaml": tftypes.String,
		}},
		map[string]tftypes.Value{
			"origin":       tftypes.NewValue(tftypes.String, origin),
			"id":           tftypes.NewValue(tftypes.String, id),
			"dataset_yaml": tftypes.NewValue(tftypes.String, datasetYaml),
		},
	)
}

func datasetState(origin, id interface{}, datasetYaml string) tfsdk.State {
	return tfsdk.State{Raw: datasetRaw(origin, id, datasetYaml), Schema: datasetTestSchema().Schema}
}

func TestDatasetResource_Metadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewDatasetResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_dataset", resp.TypeName)
}

func TestDatasetResource_Schema(t *testing.T) {
	s := datasetTestSchema().Schema
	assert.NotContains(t, s.Attributes, "dataset")
	assert.True(t, s.Attributes["origin"].IsComputed())
	assert.True(t, s.Attributes["id"].IsComputed())
	assert.True(t, s.Attributes["dataset_yaml"].IsRequired())
	assert.NotContains(t, s.Attributes, "deletion_protection")
	assert.Contains(t, s.Description, "30 days")
}

func TestDatasetSlugFromYAML(t *testing.T) {
	slug, err := datasetSlugFromYAML(datasetTestYaml)
	assert.NoError(t, err)
	assert.Equal(t, "production", slug)

	for _, bad := range []string{
		"apiVersion: dash0.com/v1alpha1\nkind: Dash0Dataset\nmetadata:\n  name: x",
		"apiVersion: dash0.com/v1alpha1\nkind: Dash0Dataset\nmetadata:\n  labels:\n    dash0.com/id: \"\"",
		"kind: [",
	} {
		_, err := datasetSlugFromYAML(bad)
		assert.Error(t, err, bad)
	}
}

func TestDatasetResource_Create_StampsOriginAndSlug(t *testing.T) {
	mockClient := &MockClient{}
	r := &DatasetResource{client: mockClient}
	mockClient.On("CreateDataset", mock.Anything,
		mock.MatchedBy(func(o string) bool { return strings.HasPrefix(o, "tf_") }),
		mock.AnythingOfType("string")).Return(nil)

	req := resource.CreateRequest{Plan: tfsdk.Plan{
		Raw:    datasetRaw(tftypes.UnknownValue, "production", datasetTestYaml),
		Schema: datasetTestSchema().Schema,
	}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: datasetTestSchema().Schema, Raw: datasetRaw(nil, nil, "")}}

	r.Create(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	var got datasetModel
	resp.State.Get(context.Background(), &got)
	assert.True(t, strings.HasPrefix(got.Origin.ValueString(), "tf_"))
	assert.Equal(t, "production", got.ID.ValueString())
	assert.Equal(t, datasetTestYaml, got.DatasetYaml.ValueString())
	mockClient.AssertExpectations(t)
}

// Permissions go in the create body, with no follow-up update: the mock has no
// UpdateDataset expectation, so a call to it fails the test.
func TestDatasetResource_Create_SendsPermissionsInCreateBody(t *testing.T) {
	mockClient := &MockClient{}
	mockClient.On("CreateDataset", mock.Anything, mock.Anything,
		mock.MatchedBy(func(body string) bool { return strings.Contains(body, "team-a") })).Return(nil)

	req := resource.CreateRequest{Plan: tfsdk.Plan{Raw: datasetRaw(tftypes.UnknownValue, "production", datasetTestYaml+datasetTestPermissions), Schema: datasetTestSchema().Schema}}
	resp := &resource.CreateResponse{State: datasetState(nil, nil, "")}

	(&DatasetResource{client: mockClient}).Create(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	mockClient.AssertExpectations(t)
}

func TestDatasetResource_Create_MissingSlug(t *testing.T) {
	mockClient := &MockClient{}
	r := &DatasetResource{client: mockClient}

	req := resource.CreateRequest{Plan: tfsdk.Plan{
		Raw:    datasetRaw(tftypes.UnknownValue, tftypes.UnknownValue, "apiVersion: dash0.com/v1alpha1\nkind: Dash0Dataset\nmetadata:\n  name: x"),
		Schema: datasetTestSchema().Schema,
	}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: datasetTestSchema().Schema, Raw: datasetRaw(nil, nil, "")}}

	r.Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "dash0.com/id")
	mockClient.AssertNotCalled(t, "CreateDataset", mock.Anything, mock.Anything, mock.Anything)
}

func TestDatasetResource_Create_ClientError(t *testing.T) {
	mockClient := &MockClient{}
	r := &DatasetResource{client: mockClient}
	mockClient.On("CreateDataset", mock.Anything, mock.Anything, mock.Anything).Return(errServer)

	req := resource.CreateRequest{Plan: tfsdk.Plan{
		Raw:    datasetRaw(tftypes.UnknownValue, "production", datasetTestYaml),
		Schema: datasetTestSchema().Schema,
	}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: datasetTestSchema().Schema, Raw: datasetRaw(nil, nil, "")}}

	r.Create(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull() || resp.State.Raw.Equal(datasetRaw(nil, nil, "")))
}

func TestDatasetResource_Update_UsesStateSlugAndOrigin(t *testing.T) {
	mockClient := &MockClient{}
	r := &DatasetResource{client: mockClient}
	mockClient.On("UpdateDataset", mock.Anything, "production", "tf_abc", mock.AnythingOfType("string")).Return(nil)

	newYaml := strings.Replace(datasetTestYaml, "30d", "90d", 1)
	req := resource.UpdateRequest{
		State: datasetState("tf_abc", "production", datasetTestYaml),
		Plan:  tfsdk.Plan{Raw: datasetRaw(tftypes.UnknownValue, "production", newYaml), Schema: datasetTestSchema().Schema},
	}
	resp := &resource.UpdateResponse{State: datasetState("tf_abc", "production", datasetTestYaml)}

	r.Update(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)
	var got datasetModel
	resp.State.Get(context.Background(), &got)
	assert.Equal(t, "tf_abc", got.Origin.ValueString())
	assert.Equal(t, newYaml, got.DatasetYaml.ValueString())
	mockClient.AssertExpectations(t)
}

func TestDatasetResource_Delete(t *testing.T) {
	t.Run("deletes by slug", func(t *testing.T) {
		mockClient := &MockClient{}
		mockClient.On("DeleteDataset", mock.Anything, "production").Return(nil)
		resp := &resource.DeleteResponse{}
		(&DatasetResource{client: mockClient}).Delete(context.Background(),
			resource.DeleteRequest{State: datasetState("tf_abc", "production", datasetTestYaml)}, resp)
		assert.False(t, resp.Diagnostics.HasError())
		mockClient.AssertExpectations(t)
	})
	t.Run("404 is success", func(t *testing.T) {
		mockClient := &MockClient{}
		mockClient.On("DeleteDataset", mock.Anything, "production").Return(errNotFound)
		resp := &resource.DeleteResponse{}
		(&DatasetResource{client: mockClient}).Delete(context.Background(),
			resource.DeleteRequest{State: datasetState("tf_abc", "production", datasetTestYaml)}, resp)
		assert.False(t, resp.Diagnostics.HasError())
	})
	t.Run("other error fails", func(t *testing.T) {
		mockClient := &MockClient{}
		mockClient.On("DeleteDataset", mock.Anything, "production").Return(errServer)
		resp := &resource.DeleteResponse{}
		(&DatasetResource{client: mockClient}).Delete(context.Background(),
			resource.DeleteRequest{State: datasetState("tf_abc", "production", datasetTestYaml)}, resp)
		assert.True(t, resp.Diagnostics.HasError())
	})
}

func datasetImportResponse() *resource.ImportStateResponse {
	return &resource.ImportStateResponse{State: datasetState(nil, nil, "")}
}

func TestDatasetResource_ImportState(t *testing.T) {
	t.Run("by slug with origin label", func(t *testing.T) {
		mockClient := &MockClient{}
		apiResponse := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0Dataset","metadata":{"name":"Production","labels":{"dash0.com/id":"production","dash0.com/origin":"tf_abc"}}}`
		mockClient.On("GetDataset", mock.Anything, "production").Return(apiResponse, nil)
		resp := datasetImportResponse()

		(&DatasetResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: "production"}, resp)

		assert.False(t, resp.Diagnostics.HasError())
		var got datasetModel
		resp.State.Get(context.Background(), &got)
		assert.Equal(t, "production", got.ID.ValueString())
		assert.Equal(t, "tf_abc", got.Origin.ValueString())
		assert.Equal(t, apiResponse, got.DatasetYaml.ValueString())
	})
	t.Run("without origin label leaves origin null", func(t *testing.T) {
		mockClient := &MockClient{}
		mockClient.On("GetDataset", mock.Anything, "production").Return(`{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0Dataset","metadata":{"name":"Production","labels":{"dash0.com/id":"production"}}}`, nil)
		resp := datasetImportResponse()

		(&DatasetResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: "production"}, resp)

		assert.False(t, resp.Diagnostics.HasError())
		var got datasetModel
		resp.State.Get(context.Background(), &got)
		assert.True(t, got.Origin.IsNull())
		assert.Equal(t, "production", got.ID.ValueString())
	})
	t.Run("by origin stores the slug as id and plans clean", func(t *testing.T) {
		mockClient := &MockClient{}
		apiResponse := `{"apiVersion":"dash0.com/v1alpha1","kind":"Dash0Dataset","metadata":{"name":"Production","labels":{"dash0.com/id":"production","dash0.com/origin":"tf_abc"}}}`
		mockClient.On("GetDataset", mock.Anything, "tf_abc").Return(apiResponse, nil)
		resp := datasetImportResponse()

		(&DatasetResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: "tf_abc"}, resp)

		assert.False(t, resp.Diagnostics.HasError())
		var got datasetModel
		resp.State.Get(context.Background(), &got)
		assert.Equal(t, "production", got.ID.ValueString())
		assert.Equal(t, "tf_abc", got.Origin.ValueString())

		// The next plan, with config that names the same slug, must not replace.
		plan := datasetModifyPlan(t, resp.State.Raw, datasetTestYaml)
		assert.False(t, plan.Diagnostics.HasError())
		assert.Empty(t, plan.RequiresReplace)
	})
	t.Run("response without slug errors", func(t *testing.T) {
		mockClient := &MockClient{}
		mockClient.On("GetDataset", mock.Anything, "tf_abc").Return(`{"kind":"Dash0Dataset","metadata":{"name":"Production"}}`, nil)
		resp := datasetImportResponse()

		(&DatasetResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: "tf_abc"}, resp)

		assert.True(t, resp.Diagnostics.HasError())
	})
	t.Run("unknown slug errors", func(t *testing.T) {
		mockClient := &MockClient{}
		mockClient.On("GetDataset", mock.Anything, "nope").Return("", errNotFound)
		resp := datasetImportResponse()

		(&DatasetResource{client: mockClient}).ImportState(context.Background(), resource.ImportStateRequest{ID: "nope"}, resp)

		assert.True(t, resp.Diagnostics.HasError())
		assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "nope")
	})
}

func datasetModifyPlan(t *testing.T, state tftypes.Value, configYaml string) *resource.ModifyPlanResponse {
	t.Helper()
	sch := datasetTestSchema().Schema
	plan := datasetRaw(tftypes.UnknownValue, tftypes.UnknownValue, configYaml)
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Raw: plan, Schema: sch}}
	(&DatasetResource{}).ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Raw: datasetRaw(nil, nil, configYaml), Schema: sch},
		Plan:   tfsdk.Plan{Raw: plan, Schema: sch},
		State:  tfsdk.State{Raw: state, Schema: sch},
	}, resp)
	return resp
}

func TestDatasetResource_ModifyPlan(t *testing.T) {
	nullState := tftypes.NewValue(datasetRaw(nil, nil, "").Type(), nil)

	t.Run("create sets id and does not replace", func(t *testing.T) {
		resp := datasetModifyPlan(t, nullState, datasetTestYaml)
		assert.False(t, resp.Diagnostics.HasError())
		assert.Empty(t, resp.RequiresReplace)
		var id types.String
		resp.Plan.GetAttribute(context.Background(), path.Root("id"), &id)
		assert.Equal(t, "production", id.ValueString())
	})
	t.Run("same slug does not replace", func(t *testing.T) {
		resp := datasetModifyPlan(t, datasetRaw("tf_abc", "production", datasetTestYaml), strings.Replace(datasetTestYaml, "30d", "90d", 1))
		assert.Empty(t, resp.RequiresReplace)
	})
	t.Run("slug change requires replace", func(t *testing.T) {
		resp := datasetModifyPlan(t, datasetRaw("tf_abc", "production", datasetTestYaml),
			strings.Replace(datasetTestYaml, "id: production", "id: staging", 1))
		assert.Equal(t, path.Paths{path.Root("id")}, resp.RequiresReplace)
	})
}

// datasetPlannedYaml runs the dataset_yaml plan modifiers and returns the planned value.
func datasetPlannedYaml(t *testing.T, stateYaml, configYaml string) string {
	t.Helper()
	req := planmodifier.StringRequest{ConfigValue: types.StringValue(configYaml), StateValue: types.StringValue(stateYaml), PlanValue: types.StringValue(configYaml)}
	v, _ := runStringPlanModifiers(context.Background(), datasetTestSchema().Schema.Attributes["dataset_yaml"].(schema.StringAttribute).PlanModifiers, req)
	return v.ValueString()
}

func TestDatasetResource_PlanPermissions(t *testing.T) {
	state := datasetTestYaml + datasetTestPermissions
	t.Run("permissions-only change is planned", func(t *testing.T) {
		config := strings.Replace(state, "user_1", "user_2", 1)
		assert.Equal(t, config, datasetPlannedYaml(t, state, config))
	})
	t.Run("entry and action order is not a change", func(t *testing.T) {
		config := datasetTestYaml + "\n  permissions:\n    - userId: user_1\n      actions: [dataset:read]\n    - teamId: team-a\n      actions: [dataset:delete, dataset:read]"
		assert.Equal(t, state, datasetPlannedYaml(t, state, config))
	})
	t.Run("permissions absent from config are not a change", func(t *testing.T) {
		assert.Equal(t, state, datasetPlannedYaml(t, state, datasetTestYaml))
	})
}

func TestDatasetResource_ValidateConfig(t *testing.T) {
	validate := func(y string) *resource.ValidateConfigResponse {
		resp := &resource.ValidateConfigResponse{}
		(&DatasetResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
			Config: tfsdk.Config{Raw: datasetRaw(nil, nil, y), Schema: datasetTestSchema().Schema},
		}, resp)
		return resp
	}

	assert.False(t, validate(datasetTestYaml).Diagnostics.HasError())
	assert.Equal(t, 0, validate(datasetTestYaml).Diagnostics.WarningsCount())
	assert.True(t, validate("kind: [").Diagnostics.HasError())
	assert.True(t, validate("kind: Dash0Team\nmetadata:\n  name: x").Diagnostics.HasError())
	assert.True(t, validate("apiVersion: dash0.com/v1alpha1\nkind: Dash0Dataset\nmetadata:\n  name: x").Diagnostics.HasError())
	assert.Empty(t, validate(datasetTestYaml+datasetTestPermissions).Diagnostics)
	assert.Empty(t, validate(datasetTestYaml+"\n  permissions: []").Diagnostics)
	writeGrant := validate(datasetTestYaml + "\n  permissions:\n    - teamId: team-a\n      actions: [dataset:read, dataset:write]").Diagnostics
	assert.True(t, writeGrant.HasError())
	assert.Contains(t, writeGrant.Errors()[0].Detail(), "spec.permissions[0] grants dataset:write")
	// telemetryFilters left the dataset document; it is no longer special-cased.
	assert.Equal(t, 0, validate(datasetTestYaml+"\n  telemetryFilters: []").Diagnostics.WarningsCount())
}

// datasetTestProvider registers only dash0_dataset so a test can drive the
// real framework plan pipeline without credentials.
type datasetTestProvider struct{}

func (datasetTestProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "dash0"
}
func (datasetTestProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {
}
func (datasetTestProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}
func (datasetTestProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }
func (datasetTestProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewDatasetResource}
}

// TestDatasetResource_PlanResourceChange_SlugChangeReplacesID runs the full
// framework plan: attribute plan modifiers first, then ModifyPlan. The
// semantic-equal modifier ignores labels, so a slug-only edit leaves
// dataset_yaml unchanged; the replace must therefore target id.
func TestDatasetResource_PlanResourceChange_SlugChangeReplacesID(t *testing.T) {
	ctx := context.Background()
	srv := providerserver.NewProtocol6(datasetTestProvider{})()

	objType := datasetRaw(nil, nil, "").Type()
	dv := func(v tftypes.Value) *tfprotov6.DynamicValue {
		d, err := tfprotov6.NewDynamicValue(objType, v)
		assert.NoError(t, err)
		return &d
	}

	renamed := strings.Replace(datasetTestYaml, "id: production", "id: staging", 1)
	prior := datasetRaw("tf_abc", "production", datasetTestYaml)
	resp, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "dash0_dataset",
		PriorState:       dv(prior),
		ProposedNewState: dv(datasetRaw("tf_abc", "production", renamed)),
		Config:           dv(datasetRaw(nil, nil, renamed)),
	})
	assert.NoError(t, err)
	assert.Empty(t, resp.Diagnostics)

	planned, err := resp.PlannedState.Unmarshal(objType)
	assert.NoError(t, err)
	var attrs map[string]tftypes.Value
	assert.NoError(t, planned.As(&attrs))
	var yamlPlan, idPlan string
	assert.NoError(t, attrs["dataset_yaml"].As(&yamlPlan))
	assert.NoError(t, attrs["id"].As(&idPlan))

	assert.Equal(t, datasetTestYaml, yamlPlan, "precondition: dataset_yaml shows no change")
	assert.Equal(t, "staging", idPlan)
	assert.Equal(t, []*tftypes.AttributePath{tftypes.NewAttributePath().WithAttributeName("id")}, resp.RequiresReplace)
}
