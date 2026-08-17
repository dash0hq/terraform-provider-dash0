package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	actionschema "github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// actionSchemaOf returns the schema an action declares.
func actionSchemaOf(t *testing.T, a action.Action) actionschema.Schema {
	t.Helper()
	resp := &action.SchemaResponse{}
	a.Schema(context.Background(), action.SchemaRequest{}, resp)
	require.False(t, resp.Diagnostics.HasError(), "schema diagnostics: %v", resp.Diagnostics)
	return resp.Schema
}

// actionTestConfig builds a tfsdk.Config for an action from a sparse map of
// values. Every attribute the schema declares but the caller omits is filled in
// as null, so tests only state what they care about.
func actionTestConfig(t *testing.T, s actionschema.Schema, values map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	ctx := context.Background()

	objType, ok := s.Type().TerraformType(ctx).(tftypes.Object)
	require.True(t, ok, "action schema should map to an object type")

	full := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		if v, provided := values[name]; provided {
			full[name] = v
			continue
		}
		full[name] = tftypes.NewValue(attrType, nil)
	}

	for name := range values {
		_, declared := objType.AttributeTypes[name]
		require.True(t, declared, "test supplied value for undeclared attribute %q", name)
	}

	return tfsdk.Config{Raw: tftypes.NewValue(objType, full), Schema: s}
}

// tfString builds a known string value.
func tfString(s string) tftypes.Value {
	return tftypes.NewValue(tftypes.String, s)
}

// tfNumber builds a known number value.
func tfNumber(n int64) tftypes.Value {
	return tftypes.NewValue(tftypes.Number, n)
}

// tfBool builds a known boolean value.
func tfBool(b bool) tftypes.Value {
	return tftypes.NewValue(tftypes.Bool, b)
}

// tfStringMap builds a known map-of-string value.
func tfStringMap(m map[string]string) tftypes.Value {
	elements := make(map[string]tftypes.Value, len(m))
	for k, v := range m {
		elements[k] = tfString(v)
	}
	return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, elements)
}

func TestMergeAttributes_DedicatedAttributesWin(t *testing.T) {
	base := map[string]string{"service.name": "checkout-api"}
	merged := mergeAttributes(base, map[string]string{
		"service.name":  "should-not-overwrite",
		"custom.tenant": "acme",
	})

	assert.Equal(t, "checkout-api", merged["service.name"])
	assert.Equal(t, "acme", merged["custom.tenant"])
}

func TestParseTimestampAttribute(t *testing.T) {
	tests := map[string]struct {
		value     string
		wantError bool
	}{
		"rfc3339":             {value: "2026-03-15T10:30:00Z"},
		"rfc3339 nanoseconds": {value: "2026-03-15T10:30:00.123456789Z"},
		"rfc3339 offset":      {value: "2026-03-15T10:30:00+02:00"},
		"date only":           {value: "2026-03-15", wantError: true},
		"nonsense":            {value: "yesterday", wantError: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			diags := &diag.Diagnostics{}
			parsed := parseTimestampAttribute(types.StringValue(tt.value), "time", diags)
			if tt.wantError {
				assert.True(t, diags.HasError(), "expected an error diagnostic")
				assert.True(t, parsed.IsZero())
				return
			}
			assert.False(t, diags.HasError(), "unexpected diagnostics: %v", diags)
			assert.False(t, parsed.IsZero())
		})
	}
}

func TestParseTimestampAttribute_UnsetYieldsZeroTime(t *testing.T) {
	diags := &diag.Diagnostics{}
	assert.True(t, parseTimestampAttribute(types.StringNull(), "time", diags).IsZero())
	assert.False(t, diags.HasError())
}

func TestValidateSeverityNumber(t *testing.T) {
	tests := map[string]struct {
		value     int64
		wantError bool
	}{
		"lowest valid":  {value: 1},
		"info":          {value: 9},
		"highest valid": {value: 24},
		"below range":   {value: 0, wantError: true},
		"above range":   {value: 25, wantError: true},
		"negative":      {value: -1, wantError: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			diags := &diag.Diagnostics{}
			validateSeverityNumber(types.Int64Value(tt.value), diags)
			assert.Equal(t, tt.wantError, diags.HasError(), "diagnostics: %v", diags)
		})
	}
}

func TestValidateSeverityNumber_UnsetIsValid(t *testing.T) {
	diags := &diag.Diagnostics{}
	validateSeverityNumber(types.Int64Null(), diags)
	assert.False(t, diags.HasError())
}

func TestValidateTraceContext(t *testing.T) {
	tests := map[string]struct {
		traceID   types.String
		spanID    types.String
		wantError bool
	}{
		"both set":      {traceID: types.StringValue("0af7651916cd43dd8448eb211c80319c"), spanID: types.StringValue("b7ad6b7169203331")},
		"both unset":    {traceID: types.StringNull(), spanID: types.StringNull()},
		"trace id only": {traceID: types.StringValue("0af7651916cd43dd8448eb211c80319c"), spanID: types.StringNull(), wantError: true},
		"span id only":  {traceID: types.StringNull(), spanID: types.StringValue("b7ad6b7169203331"), wantError: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			diags := &diag.Diagnostics{}
			validateTraceContext(tt.traceID, tt.spanID, diags)
			assert.Equal(t, tt.wantError, diags.HasError(), "diagnostics: %v", diags)
		})
	}
}
