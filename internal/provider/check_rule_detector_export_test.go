package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	dash0yaml "github.com/dash0hq/dash0-api-client-go/yaml"
	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

type detectorExportClient struct {
	client.Client
	response string
	update   string
}

func (c *detectorExportClient) GetCheckRule(context.Context, string, string) (string, error) {
	return c.response, nil
}
func (c *detectorExportClient) ResolveCheckRule(context.Context, string, string) (string, string, error) {
	return "rule-id", "https://app.dash0.com/goto/alerting/check-rules?check_rule_id=rule-id&dataset=production", nil
}
func (c *detectorExportClient) UpdateCheckRule(_ context.Context, _ string, yaml string, _ string) error {
	c.update = yaml
	return nil
}

func TestCheckRuleDetectorExportReadImportApply(t *testing.T) {
	paths, err := filepath.Glob("client/testdata/detector-export/*.yaml")
	require.NoError(t, err)
	require.Len(t, paths, 7)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			input, err := os.ReadFile(path)
			require.NoError(t, err)
			rule, err := dash0yaml.UnmarshalPrometheusRule(input)
			require.NoError(t, err)
			annotations := rule.Annotations.AdditionalProperties
			if direction, ok := annotations["dash0.com/baseline-direction"]; ok {
				rule.Thresholds.Baseline = &dash0.CheckThresholdBaseline{Direction: dash0.AnomalyDirection(direction), SpreadFloor: dash0.Ptr(0.1)}
			} else {
				value := 2.0
				if annotations["dash0.com/change-gate-comparison"] == "absolute_delta" {
					value = 0.05
				}
				rule.Thresholds.ChangeGate = &dash0.CheckThresholdChangeGate{Comparison: dash0.ChangeGateComparison(annotations["dash0.com/change-gate-comparison"]), Value: value, BaselineWindow: dash0.Duration(annotations["dash0.com/change-gate-baseline-window"])}
			}
			for _, key := range []string{"dash0.com/baseline-direction", "dash0.com/baseline-spread-floor", "dash0.com/change-gate-comparison", "dash0.com/change-gate-value", "dash0.com/change-gate-baseline-window"} {
				delete(annotations, key)
			}
			output, err := dash0yaml.MarshalPrometheusRule(rule)
			require.NoError(t, err)
			c := &detectorExportClient{response: string(output)}
			r := &CheckRuleResource{client: c}
			ctx := t.Context()
			schemaResponse := resource.SchemaResponse{}
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
			model := checkRuleModel{Origin: types.StringValue("tf_detector"), ID: types.StringValue("rule-id"), Dataset: types.StringValue("production"), CheckRuleYaml: types.StringValue(string(input)), URL: types.StringNull()}
			state := tfsdk.State{Schema: schemaResponse.Schema}
			require.False(t, state.Set(ctx, model).HasError())
			readResponse := resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, &readResponse)
			require.False(t, readResponse.Diagnostics.HasError(), readResponse.Diagnostics)
			assert.Zero(t, readResponse.Diagnostics.WarningsCount())
			var read checkRuleModel
			require.False(t, readResponse.State.Get(ctx, &read).HasError())
			assert.Equal(t, string(input), read.CheckRuleYaml.ValueString(), "an unchanged read must preserve configured YAML verbatim")
			importResponse := resource.ImportStateResponse{State: state}
			r.ImportState(ctx, resource.ImportStateRequest{ID: "production,tf_detector"}, &importResponse)
			require.False(t, importResponse.Diagnostics.HasError(), importResponse.Diagnostics)
			var imported checkRuleModel
			require.False(t, importResponse.State.Get(ctx, &imported).HasError())
			assert.Equal(t, "tf_detector", imported.Origin.ValueString())
			equivalent, err := converter.ResourceYAMLEquivalent(string(input), imported.CheckRuleYaml.ValueString(), []string{"metadata.name"}, nil)
			require.NoError(t, err)
			assert.True(t, equivalent)
			plan := tfsdk.Plan{Schema: schemaResponse.Schema}
			require.False(t, plan.Set(ctx, imported).HasError())
			updateResponse := resource.UpdateResponse{State: importResponse.State}
			r.Update(ctx, resource.UpdateRequest{Plan: plan, State: importResponse.State}, &updateResponse)
			require.False(t, updateResponse.Diagnostics.HasError(), updateResponse.Diagnostics)
			assert.Equal(t, imported.CheckRuleYaml.ValueString(), c.update)
		})
	}
}
