package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// providerTestConfig builds a tfsdk.Config for provider tests.
// Pass nil for any string value to leave it unset (null).
// Pass nil for maxRetries to leave it unset (null), or a pointer to an int64 to set it.
func providerTestConfig(url, authToken *string, maxRetries *int64) tfsdk.Config {
	urlVal := tftypes.NewValue(tftypes.String, nil)
	if url != nil {
		urlVal = tftypes.NewValue(tftypes.String, *url)
	}
	authTokenVal := tftypes.NewValue(tftypes.String, nil)
	if authToken != nil {
		authTokenVal = tftypes.NewValue(tftypes.String, *authToken)
	}
	maxRetriesVal := tftypes.NewValue(tftypes.Number, nil)
	if maxRetries != nil {
		maxRetriesVal = tftypes.NewValue(tftypes.Number, *maxRetries)
	}

	return tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":         tftypes.String,
				"auth_token":  tftypes.String,
				"max_retries": tftypes.Number,
			},
		}, map[string]tftypes.Value{
			"url":         urlVal,
			"auth_token":  authTokenVal,
			"max_retries": maxRetriesVal,
		}),
		Schema: schema.Schema{
			Attributes: map[string]schema.Attribute{
				"url": schema.StringAttribute{
					Optional: true,
				},
				"auth_token": schema.StringAttribute{
					Optional:  true,
					Sensitive: true,
				},
				"max_retries": schema.Int64Attribute{
					Optional: true,
				},
			},
		},
	}
}

func strPtr(s string) *string { return &s }
func int64Ptr(n int64) *int64 { return &n }

func TestDash0Provider_Metadata(t *testing.T) {
	p := &dash0Provider{version: "1.0.0"}
	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)

	assert.Equal(t, "dash0", resp.TypeName)
	assert.Equal(t, "1.0.0", resp.Version)
}

func TestDash0Provider_Schema(t *testing.T) {
	p := &dash0Provider{}
	resp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, resp)

	assert.NotNil(t, resp.Schema)
	assert.Contains(t, resp.Schema.Description, "observability platform")

	// Verify schema attributes
	assert.Contains(t, resp.Schema.Attributes, "url")
	assert.Contains(t, resp.Schema.Attributes, "auth_token")
	assert.Contains(t, resp.Schema.Attributes, "max_retries")

	// Check specific attribute properties
	urlAttr := resp.Schema.Attributes["url"].(schema.StringAttribute)
	assert.True(t, urlAttr.Optional)
	assert.Contains(t, urlAttr.Description, "base URL")

	authTokenAttr := resp.Schema.Attributes["auth_token"].(schema.StringAttribute)
	assert.True(t, authTokenAttr.Optional)
	assert.True(t, authTokenAttr.Sensitive)
	assert.Contains(t, authTokenAttr.Description, "auth token")

	maxRetriesAttr := resp.Schema.Attributes["max_retries"].(schema.Int64Attribute)
	assert.True(t, maxRetriesAttr.Optional)
	assert.Contains(t, maxRetriesAttr.Description, "retries")
}

func TestDash0Provider_Configure_WithEnvironmentVariables(t *testing.T) {
	t.Setenv("DASH0_URL", "https://api.example.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token_123")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil)}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.DataSourceData)
}

func TestDash0Provider_Configure_WithProviderAttributes(t *testing.T) {
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(strPtr("https://api.provider.com"), strPtr("auth_provider_token_456"), nil),
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.DataSourceData)
}

func TestDash0Provider_Configure_EnvironmentVariablesPrecedence(t *testing.T) {
	t.Setenv("DASH0_URL", "https://api.env.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_env_token_789")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(strPtr("https://api.provider.com"), strPtr("auth_provider_token_456"), nil),
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.DataSourceData)
}

func TestDash0Provider_Configure_MissingURL(t *testing.T) {
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(nil, strPtr("auth_token_only"), nil),
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "url")
}

func TestDash0Provider_Configure_MissingAuthToken(t *testing.T) {
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(strPtr("https://api.example.com"), nil, nil),
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 Auth Token")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "auth_token")
}

func TestDash0Provider_Configure_MissingBoth(t *testing.T) {
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil)}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 2)
}

func TestDash0Provider_Configure_MaxRetries(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string // DASH0_MAX_RETRIES env var; empty means unset
		attrValue    *int64 // max_retries provider attribute; nil means unset
		expectError  bool
		errorSummary string
		errorDetail  string
	}{
		// --- env var cases ---
		{
			name:     "env: valid value 0 (retries disabled)",
			envValue: "0",
		},
		{
			name:     "env: valid value 3",
			envValue: "3",
		},
		{
			name:     "env: valid value 5 (maximum)",
			envValue: "5",
		},
		{
			name:         "env: non-integer value",
			envValue:     "yolo",
			expectError:  true,
			errorSummary: "Invalid DASH0_MAX_RETRIES",
			errorDetail:  "must be a valid integer",
		},
		{
			name:         "env: floating point value",
			envValue:     "2.5",
			expectError:  true,
			errorSummary: "Invalid DASH0_MAX_RETRIES",
			errorDetail:  "must be a valid integer",
		},
		{
			name:         "env: negative value",
			envValue:     "-1",
			expectError:  true,
			errorSummary: "Invalid max_retries",
			errorDetail:  "must be between 0 and 5",
		},
		{
			name:         "env: exceeds maximum",
			envValue:     "42",
			expectError:  true,
			errorSummary: "Invalid max_retries",
			errorDetail:  "must be between 0 and 5",
		},
		// --- provider attribute cases ---
		{
			name:      "attr: valid value 0 (retries disabled)",
			attrValue: int64Ptr(0),
		},
		{
			name:      "attr: valid value 2",
			attrValue: int64Ptr(2),
		},
		{
			name:      "attr: valid value 5 (maximum)",
			attrValue: int64Ptr(5),
		},
		{
			name:         "attr: negative value",
			attrValue:    int64Ptr(-1),
			expectError:  true,
			errorSummary: "Invalid max_retries",
			errorDetail:  "must be between 0 and 5",
		},
		{
			name:         "attr: exceeds maximum",
			attrValue:    int64Ptr(42),
			expectError:  true,
			errorSummary: "Invalid max_retries",
			errorDetail:  "must be between 0 and 5",
		},
		// --- precedence ---
		{
			name:      "env takes precedence over attr",
			envValue:  "1",
			attrValue: int64Ptr(4),
		},
		{
			name:         "env takes precedence over attr (env invalid)",
			envValue:     "99",
			attrValue:    int64Ptr(2),
			expectError:  true,
			errorSummary: "Invalid max_retries",
			errorDetail:  "DASH0_MAX_RETRIES environment variable",
		},
		// --- default ---
		{
			name: "unset uses default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DASH0_URL", "https://api.example.com")
			t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token_123")
			if tt.envValue != "" {
				t.Setenv("DASH0_MAX_RETRIES", tt.envValue)
			}

			p := &dash0Provider{}
			req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, tt.attrValue)}
			resp := &provider.ConfigureResponse{}

			p.Configure(context.Background(), req, resp)

			if tt.expectError {
				assert.True(t, resp.Diagnostics.HasError())
				require.Len(t, resp.Diagnostics.Errors(), 1)
				assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), tt.errorSummary)
				assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), tt.errorDetail)
			} else {
				assert.False(t, resp.Diagnostics.HasError())
				assert.NotNil(t, resp.ResourceData)
			}
		})
	}
}

func TestDash0Provider_DataSources(t *testing.T) {
	p := &dash0Provider{}
	dataSources := p.DataSources(context.Background())
	assert.Empty(t, dataSources)
}

func TestDash0Provider_Resources(t *testing.T) {
	p := &dash0Provider{}
	resources := p.Resources(context.Background())
	assert.NotEmpty(t, resources)
	assert.Len(t, resources, 7) // DashboardResource, SyntheticCheckResource, ViewResource, CheckRuleResource, RecordingRuleResource, NotificationChannelResource, SpamFilterResource
}
