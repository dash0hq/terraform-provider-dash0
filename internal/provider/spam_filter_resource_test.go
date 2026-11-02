package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

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
	mockClient.On("GetDataset", mock.Anything, "dataset-1").Return("{}", nil)

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

// spamFilterYAMLPlanModifier returns the plan modifier the resource schema
// actually attaches to spam_filter_yaml, so the tests below exercise the
// wiring rather than a hand-built modifier.
func spamFilterYAMLPlanModifier(t *testing.T) planmodifier.String {
	t.Helper()
	resp := &resource.SchemaResponse{}
	(&SpamFilterResource{}).Schema(context.Background(), resource.SchemaRequest{}, resp)
	attr, ok := resp.Schema.Attributes["spam_filter_yaml"].(schema.StringAttribute)
	require.True(t, ok)
	require.Len(t, attr.PlanModifiers, 1)
	return attr.PlanModifiers[0]
}

func spamFilterYAMLWithEnabled(enabled string) string {
	annotations := ""
	if enabled != "" {
		annotations = "\n  annotations:\n    dash0.com/enabled: \"" + enabled + "\""
	}
	return `apiVersion: v1alpha2
kind: Dash0SpamFilter
metadata:
  name: noisy` + annotations + `
spec:
  context: log
  filter:
    - key: body
      operator: contains
      value: spam
`
}

// The plan modifier used to strip every annotation, so switching a filter off
// produced an empty plan and the filter could never be disabled.
func TestSpamFilterResource_PlanModifier_EnabledAnnotation(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		state    string
		wantDiff bool
	}{
		{name: "disabling plans an update", config: "false", state: "true", wantDiff: true},
		{name: "re-enabling plans an update", config: "true", state: "false", wantDiff: true},
		{name: "omitting re-enables a filter disabled elsewhere", config: "", state: "false", wantDiff: true},
		{name: "omitted equals the server's explicit true", config: "", state: "true", wantDiff: false},
		{name: "unchanged false", config: "false", state: "false", wantDiff: false},
	}

	modifier := spamFilterYAMLPlanModifier(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configValue := types.StringValue(spamFilterYAMLWithEnabled(tt.config))
			stateValue := types.StringValue(spamFilterYAMLWithEnabled(tt.state))
			resp := &planmodifier.StringResponse{PlanValue: configValue}

			modifier.PlanModifyString(context.Background(), planmodifier.StringRequest{
				ConfigValue: configValue,
				StateValue:  stateValue,
				PlanValue:   configValue,
			}, resp)

			if tt.wantDiff {
				assert.Equal(t, configValue, resp.PlanValue, "the config must win so Terraform plans an update")
			} else {
				assert.Equal(t, stateValue, resp.PlanValue, "semantically equal values must keep the state value")
			}
		})
	}
}

func spamFilterTestSchema() schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"origin":           schema.StringAttribute{Computed: true},
		"id":               schema.StringAttribute{Computed: true},
		"dataset":          schema.StringAttribute{Optional: true, Computed: true},
		"spam_filter_yaml": schema.StringAttribute{Required: true},
	}}
}

func spamFilterTestValue(spamFilterYAML string) tftypes.Value {
	return tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"origin":           tftypes.String,
			"id":               tftypes.String,
			"dataset":          tftypes.String,
			"spam_filter_yaml": tftypes.String,
		}},
		map[string]tftypes.Value{
			"origin":           tftypes.NewValue(tftypes.String, "tf_origin"),
			"id":               tftypes.NewValue(tftypes.String, nil),
			"dataset":          tftypes.NewValue(tftypes.String, "dataset-1"),
			"spam_filter_yaml": tftypes.NewValue(tftypes.String, spamFilterYAML),
		},
	)
}

// A filter switched off in the UI must show up as drift on the next refresh.
func TestSpamFilterResource_Read_DetectsDisabledOutOfBand(t *testing.T) {
	apiResponse := `{"apiVersion":"v1alpha2","kind":"Dash0SpamFilter","metadata":{"name":"noisy","annotations":{"dash0.com/enabled":"false"}},"spec":{"context":"log","filter":[{"key":"body","operator":"contains","value":"spam"}]}}`

	tests := []struct {
		name       string
		stateYAML  string
		wantUpdate bool
	}{
		{name: "state omits enabled", stateYAML: spamFilterYAMLWithEnabled(""), wantUpdate: true},
		{name: "state says true", stateYAML: spamFilterYAMLWithEnabled("true"), wantUpdate: true},
		{name: "state already false", stateYAML: spamFilterYAMLWithEnabled("false"), wantUpdate: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{}
			mockClient.On("GetSpamFilter", mock.Anything, "tf_origin", "dataset-1").Return(apiResponse, nil)

			state := tfsdk.State{Raw: spamFilterTestValue(tt.stateYAML), Schema: spamFilterTestSchema()}
			resp := &resource.ReadResponse{State: state}
			(&SpamFilterResource{client: mockClient}).Read(context.Background(), resource.ReadRequest{State: state}, resp)
			require.False(t, resp.Diagnostics.HasError(), resp.Diagnostics)

			var got spamFilterModel
			resp.State.Get(context.Background(), &got)
			if tt.wantUpdate {
				assert.Equal(t, apiResponse, got.SpamFilterYaml.ValueString(), "state must take the API response so the plan shows the drift")
			} else {
				assert.Equal(t, tt.stateYAML, got.SpamFilterYaml.ValueString())
			}
			mockClient.AssertExpectations(t)
		})
	}
}

func spamFilterValidateConfig(spamFilterYAML string) *resource.ValidateConfigResponse {
	resp := &resource.ValidateConfigResponse{}
	(&SpamFilterResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: spamFilterTestValue(spamFilterYAML), Schema: spamFilterTestSchema()},
	}, resp)
	return resp
}

func TestSpamFilterResource_ValidateConfig(t *testing.T) {
	assert.False(t, spamFilterValidateConfig(spamFilterYAMLWithEnabled("")).Diagnostics.HasError())
	assert.False(t, spamFilterValidateConfig(spamFilterYAMLWithEnabled("false")).Diagnostics.HasError())
	assert.False(t, spamFilterValidateConfig(spamFilterYAMLWithEnabled("true")).Diagnostics.HasError())
	assert.True(t, spamFilterValidateConfig("metadata: [").Diagnostics.HasError())

	for _, value := range []string{"false", "true", `"no"`, "0"} {
		t.Run("rejects enabled "+value, func(t *testing.T) {
			yamlDoc := strings.Replace(spamFilterYAMLWithEnabled("false"), `dash0.com/enabled: "false"`, "dash0.com/enabled: "+value, 1)
			diags := spamFilterValidateConfig(yamlDoc).Diagnostics
			require.True(t, diags.HasError(), "an unquoted or unknown value would fail later with an opaque decoding error")
			assert.Contains(t, diags.Errors()[0].Detail(), `"true" or "false"`)
		})
	}
}

func TestSpamFilterResource_ValidateConfig_CustomLabelsWarn(t *testing.T) {
	yamlDoc := strings.Replace(spamFilterYAMLWithEnabled(""), "  name: noisy", "  name: noisy\n  labels:\n    custom:\n      team: platform", 1)
	diags := spamFilterValidateConfig(yamlDoc).Diagnostics

	assert.False(t, diags.HasError(), "custom labels are a warning, not an error")
	require.Equal(t, 1, diags.WarningsCount())
	assert.Contains(t, diags.Warnings()[0].Summary(), "metadata.labels.custom")
	assert.Contains(t, diags.Warnings()[0].Detail(), "team")
}
