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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// signalToMetricsYamlWithAnnotations is signalToMetricsValidYaml with the
// given metadata.annotations block.
func signalToMetricsYamlWithAnnotations(annotations string) string {
	return strings.Replace(signalToMetricsValidYaml, "  name: checkout-errors\n",
		"  name: checkout-errors\n  annotations:\n"+annotations, 1)
}

// signalToMetricsAPIResponseWithAnnotations is signalToMetricsAPIResponseJSON
// with extra annotations, as a GET would return them once Dash0 does.
func signalToMetricsAPIResponseWithAnnotations(annotations string) string {
	return strings.Replace(signalToMetricsAPIResponseJSON, `"annotations":{`, `"annotations":{`+annotations+`,`, 1)
}

// A change to dash0.com/folder-path or dash0.com/sharing in the config plans
// an update, as on dashboards: both are sent on a PUT.
func TestSignalToMetricsResource_PlanModifierComparesFolderPathAndSharing(t *testing.T) {
	r := &SignalToMetricsResource{}
	schemaResp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, schemaResp)
	modifiers := schemaResp.Schema.Attributes["signal_to_metrics_yaml"].(schema.StringAttribute).PlanModifiers
	require.Len(t, modifiers, 1)

	for _, tc := range []struct {
		name       string
		state      string
		config     string
		wantUpdate bool
	}{
		{"folder path changed", "    dash0.com/folder-path: /a\n", "    dash0.com/folder-path: /b\n", true},
		{"sharing changed", "    dash0.com/sharing: team:a\n", "    dash0.com/sharing: team:b\n", true},
		{"folder path removed", "    dash0.com/folder-path: /a\n", "    dash0.com/created-at: \"2026-10-30T09:13:08Z\"\n", true},
		{"unchanged", "    dash0.com/folder-path: /a\n", "    dash0.com/folder-path: /a\n", false},
		{"only a timestamp differs", "    dash0.com/updated-at: \"2026-10-30T09:13:08Z\"\n", "    dash0.com/updated-at: \"2026-11-01T00:00:00Z\"\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateYaml := signalToMetricsYamlWithAnnotations(tc.state)
			configYaml := signalToMetricsYamlWithAnnotations(tc.config)
			req := planmodifier.StringRequest{
				ConfigValue: types.StringValue(configYaml),
				StateValue:  types.StringValue(stateYaml),
				PlanValue:   types.StringValue(configYaml),
			}
			resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
			modifiers[0].PlanModifyString(context.Background(), req, resp)

			if tc.wantUpdate {
				assert.Equal(t, configYaml, resp.PlanValue.ValueString(), "the change must reach the plan")
			} else {
				assert.Equal(t, stateYaml, resp.PlanValue.ValueString(), "an equivalent state must suppress the diff")
			}
		})
	}
}

// Read compares dash0.com/folder-path and dash0.com/sharing only when the API
// response carries them. Dash0 does not return them for signal-to-metrics
// rules yet, so comparing them always would show a change on every plan.
// Once it does, a change made outside Terraform is detected.
func TestSignalToMetricsResource_Read_ComparesAnnotationsOnlyWhenReturned(t *testing.T) {
	for _, tc := range []struct {
		name        string
		state       string
		apiResponse string
		wantDrift   bool
	}{
		{
			name:        "not returned: the configured values are kept",
			state:       signalToMetricsYamlWithAnnotations("    dash0.com/folder-path: /b\n    dash0.com/sharing: team:a\n"),
			apiResponse: signalToMetricsAPIResponseJSON,
		},
		{
			name:        "returned and equal",
			state:       signalToMetricsYamlWithAnnotations("    dash0.com/folder-path: /a\n"),
			apiResponse: signalToMetricsAPIResponseWithAnnotations(`"dash0.com/folder-path":"/a"`),
		},
		{
			name:        "returned and changed outside Terraform",
			state:       signalToMetricsYamlWithAnnotations("    dash0.com/folder-path: /a\n"),
			apiResponse: signalToMetricsAPIResponseWithAnnotations(`"dash0.com/folder-path":"/b"`),
			wantDrift:   true,
		},
		{
			name:        "returned but not configured",
			state:       signalToMetricsValidYaml,
			apiResponse: signalToMetricsAPIResponseWithAnnotations(`"dash0.com/sharing":"team:a"`),
			wantDrift:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockClient{}
			r := &SignalToMetricsResource{client: mockClient}
			mockClient.On("GetSignalToMetrics", mock.Anything, "tf_origin", "test-dataset").Return(tc.apiResponse, nil)

			state := signalToMetricsState(strPtr("tf_origin"), strPtr("an-id"), strPtr("test-dataset"), strPtr(tc.state))
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)

			var result signalToMetricsModel
			require.False(t, resp.State.Get(context.Background(), &result).HasError())
			if tc.wantDrift {
				assert.Equal(t, tc.apiResponse, result.SignalToMetricsYaml.ValueString(), "the change must become the new state")
			} else {
				assert.Equal(t, tc.state, result.SignalToMetricsYaml.ValueString(), "the state must be kept")
			}
		})
	}
}
