package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// customLabelsPlanCase is a resource whose API stores metadata.labels.custom.
// For these resources the plan must show a change to the custom labels, even
// when nothing else in the document changes.
type customLabelsPlanCase struct {
	name        string
	newResource func() resource.Resource
	yamlAttr    string
	document    string // a valid document without metadata.labels
	nameLine    string // the metadata.name line, where labels are inserted
}

var customLabelsPlanCases = []customLabelsPlanCase{
	{
		name:        "time_series_aggregation",
		newResource: NewTimeSeriesAggregationResource,
		yamlAttr:    "time_series_aggregation_yaml",
		document:    tsaValidYaml,
		nameLine:    "  name: http-server-request-duration-rollup\n",
	},
	{
		name:        "sampling_rule",
		newResource: NewSamplingRuleResource,
		yamlAttr:    "sampling_rule_yaml",
		document:    samplingRuleValidYaml,
		nameLine:    "  name: keep-errors\n",
	},
}

func (c customLabelsPlanCase) withLabels(t *testing.T, labels string) string {
	t.Helper()
	require.Contains(t, c.document, c.nameLine, "fixture must contain the metadata.name line")
	return strings.Replace(c.document, c.nameLine, c.nameLine+"  labels:\n"+labels, 1)
}

func (c customLabelsPlanCase) planValue(t *testing.T, config, state string) string {
	t.Helper()
	schemaResp := &resource.SchemaResponse{}
	c.newResource().Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	modifiers := schemaResp.Schema.Attributes[c.yamlAttr].(schema.StringAttribute).PlanModifiers
	require.Len(t, modifiers, 1)

	req := planmodifier.StringRequest{
		ConfigValue: types.StringValue(config),
		StateValue:  types.StringValue(state),
		PlanValue:   types.StringValue(config),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	modifiers[0].PlanModifyString(context.Background(), req, resp)
	return resp.PlanValue.ValueString()
}

func TestCustomLabels_PlanShowsLabelOnlyChanges(t *testing.T) {
	for _, c := range customLabelsPlanCases {
		t.Run(c.name, func(t *testing.T) {
			platform := c.withLabels(t, "    custom:\n      team: platform\n")
			payments := c.withLabels(t, "    custom:\n      team: payments\n")
			platformWithServerLabels := c.withLabels(t, "    custom:\n      team: platform\n    dash0.com/id: an-id\n    dash0.com/version: \"3\"\n")

			assert.Equal(t, platform, c.planValue(t, platform, c.document),
				"adding custom labels must produce a plan")
			assert.Equal(t, payments, c.planValue(t, payments, platform),
				"changing a custom label must produce a plan")
			assert.Equal(t, c.document, c.planValue(t, c.document, platform),
				"removing the custom labels must produce a plan")
			assert.Equal(t, platformWithServerLabels, c.planValue(t, platform, platformWithServerLabels),
				"the server-managed labels beside matching custom labels must not produce a plan")
		})
	}
}

// The time series aggregation modifier also ignores spec.priority when the
// configuration omits it. Comparing custom labels must not lose that.
func TestCustomLabels_TimeSeriesAggregationKeepsConditionallyIgnoredFields(t *testing.T) {
	c := customLabelsPlanCases[0]
	require.Equal(t, "time_series_aggregation", c.name)

	config := c.withLabels(t, "    custom:\n      team: platform\n")
	state := strings.Replace(config, "  enabled: true\n", "  enabled: true\n  priority: 2\n", 1)
	require.NotEqual(t, config, state, "fixture must add spec.priority")

	assert.Equal(t, state, c.planValue(t, config, state),
		"a server-defaulted spec.priority the configuration omits must not produce a plan")
}
