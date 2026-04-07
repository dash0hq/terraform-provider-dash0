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
	assert.Contains(t, resp.Schema.Description, "Dash0 observability platform")

	// Verify schema attributes
	assert.Contains(t, resp.Schema.Attributes, "url")
	assert.Contains(t, resp.Schema.Attributes, "auth_token")

	// Check specific attribute properties
	urlAttr := resp.Schema.Attributes["url"].(schema.StringAttribute)
	assert.True(t, urlAttr.Optional)
	assert.Contains(t, urlAttr.Description, "Dash0 base URL")

	authTokenAttr := resp.Schema.Attributes["auth_token"].(schema.StringAttribute)
	assert.True(t, authTokenAttr.Optional)
	assert.True(t, authTokenAttr.Sensitive)
	assert.Contains(t, authTokenAttr.Description, "Dash0 auth token")
}

func TestDash0Provider_Configure_WithEnvironmentVariables(t *testing.T) {
	// Set environment variables
	t.Setenv("DASH0_URL", "https://api.example.com")
	t.Setenv("DASH0_AUTH_TOKEN", "test_token_123")

	p := &dash0Provider{}

	// Create empty config (no provider attributes set)
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
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
			},
		},
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.DataSourceData)
}

func TestDash0Provider_Configure_WithProviderAttributes(t *testing.T) {
	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}

	// Create config with provider attributes
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.provider.com"),
			"auth_token": tftypes.NewValue(tftypes.String, "provider_token_456"),
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
			},
		},
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.DataSourceData)
}

func TestDash0Provider_Configure_EnvironmentVariablesPrecedence(t *testing.T) {
	// Set environment variables - these should take precedence
	t.Setenv("DASH0_URL", "https://api.env.com")
	t.Setenv("DASH0_AUTH_TOKEN", "env_token_789")

	p := &dash0Provider{}

	// Create config with different provider attributes
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.provider.com"),
			"auth_token": tftypes.NewValue(tftypes.String, "provider_token_456"),
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
			},
		},
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	// Environment variables should take precedence, so configuration should succeed
	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.DataSourceData)
}

func TestDash0Provider_Configure_MissingURL(t *testing.T) {
	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}

	// Create config with only auth_token
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "token_only"),
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
			},
		},
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "url")
}

func TestDash0Provider_Configure_MissingAuthToken(t *testing.T) {
	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}

	// Create config with only url
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.example.com"),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
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
			},
		},
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 Auth Token")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "auth_token")
}

func TestDash0Provider_Configure_MissingBoth(t *testing.T) {
	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	p := &dash0Provider{}

	// Create empty config
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
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
			},
		},
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 2)
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
	assert.Len(t, resources, 4) // DashboardResource, SyntheticCheckResource, ViewResource, CheckRuleResource
}
