package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// TestDatasetScopedDelete_NotFound covers a destroy of an asset the server no
// longer has, for example because its dataset was deleted while the asset was
// kept in state. A 404 is the desired end state; any other error must surface.
func TestDatasetScopedDelete_NotFound(t *testing.T) {
	tests := []struct {
		name   string
		method string
		build  func(c client.Client) resource.Resource
	}{
		{"dashboard", "DeleteDashboard", func(c client.Client) resource.Resource { return &DashboardResource{client: c} }},
		{"view", "DeleteView", func(c client.Client) resource.Resource { return &ViewResource{client: c} }},
		{"check_rule", "DeleteCheckRule", func(c client.Client) resource.Resource { return &CheckRuleResource{client: c} }},
		{"synthetic_check", "DeleteSyntheticCheck", func(c client.Client) resource.Resource { return &SyntheticCheckResource{client: c} }},
		{"recording_rule", "DeleteRecordingRule", func(c client.Client) resource.Resource { return &RecordingRuleResource{client: c} }},
		{"slo", "DeleteSLO", func(c client.Client) resource.Resource { return &SLOResource{client: c} }},
		{"spam_filter", "DeleteSpamFilter", func(c client.Client) resource.Resource { return &SpamFilterResource{client: c} }},
	}
	for _, tt := range tests {
		for _, tc := range []struct {
			name    string
			err     error
			wantErr bool
		}{
			{"404 is success", errNotFound, false},
			{"500 surfaces", errServer, true},
		} {
			t.Run(tt.name+"/"+tc.name, func(t *testing.T) {
				m := &MockClient{}
				m.On(tt.method, context.Background(), "tf_x", "ds").Return(tc.err)
				r := tt.build(m)

				var schemaResp resource.SchemaResponse
				r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
				objType := schemaResp.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
				raw := make(map[string]tftypes.Value, len(objType.AttributeTypes))
				for name, typ := range objType.AttributeTypes {
					switch name {
					case "origin":
						raw[name] = tftypes.NewValue(typ, "tf_x")
					case "dataset":
						raw[name] = tftypes.NewValue(typ, "ds")
					default:
						raw[name] = tftypes.NewValue(typ, nil)
					}
				}
				state := tfsdk.State{Raw: tftypes.NewValue(objType, raw), Schema: schemaResp.Schema}

				resp := &resource.DeleteResponse{State: state}
				r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

				m.AssertExpectations(t)
				assert.Equal(t, tc.wantErr, resp.Diagnostics.HasError())
			})
		}
	}
}
