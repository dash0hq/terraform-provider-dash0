package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

const telemetryTransformationRuleYAML = `kind: Dash0TelemetryTransformationRule
metadata:
  name: normalize-logs
spec:
  enabled: false
  signal: logs
  context: log
  conditions:
    - 'attributes["env"] == "production"'
  transformations:
    - name: prepare
      statements:
        - 'set(attributes["stage"], "one")'
        - 'set(attributes["stage"], "two")'
    - name: finish
      statements:
        - 'set(attributes["done"], true)'
  examples:
    - name: sample
      otlp:
        resourceLogs:
          - scopeLogs:
              - logRecords:
                  - timeUnixNano: "1793628000123456789"
                    body:
                      stringValue: sample log
`

const telemetryTransformationRuleAPIJSON = `{
  "kind":"Dash0TelemetryTransformationRule",
  "metadata":{"name":"normalize-logs","labels":{"dash0.com/id":"ttr_rule-id","dash0.com/origin":"tf_origin","dash0.com/dataset":"production","dash0.com/source":"terraform"},"annotations":{"dash0.com/created-at":"2026-11-01T00:00:00Z","dash0.com/updated-at":"2026-11-02T14:03:08.123456789Z"}},
  "spec":{
    "enabled":false,"signal":"logs","context":"log","priority":200,
    "conditions":["attributes[\"env\"] == \"production\""],
    "transformations":[{"name":"prepare","statements":["set(attributes[\"stage\"], \"one\")","set(attributes[\"stage\"], \"two\")"]},{"name":"finish","statements":["set(attributes[\"done\"], true)"]}],
    "examples":[{"name":"sample","otlp":{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"1793628000123456789","body":{"stringValue":"sample log"}}]}]}]}}]
  }
}`

func transformationRuleTestState(t *testing.T, model telemetryTransformationRuleModel) tfsdk.State {
	t.Helper()
	var schemaResponse resource.SchemaResponse
	(&TelemetryTransformationRuleResource{}).Schema(t.Context(), resource.SchemaRequest{}, &schemaResponse)
	state := tfsdk.State{Schema: schemaResponse.Schema}
	require.False(t, state.Set(t.Context(), &model).HasError())
	return state
}

func transformationRuleTestModel() telemetryTransformationRuleModel {
	return telemetryTransformationRuleModel{
		Origin:                          types.StringValue("tf_origin"),
		ID:                              types.StringValue("ttr_rule-id"),
		Dataset:                         types.StringValue("production"),
		TelemetryTransformationRuleYAML: types.StringValue(telemetryTransformationRuleYAML),
	}
}

func TestTelemetryTransformationRuleResourceRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		changed bool
	}{
		{"metadata and default priority", telemetryTransformationRuleAPIJSON, false},
		{"disabled rule enabled", strings.Replace(telemetryTransformationRuleAPIJSON, `"enabled":false`, `"enabled":true`, 1), true},
		{"explicit zero priority", strings.Replace(telemetryTransformationRuleAPIJSON, `"priority":200`, `"priority":0`, 1), true},
		{"explicit high priority", strings.Replace(telemetryTransformationRuleAPIJSON, `"priority":200`, `"priority":400`, 1), true},
		{"reordered statements", strings.Replace(telemetryTransformationRuleAPIJSON, `"set(attributes[\"stage\"], \"one\")","set(attributes[\"stage\"], \"two\")"`, `"set(attributes[\"stage\"], \"two\")","set(attributes[\"stage\"], \"one\")"`, 1), true},
		{"changed example", strings.Replace(telemetryTransformationRuleAPIJSON, `"sample log"`, `"changed sample"`, 1), true},
		{"changed timestamp inside example", strings.Replace(telemetryTransformationRuleAPIJSON, `"1793628000123456789"`, `"1793628000123456790"`, 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &MockClient{}
			c.On("GetTelemetryTransformationRule", mock.Anything, "tf_origin", "production").Return(tc.body, nil)
			r := &TelemetryTransformationRuleResource{client: c}
			state := transformationRuleTestState(t, transformationRuleTestModel())
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Empty(t, resp.Diagnostics)
			var model telemetryTransformationRuleModel
			require.False(t, resp.State.Get(t.Context(), &model).HasError())
			assert.Equal(t, "ttr_rule-id", model.ID.ValueString())
			if tc.changed {
				assert.Equal(t, tc.body, model.TelemetryTransformationRuleYAML.ValueString())
			} else {
				assert.Equal(t, telemetryTransformationRuleYAML, model.TelemetryTransformationRuleYAML.ValueString())
			}
		})
	}
}

func TestTelemetryTransformationRuleResourceMissing(t *testing.T) {
	for _, datasetGone := range []bool{false, true} {
		t.Run(fmt.Sprint(datasetGone), func(t *testing.T) {
			c := &MockClient{}
			c.On("GetTelemetryTransformationRule", mock.Anything, "tf_origin", "production").Return("", &dash0.APIError{StatusCode: 404})
			var datasetErr error
			if datasetGone {
				datasetErr = &dash0.APIError{StatusCode: 404}
			}
			c.On("GetDataset", mock.Anything, "production").Return("{}", datasetErr)
			r := &TelemetryTransformationRuleResource{client: c}
			state := transformationRuleTestState(t, transformationRuleTestModel())
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError())
			assert.Equal(t, !datasetGone, resp.State.Raw.IsNull())
			assert.Equal(t, datasetGone, resp.Diagnostics.WarningsCount() > 0)
		})
	}
}

func TestTelemetryTransformationRuleResourceImport(t *testing.T) {
	for _, identifier := range []string{"tf_origin", "ttr_rule-id"} {
		t.Run(identifier, func(t *testing.T) {
			c := &MockClient{}
			c.On("GetTelemetryTransformationRule", mock.Anything, identifier, "production").Return(telemetryTransformationRuleAPIJSON, nil)
			r := &TelemetryTransformationRuleResource{client: c}
			resp := resource.ImportStateResponse{State: transformationRuleTestState(t, telemetryTransformationRuleModel{})}
			r.ImportState(t.Context(), resource.ImportStateRequest{ID: "production," + identifier}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			var model telemetryTransformationRuleModel
			require.False(t, resp.State.Get(t.Context(), &model).HasError())
			assert.Equal(t, identifier, model.Origin.ValueString())
			assert.Equal(t, "ttr_rule-id", model.ID.ValueString())
			assert.Equal(t, "production", model.Dataset.ValueString())
			assert.JSONEq(t, telemetryTransformationRuleAPIJSON, model.TelemetryTransformationRuleYAML.ValueString())
		})
	}
	for _, identifier := range []string{"", "tf_origin", ",tf_origin", "production,", "production,origin,extra"} {
		r := &TelemetryTransformationRuleResource{}
		resp := resource.ImportStateResponse{}
		r.ImportState(t.Context(), resource.ImportStateRequest{ID: identifier}, &resp)
		require.True(t, resp.Diagnostics.HasError(), identifier)
	}
}

func TestTelemetryTransformationRuleResourceValidation(t *testing.T) {
	for _, document := range []string{"", "null", "[]", "kind: Other", "invalid: [", strings.Replace(telemetryTransformationRuleYAML, "  enabled: false\n", "", 1), strings.Replace(telemetryTransformationRuleYAML, "enabled: false", "enabled: null", 1)} {
		r := &TelemetryTransformationRuleResource{}
		model := transformationRuleTestModel()
		model.TelemetryTransformationRuleYAML = types.StringValue(document)
		state := transformationRuleTestState(t, model)
		resp := resource.ValidateConfigResponse{}
		r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: tfsdk.Config{Raw: state.Raw, Schema: state.Schema}}, &resp)
		require.True(t, resp.Diagnostics.HasError(), document)
	}
}

func TestTelemetryTransformationRuleResourcePlanPreservesOrder(t *testing.T) {
	var schemaResponse resource.SchemaResponse
	(&TelemetryTransformationRuleResource{}).Schema(t.Context(), resource.SchemaRequest{}, &schemaResponse)
	attribute := schemaResponse.Schema.Attributes[telemetryTransformationRuleYAMLAttribute]
	stringAttribute := attribute.(schema.StringAttribute)
	for _, changed := range []bool{false, true} {
		server := telemetryTransformationRuleAPIJSON
		if changed {
			server = strings.Replace(server, `"set(attributes[\"stage\"], \"one\")","set(attributes[\"stage\"], \"two\")"`, `"set(attributes[\"stage\"], \"two\")","set(attributes[\"stage\"], \"one\")"`, 1)
		}
		req := planmodifier.StringRequest{ConfigValue: types.StringValue(telemetryTransformationRuleYAML), PlanValue: types.StringValue(telemetryTransformationRuleYAML), StateValue: types.StringValue(server)}
		resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
		for _, modifier := range stringAttribute.PlanModifiers {
			modifier.PlanModifyString(t.Context(), req, &resp)
		}
		if changed {
			assert.Equal(t, telemetryTransformationRuleYAML, resp.PlanValue.ValueString())
		} else {
			assert.Equal(t, server, resp.PlanValue.ValueString())
		}
	}
}
