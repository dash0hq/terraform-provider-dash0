package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

// awsIntegrationSchema returns a minimal schema for test state construction.
func awsIntegrationSchema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":                               schema.StringAttribute{Computed: true},
			"dataset":                          schema.StringAttribute{Required: true},
			"external_id":                      schema.StringAttribute{Required: true},
			"iam_role_name_prefix":             schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("dash0")},
			"enable_resources_instrumentation": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"dash0_aws_account_id":             schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("115813213817")},
			"tags":                             schema.MapAttribute{Optional: true, ElementType: types.StringType},
			"aws_region":                       schema.StringAttribute{Optional: true},
			"aws_profile":                      schema.StringAttribute{Optional: true},
			"aws_access_key":                   schema.StringAttribute{Optional: true, Sensitive: true},
			"aws_secret_key":                   schema.StringAttribute{Optional: true, Sensitive: true},
			"read_only_role_arn":               schema.StringAttribute{Computed: true},
			"instrumentation_role_arn":         schema.StringAttribute{Computed: true},
			"aws_account_id":                   schema.StringAttribute{Computed: true},
		},
	}
}

func TestAwsIntegrationResource_Metadata(t *testing.T) {
	r := &AwsIntegrationResource{}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "dash0"}, resp)
	assert.Equal(t, "dash0_aws_integration", resp.TypeName)
}

func TestAwsIntegrationResource_Schema(t *testing.T) {
	r := &AwsIntegrationResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)

	// Required
	assert.True(t, resp.Schema.Attributes["dataset"].(schema.StringAttribute).Required)
	assert.True(t, resp.Schema.Attributes["external_id"].(schema.StringAttribute).Required)

	// Computed
	assert.True(t, resp.Schema.Attributes["id"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["read_only_role_arn"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["instrumentation_role_arn"].(schema.StringAttribute).Computed)
	assert.True(t, resp.Schema.Attributes["aws_account_id"].(schema.StringAttribute).Computed)

	// Optional with defaults
	assert.True(t, resp.Schema.Attributes["iam_role_name_prefix"].(schema.StringAttribute).Optional)
	assert.True(t, resp.Schema.Attributes["enable_resources_instrumentation"].(schema.BoolAttribute).Optional)
	assert.True(t, resp.Schema.Attributes["dash0_aws_account_id"].(schema.StringAttribute).Optional)

	// Sensitive
	assert.True(t, resp.Schema.Attributes["aws_access_key"].(schema.StringAttribute).Sensitive)
	assert.True(t, resp.Schema.Attributes["aws_secret_key"].(schema.StringAttribute).Sensitive)
}

func TestAwsIntegrationResource_Configure(t *testing.T) {
	r := &AwsIntegrationResource{}
	client := &MockClient{}

	// nil provider data
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{}, resp)
	assert.Nil(t, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// valid provider data
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: client}, resp)
	assert.Equal(t, client, r.client)
	assert.False(t, resp.Diagnostics.HasError())

	// invalid provider data
	resp = &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "invalid"}, resp)
	assert.True(t, resp.Diagnostics.HasError())
}

func TestAwsIntegrationOrigin_Deterministic(t *testing.T) {
	origin1 := model.AwsIntegrationOrigin("123456789012", "ext-id-1")
	origin2 := model.AwsIntegrationOrigin("123456789012", "ext-id-1")
	assert.Equal(t, origin1, origin2)
	assert.Contains(t, origin1, "terraform-")
}

func TestAwsIntegrationOrigin_UniquePerInput(t *testing.T) {
	origin1 := model.AwsIntegrationOrigin("123456789012", "ext-id-1")
	origin2 := model.AwsIntegrationOrigin("123456789012", "ext-id-2")
	origin3 := model.AwsIntegrationOrigin("999999999999", "ext-id-1")

	assert.NotEqual(t, origin1, origin2)
	assert.NotEqual(t, origin1, origin3)
}

func TestBuildAwsIntegrationDefinition_ReadOnlyOnly(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                        types.StringValue("default"),
		ExternalID:                     types.StringValue("org-tech-id"),
		ReadOnlyRoleArn:                types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		EnableResourcesInstrumentation: types.BoolValue(false),
	}

	origin := "terraform-test-origin"
	def := model.BuildAwsIntegrationDefinition(integration, "123456789012", origin)

	assert.Equal(t, "Dash0Integration", def.Kind)
	assert.Equal(t, "AWS 123456789012 (terraform)", def.Metadata.Name)
	assert.Equal(t, origin, def.Metadata.Labels.Origin)
	assert.True(t, def.Spec.Enabled)
	assert.Equal(t, "aws", def.Spec.Integration.Kind)
	assert.Equal(t, "default", def.Spec.Integration.Spec.Dataset)
	assert.Equal(t, "123456789012", def.Spec.Integration.Spec.AccountID)

	// Only read-only role
	assert.Len(t, def.Spec.Integration.Spec.Roles, 1)
	assert.Equal(t, model.PermissionTypeReadOnly, def.Spec.Integration.Spec.Roles[0].PermissionType)
	assert.Equal(t, "arn:aws:iam::123456789012:role/dash0-read-only", def.Spec.Integration.Spec.Roles[0].Arn)
	assert.Equal(t, "org-tech-id", def.Spec.Integration.Spec.Roles[0].ExternalID)
}

func TestBuildAwsIntegrationDefinition_WithInstrumentation(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                        types.StringValue("default"),
		ExternalID:                     types.StringValue("org-tech-id"),
		ReadOnlyRoleArn:                types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		InstrumentationRoleArn:         types.StringValue("arn:aws:iam::123456789012:role/dash0-instrumentation"),
		EnableResourcesInstrumentation: types.BoolValue(true),
	}

	def := model.BuildAwsIntegrationDefinition(integration, "123456789012", "terraform-test-origin")

	assert.Len(t, def.Spec.Integration.Spec.Roles, 2)
	assert.Equal(t, model.PermissionTypeReadOnly, def.Spec.Integration.Spec.Roles[0].PermissionType)
	assert.Equal(t, model.PermissionTypeResourcesInstrumentation, def.Spec.Integration.Spec.Roles[1].PermissionType)
	assert.Equal(t, "arn:aws:iam::123456789012:role/dash0-instrumentation", def.Spec.Integration.Spec.Roles[1].Arn)
}

func TestBuildAwsIntegrationDefinition_OriginInLabels(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                        types.StringValue("default"),
		ExternalID:                     types.StringValue("org-tech-id"),
		ReadOnlyRoleArn:                types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		EnableResourcesInstrumentation: types.BoolValue(false),
	}

	origin := model.AwsIntegrationOrigin("123456789012", "org-tech-id")
	def := model.BuildAwsIntegrationDefinition(integration, "123456789012", origin)

	require.NotNil(t, def.Metadata.Labels)
	assert.Equal(t, origin, def.Metadata.Labels.Origin)
	assert.Contains(t, def.Metadata.Labels.Origin, "terraform-")
}

func TestBuildAwsIntegrationDefinition_JSONRoundTrip(t *testing.T) {
	integration := model.AwsIntegration{
		Dataset:                        types.StringValue("default"),
		ExternalID:                     types.StringValue("org-tech-id"),
		ReadOnlyRoleArn:                types.StringValue("arn:aws:iam::123456789012:role/dash0-read-only"),
		InstrumentationRoleArn:         types.StringValue("arn:aws:iam::123456789012:role/dash0-instrumentation"),
		EnableResourcesInstrumentation: types.BoolValue(true),
	}

	origin := "terraform-test-origin"
	def := model.BuildAwsIntegrationDefinition(integration, "123456789012", origin)

	body, err := json.Marshal(def)
	require.NoError(t, err)

	var parsed model.IntegrationDefinition
	err = json.Unmarshal(body, &parsed)
	require.NoError(t, err)

	assert.Equal(t, def.Kind, parsed.Kind)
	assert.Equal(t, def.Metadata.Name, parsed.Metadata.Name)
	assert.Equal(t, origin, parsed.Metadata.Labels.Origin)
	assert.Equal(t, def.Spec.Integration.Spec.AccountID, parsed.Spec.Integration.Spec.AccountID)
	assert.Len(t, parsed.Spec.Integration.Spec.Roles, 2)
}

func TestImportState_ValidFormats(t *testing.T) {
	tests := []struct {
		name          string
		importID      string
		expectDataset string
		expectExtID   string
		expectPrefix  string
		expectError   bool
	}{
		{
			name:          "two parts uses default prefix",
			importID:      "default,org-tech-id",
			expectDataset: "default",
			expectExtID:   "org-tech-id",
			expectPrefix:  "dash0",
		},
		{
			name:          "three parts with custom prefix",
			importID:      "default,org-tech-id,my-prefix",
			expectDataset: "default",
			expectExtID:   "org-tech-id",
			expectPrefix:  "my-prefix",
		},
		{
			name:        "one part is invalid",
			importID:    "only-one",
			expectError: true,
		},
		{
			name:        "four parts is invalid",
			importID:    "a,b,c,d",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &AwsIntegrationResource{}
			s := awsIntegrationSchema()

			// Initialize state with empty values so SetAttribute works
			emptyState := tftypes.NewValue(tftypes.Object{
				AttributeTypes: map[string]tftypes.Type{
					"id":                               tftypes.String,
					"dataset":                          tftypes.String,
					"external_id":                      tftypes.String,
					"iam_role_name_prefix":             tftypes.String,
					"enable_resources_instrumentation": tftypes.Bool,
					"dash0_aws_account_id":             tftypes.String,
					"tags":                             tftypes.Map{ElementType: tftypes.String},
					"aws_region":                       tftypes.String,
					"aws_profile":                      tftypes.String,
					"aws_access_key":                   tftypes.String,
					"aws_secret_key":                   tftypes.String,
					"read_only_role_arn":               tftypes.String,
					"instrumentation_role_arn":         tftypes.String,
					"aws_account_id":                   tftypes.String,
				},
			}, map[string]tftypes.Value{
				"id":                               tftypes.NewValue(tftypes.String, nil),
				"dataset":                          tftypes.NewValue(tftypes.String, nil),
				"external_id":                      tftypes.NewValue(tftypes.String, nil),
				"iam_role_name_prefix":             tftypes.NewValue(tftypes.String, nil),
				"enable_resources_instrumentation": tftypes.NewValue(tftypes.Bool, nil),
				"dash0_aws_account_id":             tftypes.NewValue(tftypes.String, nil),
				"tags":                             tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
				"aws_region":                       tftypes.NewValue(tftypes.String, nil),
				"aws_profile":                      tftypes.NewValue(tftypes.String, nil),
				"aws_access_key":                   tftypes.NewValue(tftypes.String, nil),
				"aws_secret_key":                   tftypes.NewValue(tftypes.String, nil),
				"read_only_role_arn":               tftypes.NewValue(tftypes.String, nil),
				"instrumentation_role_arn":         tftypes.NewValue(tftypes.String, nil),
				"aws_account_id":                   tftypes.NewValue(tftypes.String, nil),
			})

			resp := &resource.ImportStateResponse{
				State: tfsdk.State{Schema: s, Raw: emptyState},
			}
			req := resource.ImportStateRequest{ID: tc.importID}
			r.ImportState(context.Background(), req, resp)

			if tc.expectError {
				assert.True(t, resp.Diagnostics.HasError())
				return
			}

			assert.False(t, resp.Diagnostics.HasError())

			var state model.AwsIntegration
			diags := resp.State.Get(context.Background(), &state)
			require.False(t, diags.HasError())
			assert.Equal(t, tc.expectDataset, state.Dataset.ValueString())
			assert.Equal(t, tc.expectExtID, state.ExternalID.ValueString())
			assert.Equal(t, tc.expectPrefix, state.IamRoleNamePrefix.ValueString())
		})
	}
}
