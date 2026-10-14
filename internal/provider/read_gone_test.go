package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dash0 "github.com/dash0hq/dash0-api-client-go"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// softDeletedJSON is a minimal soft-delete tombstone as the API returns it:
// 200 with metadata.annotations["dash0.com/deleted-at"] set.
const softDeletedJSON = `{"metadata":{"annotations":{"dash0.com/deleted-at":"2026-01-01T00:00:00Z"}}}`

var (
	errNotFound = &dash0.APIError{StatusCode: 404, Status: "404 Not Found"}
	errServer   = &dash0.APIError{StatusCode: 500, Status: "500 Internal Server Error"}
)

// readWithState runs r.Read against a state built from the resource's real
// schema. Attributes in vals are set as strings; all others are null.
func readWithState(t *testing.T, r resource.Resource, vals map[string]string) resource.ReadResponse {
	t.Helper()
	ctx := context.Background()

	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	objType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	raw := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		if v, set := vals[name]; set {
			raw[name] = tftypes.NewValue(typ, v)
		} else {
			raw[name] = tftypes.NewValue(typ, nil)
		}
	}

	state := tfsdk.State{Raw: tftypes.NewValue(objType, raw), Schema: schemaResp.Schema}
	resp := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &resp)
	return resp
}

// assertReadClearsState checks Read dropped the resource without an error.
func assertReadClearsState(t *testing.T, resp resource.ReadResponse) {
	t.Helper()
	assert.False(t, resp.Diagnostics.HasError(), "must not surface as an error")
	assert.True(t, resp.State.Raw.IsNull(), "state must be cleared")
}

// assertReadKeepsStateAndErrors checks Read failed and left state in place.
func assertReadKeepsStateAndErrors(t *testing.T, resp resource.ReadResponse) {
	t.Helper()
	assert.True(t, resp.Diagnostics.HasError(), "error must surface")
	assert.False(t, resp.State.Raw.IsNull(), "state must be kept")
}

// datasetLookup returns a client whose GetDataset("x") returns err, for the
// dataset check a 404 on a dataset-scoped asset triggers. A nil err means the
// dataset exists.
func datasetLookup(err error) client.Client {
	m := &MockClient{}
	m.On("GetDataset", mock.Anything, "x").Return("{}", err)
	return m
}

// assertReadKeepsStateWithDiag checks Read left state in place and added one
// diagnostic of the wanted severity.
func assertReadKeepsStateWithDiag(t *testing.T, resp resource.ReadResponse, wantErr bool) {
	t.Helper()
	assert.False(t, resp.State.Raw.IsNull(), "state must be kept")
	assert.Equal(t, wantErr, resp.Diagnostics.HasError())
	assert.Equal(t, !wantErr, resp.Diagnostics.WarningsCount() == 1, "a deleted dataset warns")
}
