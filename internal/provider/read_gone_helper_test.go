package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRemovedOutOfBand(t *testing.T) {
	tombstone := softDeletedJSON
	tests := []struct {
		name        string
		body        string
		err         error
		wantRemoved bool
	}{
		{"404 removes", "", errNotFound, true},
		{"wrapped 404 removes", "", fmt.Errorf("get: %w", errNotFound), true},
		{"other error is left to the caller", "", errServer, false},
		{"other error ignores a tombstone body", tombstone, errServer, false},
		{"live asset is kept", `{"metadata":{"annotations":{"dash0.com/updated-at":"x"}}}`, nil, false},
		{"soft-deleted asset removes", tombstone, nil, true},
		{"empty deleted-at is kept", `{"metadata":{"annotations":{"dash0.com/deleted-at":""}}}`, nil, false},
		{"unparseable body is kept", "{not json", nil, false},
		{"body without metadata is kept", `{"spec":{}}`, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch := schema.Schema{Attributes: map[string]schema.Attribute{"origin": schema.StringAttribute{Required: true}}}
			objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"origin": tftypes.String}}
			state := tfsdk.State{
				Raw:    tftypes.NewValue(objType, map[string]tftypes.Value{"origin": tftypes.NewValue(tftypes.String, "tf_x")}),
				Schema: sch,
			}
			resp := &resource.ReadResponse{State: state}

			got := removedOutOfBand(context.Background(), resp, "Thing", "tf_x", tt.body, tt.err)

			assert.Equal(t, tt.wantRemoved, got)
			assert.Equal(t, tt.wantRemoved, resp.State.Raw.IsNull(), "state is cleared exactly when removed")
			assert.False(t, resp.Diagnostics.HasError(), "the helper never adds errors")
		})
	}
}

func TestRemovedOutOfBandInDataset(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		err         error
		dsErr       error // nil: dataset exists; unused when no dataset lookup happens
		wantLookup  bool
		wantRemoved bool // state cleared
		wantErrDiag bool
		wantWarn    bool
	}{
		{"404 in existing dataset removes", "", errNotFound, nil, true, true, false, false},
		{"404 in deleted dataset keeps and warns", "", errNotFound, errNotFound, true, false, false, true},
		{"404 with failing dataset check keeps and errors", "", errNotFound, errServer, true, false, true, false},
		{"tombstone removes without a dataset lookup", softDeletedJSON, nil, nil, false, true, false, false},
		{"other error is left to the caller", "", errServer, nil, false, false, false, false},
		{"live asset is kept", `{"spec":{}}`, nil, nil, false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch := schema.Schema{Attributes: map[string]schema.Attribute{"origin": schema.StringAttribute{Required: true}}}
			objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"origin": tftypes.String}}
			state := tfsdk.State{
				Raw:    tftypes.NewValue(objType, map[string]tftypes.Value{"origin": tftypes.NewValue(tftypes.String, "tf_x")}),
				Schema: sch,
			}
			resp := &resource.ReadResponse{State: state}
			m := &MockClient{}
			if tt.wantLookup {
				m.On("GetDataset", mock.Anything, "ds").Return("{}", tt.dsErr).Once()
			}

			got := removedOutOfBandInDataset(context.Background(), resp, m, "ds", "Thing", "tf_x", tt.body, tt.err)

			assert.Equal(t, tt.wantRemoved || tt.wantLookup && tt.dsErr != nil, got, "true when the caller must return")
			assert.Equal(t, tt.wantRemoved, resp.State.Raw.IsNull())
			assert.Equal(t, tt.wantErrDiag, resp.Diagnostics.HasError())
			assert.Equal(t, tt.wantWarn, resp.Diagnostics.WarningsCount() == 1)
			m.AssertExpectations(t)
		})
	}
}

// An empty dataset slug (import ID ",origin") must not trigger GetDataset: the
// request would hit the dataset list endpoint, which always returns 200.
func TestRemovedOutOfBandInDataset_EmptyDatasetSkipsLookup(t *testing.T) {
	sch := schema.Schema{Attributes: map[string]schema.Attribute{"origin": schema.StringAttribute{Required: true}}}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"origin": tftypes.String}}
	state := tfsdk.State{
		Raw:    tftypes.NewValue(objType, map[string]tftypes.Value{"origin": tftypes.NewValue(tftypes.String, "tf_x")}),
		Schema: sch,
	}
	resp := &resource.ReadResponse{State: state}
	m := &MockClient{} // no expectations: any call panics

	got := removedOutOfBandInDataset(context.Background(), resp, m, "", "Thing", "tf_x", "", errNotFound)

	assert.True(t, got)
	assert.True(t, resp.State.Raw.IsNull(), "asset is dropped on 404")
	assert.False(t, resp.Diagnostics.HasError())
	m.AssertNotCalled(t, "GetDataset", mock.Anything, mock.Anything)
}
