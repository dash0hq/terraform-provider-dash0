package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestViewResource_ImportStateStoresYAML covers the ImportState half of
// dash0hq/terraform-provider-dash0#170. Import has no prior state value to align
// against, so it renders with the package's own spelling, but it must still
// store YAML rather than the JSON string the client wrapper returns. Otherwise
// `terraform plan -generate-config-out` emits a jsonencode block and the first
// plan after an import shows a full-document replacement.
//
// All six resources share this wiring line by line; view stands in for them.
func TestViewResource_ImportStateStoresYAML(t *testing.T) {
	mockClient := &MockClient{}
	r := &ViewResource{client: mockClient}

	apiResponse := `{"kind":"Dash0View","metadata":{"name":"web"},"spec":{"type":"spans","display":{"name":"Web"}}}`
	mockClient.On("GetView", mock.Anything, "tf_origin", "default").Return(apiResponse, nil)
	mockClient.On("ResolveView", mock.Anything, "tf_origin", "default").
		Return("00000000-0000-0000-0000-000000000001", "https://app.dash0.com/goto/x", nil)

	attributeTypes := map[string]tftypes.Type{
		"origin":    tftypes.String,
		"id":        tftypes.String,
		"dataset":   tftypes.String,
		"view_yaml": tftypes.String,
		"url":       tftypes.String,
	}
	stateSchema := schema.Schema{
		Attributes: map[string]schema.Attribute{
			"origin":    schema.StringAttribute{Computed: true},
			"id":        schema.StringAttribute{Computed: true},
			"dataset":   schema.StringAttribute{Required: true},
			"view_yaml": schema.StringAttribute{Required: true},
			"url":       schema.StringAttribute{Computed: true},
		},
	}

	resp := &resource.ImportStateResponse{
		State: tfsdk.State{
			Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: attributeTypes}, map[string]tftypes.Value{
				"origin":    tftypes.NewValue(tftypes.String, nil),
				"id":        tftypes.NewValue(tftypes.String, nil),
				"dataset":   tftypes.NewValue(tftypes.String, nil),
				"view_yaml": tftypes.NewValue(tftypes.String, nil),
				"url":       tftypes.NewValue(tftypes.String, nil),
			}),
			Schema: stateSchema,
		},
	}

	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "default,tf_origin"}, resp)
	assert.False(t, resp.Diagnostics.HasError())

	var state viewModel
	resp.State.Get(context.Background(), &state)

	assert.Equal(t, "tf_origin", state.Origin.ValueString())
	assert.Equal(t, "default", state.Dataset.ValueString())
	assert.Equal(t, `kind: Dash0View
metadata:
  name: web
spec:
  type: spans
  display:
    name: Web
`, state.ViewYaml.ValueString())

	mockClient.AssertExpectations(t)
}
