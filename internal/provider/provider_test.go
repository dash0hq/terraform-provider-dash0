package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// profilesFixture is a small set of profiles used by tests that exercise the
// CLI-profile fallback. test1 has the real-looking credentials; test2 is a
// second profile so tests can verify named-profile lookup; "empty" has blank
// values so tests can verify attribute-over-profile precedence.
const profilesFixture = `{
  "profiles": [
    {
      "name": "test1",
      "configuration": {
        "apiUrl": "https://api.us-west-2.aws.dash0.com",
        "authToken": "auth_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
    },
    {
      "name": "test2",
      "configuration": {
        "apiUrl": "https://api.us-west-1.aws.dash0.com",
        "authToken": "auth_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    },
    {
      "name": "empty",
      "configuration": {"apiUrl": "", "authToken": ""}
    }
  ]
}`

// setupCLIConfigDir writes the given activeProfile content and profiles.json
// content into a temp directory and points DASH0_CONFIG_DIR at it. Pass an
// empty string to skip writing the corresponding file.
func setupCLIConfigDir(t *testing.T, activeProfile, profilesJSON string) string {
	t.Helper()
	dir := t.TempDir()
	if activeProfile != "" {
		if err := os.WriteFile(filepath.Join(dir, "activeProfile"), []byte(activeProfile), 0o600); err != nil {
			t.Fatalf("write activeProfile: %v", err)
		}
	}
	if profilesJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "profiles.json"), []byte(profilesJSON), 0o600); err != nil {
			t.Fatalf("write profiles.json: %v", err)
		}
	}
	t.Setenv("DASH0_CONFIG_DIR", dir)
	return dir
}

// clearCredentialEnv blanks out every env var that resolveAuthInfo reads, and
// points DASH0_CONFIG_DIR at a non-existent path so the test never picks up
// the developer's real ~/.dash0. Individual tests can override the config dir.
func clearCredentialEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DASH0_API_URL", "")
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	t.Setenv("DASH0_CONFIG_DIR", filepath.Join(t.TempDir(), "no-config-here"))
}

// providerTestConfig builds a tfsdk.Config for provider tests. Pass nil for
// any value to leave it unset (null).
func providerTestConfig(url, authToken, profile *string, maxRetries *int64) tfsdk.Config {
	stringVal := func(p *string) tftypes.Value {
		if p == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *p)
	}
	numberVal := func(p *int64) tftypes.Value {
		if p == nil {
			return tftypes.NewValue(tftypes.Number, nil)
		}
		return tftypes.NewValue(tftypes.Number, *p)
	}
	return tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":         tftypes.String,
				"auth_token":  tftypes.String,
				"profile":     tftypes.String,
				"max_retries": tftypes.Number,
			},
		}, map[string]tftypes.Value{
			"url":         stringVal(url),
			"auth_token":  stringVal(authToken),
			"profile":     stringVal(profile),
			"max_retries": numberVal(maxRetries),
		}),
		Schema: providerSchema(),
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

	for _, name := range []string{"url", "auth_token", "profile", "max_retries"} {
		assert.Contains(t, resp.Schema.Attributes, name)
	}

	urlAttr := resp.Schema.Attributes["url"].(schema.StringAttribute)
	assert.True(t, urlAttr.Optional)
	assert.Contains(t, urlAttr.Description, "DASH0_API_URL")

	authTokenAttr := resp.Schema.Attributes["auth_token"].(schema.StringAttribute)
	assert.True(t, authTokenAttr.Optional)
	assert.True(t, authTokenAttr.Sensitive)

	profileAttr := resp.Schema.Attributes["profile"].(schema.StringAttribute)
	assert.True(t, profileAttr.Optional)
	assert.Contains(t, profileAttr.Description, "dash0 CLI")

	maxRetriesAttr := resp.Schema.Attributes["max_retries"].(schema.Int64Attribute)
	assert.True(t, maxRetriesAttr.Optional)
}

func TestDash0Provider_Configure_WithEnvironmentVariables(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("DASH0_API_URL", "https://api.example.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token_123")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
}

func TestDash0Provider_Configure_DASH0URL_LegacyFallback(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("DASH0_URL", "https://api.legacy.example.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_legacy_token")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
}

func TestDash0Provider_Configure_WithProviderAttributes(t *testing.T) {
	clearCredentialEnv(t)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(
			strPtr("https://api.provider.com"),
			strPtr("auth_provider_token"),
			nil, nil,
		),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
	assert.NotNil(t, resp.ResourceData)
}

func TestDash0Provider_Configure_EnvironmentVariablesPrecedence(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("DASH0_API_URL", "https://api.env.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_env_token")

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(
			strPtr("https://api.provider.com"),
			strPtr("auth_provider_token"),
			nil, nil,
		),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError())
}

func TestDash0Provider_Configure_MissingURL(t *testing.T) {
	clearCredentialEnv(t)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(nil, strPtr("auth_token_only"), nil, nil),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
}

func TestDash0Provider_Configure_MissingAuthToken(t *testing.T) {
	clearCredentialEnv(t)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(strPtr("https://api.example.com"), nil, nil, nil),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 Auth Token")
}

func TestDash0Provider_Configure_MissingBoth(t *testing.T) {
	clearCredentialEnv(t)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 2)
}

func TestDash0Provider_Configure_LoadsFromActiveProfile(t *testing.T) {
	clearCredentialEnv(t)
	setupCLIConfigDir(t, "test1", profilesFixture)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics.Errors())
	assert.NotNil(t, resp.ResourceData)
}

func TestDash0Provider_Configure_LoadsFromNamedProfile(t *testing.T) {
	clearCredentialEnv(t)
	// activeProfile points to test1, but the provider should honor the explicit
	// `profile` attribute and load test2 instead.
	setupCLIConfigDir(t, "test1", profilesFixture)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(nil, nil, strPtr("test2"), nil),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics.Errors())
	assert.NotNil(t, resp.ResourceData)
}

func TestDash0Provider_Configure_NamedProfileNotFound(t *testing.T) {
	clearCredentialEnv(t)
	setupCLIConfigDir(t, "test1", profilesFixture)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(nil, nil, strPtr("does-not-exist"), nil),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.GreaterOrEqual(t, len(resp.Diagnostics.Errors()), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Unable to load credentials")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), `"does-not-exist"`)
}

func TestDash0Provider_Configure_AttributesFillGapsInProfile(t *testing.T) {
	clearCredentialEnv(t)
	// Active profile is "empty" (blank ApiUrl/AuthToken). The provider should
	// still succeed because the attributes supply the missing pieces.
	setupCLIConfigDir(t, "empty", profilesFixture)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{
		Config: providerTestConfig(
			strPtr("https://api.attr.com"),
			strPtr("auth_attr_token"),
			nil, nil,
		),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics.Errors())
}

func TestDash0Provider_Configure_MalformedProfilesJSONSurfacesError(t *testing.T) {
	clearCredentialEnv(t)
	setupCLIConfigDir(t, "test1", `{"profiles": [{not valid json}]}`)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	// First diagnostic must reference the CLI-profile loading failure rather
	// than swallowing it under a bare "Missing Dash0 URL" message.
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Unable to load credentials")
}

func TestDash0Provider_Configure_NoActiveProfileIsSilent(t *testing.T) {
	clearCredentialEnv(t)
	// CLI config directory exists but has no activeProfile file. The provider
	// must not surface a CLI-loading error; only the credentials-missing
	// diagnostics should appear.
	setupCLIConfigDir(t, "", profilesFixture)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Errors(), 2)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
	assert.Contains(t, resp.Diagnostics.Errors()[1].Summary(), "Missing Dash0 Auth Token")
}

func TestDash0Provider_Configure_MaxRetries(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		attrValue    *int64
		expectError  bool
		errorSummary string
		errorDetail  string
	}{
		{name: "env: valid value 0 (retries disabled)", envValue: "0"},
		{name: "env: valid value 3", envValue: "3"},
		{name: "env: valid value 5 (maximum)", envValue: "5"},
		{
			name: "env: non-integer value", envValue: "yolo",
			expectError: true, errorSummary: "Invalid DASH0_MAX_RETRIES", errorDetail: "must be a valid integer",
		},
		{
			name: "env: floating point value", envValue: "2.5",
			expectError: true, errorSummary: "Invalid DASH0_MAX_RETRIES", errorDetail: "must be a valid integer",
		},
		{
			name: "env: negative value", envValue: "-1",
			expectError: true, errorSummary: "Invalid max_retries", errorDetail: "must be between 0 and 5",
		},
		{
			name: "env: exceeds maximum", envValue: "42",
			expectError: true, errorSummary: "Invalid max_retries", errorDetail: "must be between 0 and 5",
		},
		{name: "attr: valid value 0 (retries disabled)", attrValue: int64Ptr(0)},
		{name: "attr: valid value 2", attrValue: int64Ptr(2)},
		{name: "attr: valid value 5 (maximum)", attrValue: int64Ptr(5)},
		{
			name: "attr: negative value", attrValue: int64Ptr(-1),
			expectError: true, errorSummary: "Invalid max_retries", errorDetail: "must be between 0 and 5",
		},
		{
			name: "attr: exceeds maximum", attrValue: int64Ptr(42),
			expectError: true, errorSummary: "Invalid max_retries", errorDetail: "must be between 0 and 5",
		},
		{name: "env takes precedence over attr", envValue: "1", attrValue: int64Ptr(4)},
		{
			name: "env takes precedence over attr (env invalid)", envValue: "99", attrValue: int64Ptr(2),
			expectError: true, errorSummary: "Invalid max_retries", errorDetail: "DASH0_MAX_RETRIES environment variable",
		},
		{name: "unset uses default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearCredentialEnv(t)
			t.Setenv("DASH0_API_URL", "https://api.example.com")
			t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token_123")
			if tt.envValue != "" {
				t.Setenv("DASH0_MAX_RETRIES", tt.envValue)
			}

			p := &dash0Provider{}
			req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, tt.attrValue)}
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
	assert.Len(t, resources, 7)
}

// TestResolveAuthInfo_Precedence pins the precedence order in a single place
// without going through Configure's diagnostic plumbing.
func TestResolveAuthInfo_Precedence(t *testing.T) {
	ctx := context.Background()

	t.Run("env beats attr beats profile", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "test1", profilesFixture)
		t.Setenv("DASH0_API_URL", "https://env.example.com")
		t.Setenv("DASH0_AUTH_TOKEN", "auth_env")

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			URL:       types.StringValue("https://attr.example.com"),
			AuthToken: types.StringValue("auth_attr"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://env.example.com", auth.url)
		assert.Equal(t, "auth_env", auth.token)
		assert.False(t, auth.isOAuth)
	})

	t.Run("attr beats profile when env absent", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "test1", profilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			URL:       types.StringValue("https://attr.example.com"),
			AuthToken: types.StringValue("auth_attr"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://attr.example.com", auth.url)
		assert.Equal(t, "auth_attr", auth.token)
		assert.False(t, auth.isOAuth)
	})

	t.Run("active profile fills both when env and attr absent", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "test1", profilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.Equal(t, "https://api.us-west-2.aws.dash0.com", auth.url)
		assert.Equal(t, "auth_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", auth.token)
		assert.False(t, auth.isOAuth)
	})

	t.Run("named profile overrides active when env and attr absent", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "test1", profilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			Profile: types.StringValue("test2"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://api.us-west-1.aws.dash0.com", auth.url)
		assert.Equal(t, "auth_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", auth.token)
		assert.False(t, auth.isOAuth)
	})
}

// TestResolveAuthInfo_OAuth tests OAuth-specific behavior.
func TestResolveAuthInfo_OAuth(t *testing.T) {
	ctx := context.Background()

	oauthProfilesFixture := `{
  "profiles": [
    {
      "name": "oauth-profile",
      "configuration": {
        "apiUrl": "https://api.us-west-2.aws.dash0.com",
        "authToken": "dash0_at_oauth-access-token",
        "oauth": {
          "clientId": "client-id-123",
          "refreshToken": "refresh-token-456",
          "expiresAt": "2099-12-31T23:59:59Z"
        }
      }
    },
    {
      "name": "static-profile",
      "configuration": {
        "apiUrl": "https://api.us-west-1.aws.dash0.com",
        "authToken": "auth_static_token"
      }
    }
  ]
}`

	t.Run("active OAuth profile sets isOAuth flag", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "oauth-profile", oauthProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.Equal(t, "https://api.us-west-2.aws.dash0.com", auth.url)
		assert.Equal(t, "dash0_at_oauth-access-token", auth.token)
		assert.True(t, auth.isOAuth)
	})

	t.Run("named OAuth profile sets isOAuth flag", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "static-profile", oauthProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			Profile: types.StringValue("oauth-profile"),
		})
		require.NoError(t, err)
		assert.True(t, auth.isOAuth)
	})

	t.Run("env auth token overrides OAuth profile and clears isOAuth", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "oauth-profile", oauthProfilesFixture)
		t.Setenv("DASH0_AUTH_TOKEN", "auth_env_override")

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.Equal(t, "auth_env_override", auth.token)
		assert.False(t, auth.isOAuth)
	})

	t.Run("attr auth token overrides OAuth profile and clears isOAuth", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "oauth-profile", oauthProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			AuthToken: types.StringValue("auth_attr_override"),
		})
		require.NoError(t, err)
		assert.Equal(t, "auth_attr_override", auth.token)
		assert.False(t, auth.isOAuth)
	})

	t.Run("static named profile does not set isOAuth", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "oauth-profile", oauthProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			Profile: types.StringValue("static-profile"),
		})
		require.NoError(t, err)
		assert.Equal(t, "auth_static_token", auth.token)
		assert.False(t, auth.isOAuth)
	})
}

// TestDash0Provider_Configure_OAuthProfile verifies that an OAuth-enabled CLI
// profile is accepted by Configure (JWT tokens do not start with "auth_").
func TestDash0Provider_Configure_OAuthProfile(t *testing.T) {
	clearCredentialEnv(t)
	oauthProfilesFixture := `{
  "profiles": [
    {
      "name": "oauth",
      "configuration": {
        "apiUrl": "https://api.us-west-2.aws.dash0.com",
        "authToken": "dash0_at_oauth-access-token",
        "oauth": {
          "clientId": "cid",
          "refreshToken": "rt",
          "expiresAt": "2099-12-31T23:59:59Z"
        }
      }
    }
  ]
}`
	setupCLIConfigDir(t, "oauth", oauthProfilesFixture)

	p := &dash0Provider{}
	req := provider.ConfigureRequest{Config: providerTestConfig(nil, nil, nil, nil)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	assert.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics.Errors())
	assert.NotNil(t, resp.ResourceData)
}
