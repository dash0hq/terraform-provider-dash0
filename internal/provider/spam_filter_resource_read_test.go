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

// TestSpamFilterResource_ReadKeepsPriorKeyOrder guards the same Read wiring the
// other five resources cover in their own ReadWithDiffs tables. The response
// arrives with its keys in a different order than state, which is the only
// shape that shows whether Read passed the prior state value to the converter.
func TestSpamFilterResource_ReadKeepsPriorKeyOrder(t *testing.T) {
	stateYAML := `kind: Dash0SpamFilter
metadata:
  name: noisy-logs
spec:
  enabled: true
  filter: 'severity_text = "DEBUG"'
`
	apiResponse := `{"spec":{"filter":"severity_text = \"TRACE\"","enabled":true},"metadata":{"name":"noisy-logs"},"kind":"Dash0SpamFilter"}`

	mockClient := &MockClient{}
	mockClient.On("GetSpamFilter", mock.Anything, "tf_origin", "dataset-1").Return(apiResponse, nil)
	r := &SpamFilterResource{client: mockClient}

	state := tfsdk.State{
		Raw: tftypes.NewValue(
			tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"origin":           tftypes.String,
					"id":               tftypes.String,
					"dataset":          tftypes.String,
					"spam_filter_yaml": tftypes.String,
				},
			},
			map[string]tftypes.Value{
				"origin":           tftypes.NewValue(tftypes.String, "tf_origin"),
				"id":               tftypes.NewValue(tftypes.String, nil),
				"dataset":          tftypes.NewValue(tftypes.String, "dataset-1"),
				"spam_filter_yaml": tftypes.NewValue(tftypes.String, stateYAML),
			},
		),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"origin":           schema.StringAttribute{Computed: true},
				"id":               schema.StringAttribute{Computed: true},
				"dataset":          schema.StringAttribute{Required: true},
				"spam_filter_yaml": schema.StringAttribute{Required: true},
			},
		},
	}

	req := resource.ReadRequest{State: state}
	resp := &resource.ReadResponse{State: state}

	r.Read(context.Background(), req, resp)
	assert.False(t, resp.Diagnostics.HasError())

	var result spamFilterModel
	resp.State.Get(context.Background(), &result)
	assertYAMLStateRefreshed(t, apiResponse, stateYAML, result.SpamFilterYaml.ValueString())

	mockClient.AssertExpectations(t)
}
