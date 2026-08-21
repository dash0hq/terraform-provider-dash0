package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	customplanmodifier "github.com/dash0hq/terraform-provider-dash0/internal/provider/planmodifier"
)

// TestSpamFilterResource_Read_404IsNotHandledAsGone covers the bug reported in
// https://github.com/dash0hq/terraform-provider-dash0/issues/164 (spam filter
// deleted out-of-band via the UI, then a Terraform plan/apply runs Read).
//
// SpamFilterResource.Read now checks dash0.IsNotFound(err) and calls
// resp.State.RemoveResource(ctx), so Terraform treats the resource as gone and
// proposes to re-create it on the next apply, instead of surfacing a hard
// error that blocks `terraform plan`/`apply` until the user manually runs
// `terraform state rm`.
func TestSpamFilterResource_Read_404IsNotHandledAsGone(t *testing.T) {
	mockClient := &MockClient{}
	r := &SpamFilterResource{client: mockClient}

	notFound := &dash0.APIError{
		StatusCode: 404,
		Status:     "404 Not Found",
		Message:    "spam filter not found",
		TraceID:    "repro-trace-id",
	}
	mockClient.On("GetSpamFilter", mock.Anything, "tf_origin", "dataset-1").
		Return("", notFound)

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
				"spam_filter_yaml": tftypes.NewValue(tftypes.String, "test-yaml"),
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

	// The real terraform-plugin-framework runtime always seeds
	// ReadResponse.State from the request state (schema included) before
	// calling Resource.Read — see internal/fwserver/server_readresource.go.
	// Mirror that here so RemoveResource(ctx), which needs the schema to
	// build the null value, has one to work with.
	req := resource.ReadRequest{State: state}
	resp := &resource.ReadResponse{State: state}

	r.Read(context.Background(), req, resp)

	// What SHOULD happen, matching team_resource.go's dash0.IsNotFound branch:
	assert.False(t, resp.Diagnostics.HasError(),
		"a 404 from GetSpamFilter should be treated as 'resource is gone', not surfaced as a hard error")
	assert.True(t, resp.State.Raw.IsNull(),
		"state should be removed via RemoveResource so terraform plan can recreate the spam filter")

	mockClient.AssertExpectations(t)
}

// TestSpamFilterResource_SharingAnnotationIgnored verifies that spam filters
// do NOT preserve dash0.com/sharing — changes to it should not trigger a replan.
func TestSpamFilterResource_SharingAnnotationIgnored(t *testing.T) {
	modifier := customplanmodifier.YAMLSemanticEqual() // no preserved annotation keys

	configValue := types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: all-users
spec:
  jsonPath: $.body
  matchType: contains
  matchValue: spam
`)
	stateValue := types.StringValue(`
metadata:
  annotations:
    dash0.com/sharing: private
spec:
  jsonPath: $.body
  matchType: contains
  matchValue: spam
`)

	req := planmodifier.StringRequest{
		ConfigValue: configValue,
		StateValue:  stateValue,
		PlanValue:   configValue,
	}
	resp := &planmodifier.StringResponse{
		PlanValue: configValue,
	}

	modifier.PlanModifyString(context.Background(), req, resp)

	assert.Equal(t, stateValue, resp.PlanValue,
		"Should use state value when dash0.com/sharing is not in the preserved list (spam filter)")
}
