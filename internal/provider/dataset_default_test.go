package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// datasetInheritanceCase describes one dataset-scoped resource for the
// table-driven tests below. Each resource has its own Create path and client
// call, so the table captures just enough to drive Create and read back the
// resolved dataset generically.
type datasetInheritanceCase struct {
	name        string
	newResource func() resource.Resource
	yamlAttr    string
	yamlValue   string
	hasURL      bool
	// mockSetup registers the Create<X>/Resolve<X> expectations for a Create
	// call that is expected to use dataset as the resolved dataset.
	mockSetup func(m *MockClient, dataset string)
}

var datasetInheritanceCases = []datasetInheritanceCase{
	{
		name:        "dashboard",
		newResource: NewDashboardResource,
		yamlAttr:    "dashboard_yaml",
		yamlValue:   "kind: Dashboard\nmetadata:\n  name: system-overview\nspec:\n  title: System Overview",
		hasURL:      true,
		mockSetup: func(m *MockClient, dataset string) {
			m.On("CreateDashboard", mock.Anything, mock.Anything, mock.Anything, dataset).Return(nil)
			m.On("ResolveDashboard", mock.Anything, mock.Anything, dataset).Return("test-id", "", nil)
		},
	},
	{
		name:        "check_rule",
		newResource: NewCheckRuleResource,
		yamlAttr:    "check_rule_yaml",
		yamlValue: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-rule
spec:
  groups:
    - name: TestGroup
      rules:
        - alert: TestAlert
          expr: up == 0
          for: 5m`,
		hasURL: true,
		mockSetup: func(m *MockClient, dataset string) {
			m.On("CreateCheckRule", mock.Anything, mock.Anything, mock.Anything, dataset).Return(nil)
			m.On("ResolveCheckRule", mock.Anything, mock.Anything, dataset).Return("test-id", "", nil)
		},
	},
	{
		name:        "recording_rule",
		newResource: NewRecordingRuleResource,
		yamlAttr:    "recording_rule_yaml",
		yamlValue: `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-recording-rule
spec:
  groups:
    - name: TestGroup
      rules:
        - record: test_metric
          expr: sum(rate(http_requests_total[5m]))`,
		hasURL: false,
		mockSetup: func(m *MockClient, dataset string) {
			m.On("CreateRecordingRule", mock.Anything, mock.Anything, mock.Anything, dataset).Return(nil)
			m.On("ResolveRecordingRule", mock.Anything, mock.Anything, dataset).Return("test-id", nil)
		},
	},
	{
		name:        "spam_filter",
		newResource: NewSpamFilterResource,
		yamlAttr:    "spam_filter_yaml",
		yamlValue: `apiVersion: v1alpha1
kind: Dash0SpamFilter
metadata:
  name: Drop noisy health checks
spec:
  contexts:
    - log
  filter:
    - key: "k8s.namespace.name"
      operator: "is"
      value: "kube-system"`,
		hasURL: false,
		mockSetup: func(m *MockClient, dataset string) {
			m.On("CreateSpamFilter", mock.Anything, mock.Anything, mock.Anything, dataset).Return(nil)
			m.On("ResolveSpamFilter", mock.Anything, mock.Anything, dataset).Return("test-id", nil)
		},
	},
	{
		name:        "synthetic_check",
		newResource: NewSyntheticCheckResource,
		yamlAttr:    "synthetic_check_yaml",
		yamlValue: `
kind: Dash0SyntheticCheck
metadata:
  name: examplecom
spec:
  enabled: true
  plugin:
    kind: http
    spec:
      request:
        url: https://www.example.com`,
		hasURL: true,
		mockSetup: func(m *MockClient, dataset string) {
			m.On("CreateSyntheticCheck", mock.Anything, mock.Anything, mock.Anything, dataset).Return(nil)
			m.On("ResolveSyntheticCheck", mock.Anything, mock.Anything, dataset).Return("test-id", "", nil)
		},
	},
	{
		name:        "view",
		newResource: NewViewResource,
		yamlAttr:    "view_yaml",
		yamlValue:   "kind: View\nmetadata:\n  name: example-view\nspec:\n  title: Example View",
		hasURL:      true,
		mockSetup: func(m *MockClient, dataset string) {
			m.On("CreateView", mock.Anything, mock.Anything, mock.Anything, dataset).Return(nil)
			m.On("ResolveView", mock.Anything, mock.Anything, dataset).Return("test-id", "", nil)
		},
	},
}

// buildOmittedDatasetCreatePlan builds a minimal Create plan for a
// dataset-scoped resource where `dataset` is omitted from config. For an
// Optional+Computed attribute with no config value and no prior state, the
// framework's planned value is unknown -- exactly as it would be for a real
// `terraform plan` on a new resource that relies on the provider default.
func buildOmittedDatasetCreatePlan(yamlAttr, yamlValue string, hasURL bool) tfsdk.Plan {
	attrTypes := map[string]tftypes.Type{
		"origin":  tftypes.String,
		"id":      tftypes.String,
		"dataset": tftypes.String,
		yamlAttr:  tftypes.String,
	}
	attrValues := map[string]tftypes.Value{
		"origin":  tftypes.NewValue(tftypes.String, ""),
		"id":      tftypes.NewValue(tftypes.String, nil),
		"dataset": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		yamlAttr:  tftypes.NewValue(tftypes.String, yamlValue),
	}
	schemaAttrs := map[string]schema.Attribute{
		"origin":  schema.StringAttribute{Computed: true},
		"id":      schema.StringAttribute{Computed: true},
		"dataset": schema.StringAttribute{Optional: true, Computed: true},
		yamlAttr:  schema.StringAttribute{Required: true},
	}
	if hasURL {
		attrTypes["url"] = tftypes.String
		attrValues["url"] = tftypes.NewValue(tftypes.String, nil)
		schemaAttrs["url"] = schema.StringAttribute{Computed: true}
	}
	return tfsdk.Plan{
		Raw:    tftypes.NewValue(tftypes.Object{AttributeTypes: attrTypes}, attrValues),
		Schema: schema.Schema{Attributes: schemaAttrs},
	}
}

// TestDatasetScopedResources_Create_InheritsProviderDefaultDataset covers all
// six dataset-scoped resources, verifying each resource's own Create path
// falls back to the provider-level default dataset when the resource omits
// its own `dataset` attribute. Each resource has a distinct client call, so
// this cannot be collapsed into a single shared code path -- the table
// exists precisely to exercise all six independently.
func TestDatasetScopedResources_Create_InheritsProviderDefaultDataset(t *testing.T) {
	const providerDefault = "provider-default-dataset"

	for _, tc := range datasetInheritanceCases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.newResource()

			mockClient := new(MockClient)
			tc.mockSetup(mockClient, providerDefault)

			configurable, ok := r.(resource.ResourceWithConfigure)
			require.True(t, ok, "%s must implement resource.ResourceWithConfigure", tc.name)

			configureResp := &resource.ConfigureResponse{}
			configurable.Configure(context.Background(), resource.ConfigureRequest{
				ProviderData: resourceProviderData{client: mockClient, defaultDataset: providerDefault},
			}, configureResp)
			require.False(t, configureResp.Diagnostics.HasError(), "configure diagnostics: %v", configureResp.Diagnostics.Errors())

			plan := buildOmittedDatasetCreatePlan(tc.yamlAttr, tc.yamlValue, tc.hasURL)
			createReq := resource.CreateRequest{Plan: plan}
			createResp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}

			r.Create(context.Background(), createReq, &createResp)

			require.False(t, createResp.Diagnostics.HasError(), "create diagnostics: %v", createResp.Diagnostics.Errors())
			mockClient.AssertExpectations(t)

			var dataset types.String
			diags := createResp.State.GetAttribute(context.Background(), path.Root("dataset"), &dataset)
			require.False(t, diags.HasError(), "GetAttribute diagnostics: %v", diags.Errors())
			assert.Equal(t, providerDefault, dataset.ValueString())
		})
	}
}

// datasetSchemaCase is the minimal descriptor needed to fetch a resource's
// real `dataset` schema attribute for the plan-modifier tests below.
type datasetSchemaCase struct {
	name        string
	newResource func() resource.Resource
}

var datasetSchemaCases = []datasetSchemaCase{
	{"dashboard", NewDashboardResource},
	{"check_rule", NewCheckRuleResource},
	{"recording_rule", NewRecordingRuleResource},
	{"spam_filter", NewSpamFilterResource},
	{"synthetic_check", NewSyntheticCheckResource},
	{"view", NewViewResource},
}

// nonNullEmptyObject returns a known (non-null), attribute-less object value,
// used to stand in for tfsdk.State.Raw / tfsdk.Plan.Raw when a plan modifier
// only cares whether the resource is being created (null state) or destroyed
// (null plan), not about any other attribute.
func nonNullEmptyObject() tftypes.Value {
	return tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, map[string]tftypes.Value{})
}

// runStringPlanModifiers replicates the terraform-plugin-framework's
// sequential plan-modifier chaining for a single attribute: each modifier's
// PlanValue output becomes the next modifier's PlanValue input, while
// RequiresReplace accumulates across the whole chain and is never unset by a
// later modifier. See fwserver's attribute plan modification loop, which this
// mirrors, for the authoritative behavior.
func runStringPlanModifiers(ctx context.Context, modifiers []planmodifier.String, req planmodifier.StringRequest) (finalValue types.String, requiresReplace bool) {
	value := req.PlanValue
	for _, m := range modifiers {
		stepReq := req
		stepReq.PlanValue = value
		stepResp := &planmodifier.StringResponse{PlanValue: value}
		m.PlanModifyString(ctx, stepReq, stepResp)
		value = stepResp.PlanValue
		if stepResp.RequiresReplace {
			requiresReplace = true
		}
	}
	return value, requiresReplace
}

// TestDatasetScopedResources_DatasetPlanModifiers exercises the `dataset`
// attribute's real plan modifiers -- UseStateForUnknown then RequiresReplace
// -- for all six dataset-scoped resources, at the planning stage rather than
// through Create. This is deliberately independent of any provider-level
// default: plan modifiers never see the provider configuration, so a
// resource's own prior state is the only thing an omitted `dataset` can pin
// to. That is what makes "changing the provider default does not move
// existing resources" true, and this test is what proves it holds for every
// resource, not just at the Create-time default-resolution level covered
// above.
//
// The order of these two modifiers matters: RequiresReplace must run after
// UseStateForUnknown resolves the omitted attribute back to its known prior
// value. Running it first would compare the not-yet-resolved unknown planned
// value against the known state value on every single update -- unrelated or
// not -- and force replacement every time a resource with an inherited
// dataset was updated at all.
func TestDatasetScopedResources_DatasetPlanModifiers(t *testing.T) {
	ctx := context.Background()
	nonNullState := tfsdk.State{Raw: nonNullEmptyObject()}
	nonNullPlan := tfsdk.Plan{Raw: nonNullEmptyObject()}

	for _, tc := range datasetSchemaCases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.newResource()
			schemaResp := &resource.SchemaResponse{}
			r.Schema(ctx, resource.SchemaRequest{}, schemaResp)

			datasetAttr, ok := schemaResp.Schema.Attributes["dataset"].(schema.StringAttribute)
			require.True(t, ok, "%s: dataset attribute must be a StringAttribute", tc.name)
			modifiers := datasetAttr.PlanModifiers
			require.NotEmpty(t, modifiers, "%s: dataset attribute must have plan modifiers", tc.name)

			t.Run("omitted dataset on update inherits and pins the prior state, without requiring replacement", func(t *testing.T) {
				req := planmodifier.StringRequest{
					State:       nonNullState,
					Plan:        nonNullPlan,
					ConfigValue: types.StringNull(),
					PlanValue:   types.StringUnknown(),
					StateValue:  types.StringValue("prior-dataset"),
				}

				finalValue, requiresReplace := runStringPlanModifiers(ctx, modifiers, req)

				assert.Equal(t, "prior-dataset", finalValue.ValueString(),
					"an omitted dataset must be pinned to the resource's own prior state, independent of any provider-level default")
				assert.False(t, requiresReplace,
					"omitting dataset on an otherwise unrelated update must not force replacement")
			})

			t.Run("an explicit resource-level dataset change still requires replacement", func(t *testing.T) {
				req := planmodifier.StringRequest{
					State:       nonNullState,
					Plan:        nonNullPlan,
					ConfigValue: types.StringValue("new-dataset"),
					PlanValue:   types.StringValue("new-dataset"),
					StateValue:  types.StringValue("prior-dataset"),
				}

				finalValue, requiresReplace := runStringPlanModifiers(ctx, modifiers, req)

				assert.Equal(t, "new-dataset", finalValue.ValueString())
				assert.True(t, requiresReplace, "an explicit dataset change must still force replacement")
			})
		})
	}
}
