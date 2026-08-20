package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// otlpProfilesFixture mirrors profilesFixture but adds an OTLP URL, so tests can
// verify that the provider picks up the dash0 CLI profile's otlpUrl field.
const otlpProfilesFixture = `{
  "profiles": [
    {
      "name": "withOtlp",
      "configuration": {
        "apiUrl": "https://api.us-west-2.aws.dash0.com",
        "authToken": "auth_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "otlpUrl": "https://ingress.us-west-2.aws.dash0.com"
      }
    },
    {
      "name": "withoutOtlp",
      "configuration": {
        "apiUrl": "https://api.us-west-1.aws.dash0.com",
        "authToken": "auth_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  ]
}`

// writeBrokenProfiles points DASH0_CONFIG_DIR at a config dir whose profiles.json
// is malformed, so profile loading fails with something other than
// ErrNoActiveProfile.
func writeBrokenProfiles(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "activeProfile"), []byte("broken"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "profiles.json"), []byte("{not json"), 0o600))
	t.Setenv("DASH0_CONFIG_DIR", dir)
}

func TestResolveAuthInfo_OtlpURL(t *testing.T) {
	ctx := context.Background()

	t.Run("from attribute", func(t *testing.T) {
		clearCredentialEnv(t)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			URL:       types.StringValue("https://api.example.com"),
			AuthToken: types.StringValue("auth_attr"),
			OtlpURL:   types.StringValue("https://ingress.example.com"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://ingress.example.com", auth.otlpURL)
	})

	t.Run("env takes precedence over attribute", func(t *testing.T) {
		clearCredentialEnv(t)
		t.Setenv("DASH0_OTLP_URL", "https://ingress.from-env.example.com")

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			URL:       types.StringValue("https://api.example.com"),
			AuthToken: types.StringValue("auth_attr"),
			OtlpURL:   types.StringValue("https://ingress.from-attribute.example.com"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://ingress.from-env.example.com", auth.otlpURL)
	})

	t.Run("from profile", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "withOtlp", otlpProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.Equal(t, "https://ingress.us-west-2.aws.dash0.com", auth.otlpURL)
		assert.Equal(t, "https://api.us-west-2.aws.dash0.com", auth.url)
	})

	t.Run("not adopted from profile when url/token come from env", func(t *testing.T) {
		// Regression guard: the profile's OTLP URL must only be adopted when
		// the API URL also came from that same profile. Otherwise env
		// credentials for one region/tenant could silently pair with a
		// different profile's OTLP endpoint, surfacing as an opaque 401 or
		// events landing in the wrong place.
		clearCredentialEnv(t)
		t.Setenv("DASH0_API_URL", "https://api.from-env.example.com")
		t.Setenv("DASH0_AUTH_TOKEN", "auth_from_env")
		setupCLIConfigDir(t, "withOtlp", otlpProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.Equal(t, "https://api.from-env.example.com", auth.url, "env credentials must still win")
		assert.Equal(t, "auth_from_env", auth.token)
		assert.Empty(t, auth.otlpURL, "the profile's OTLP URL must not be adopted when the API URL did not come from that profile")
	})

	t.Run("absent everywhere is not an error", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "withoutOtlp", otlpProfilesFixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.Empty(t, auth.otlpURL, "only the actions need an OTLP URL")
	})
}

func TestResolveAuthInfo_BrokenProfileHandling(t *testing.T) {
	ctx := context.Background()

	t.Run("tolerated when credentials already complete", func(t *testing.T) {
		// Regression guard: resolving the OTLP URL made the profile lookup
		// reachable for configurations that previously never touched it. A
		// malformed profiles.json must not break an env-var-only setup.
		clearCredentialEnv(t)
		writeBrokenProfiles(t)
		t.Setenv("DASH0_API_URL", "https://api.from-env.example.com")
		t.Setenv("DASH0_AUTH_TOKEN", "auth_from_env")

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err, "a broken profiles file must not fail a fully-configured provider")
		assert.Equal(t, "https://api.from-env.example.com", auth.url)
		assert.Empty(t, auth.otlpURL)
	})

	t.Run("surfaced when credentials incomplete", func(t *testing.T) {
		// The converse: when the profile is the only possible source of
		// credentials, its failure must still surface.
		clearCredentialEnv(t)
		writeBrokenProfiles(t)

		_, err := resolveAuthInfo(ctx, &providerConfigModel{})
		assert.Error(t, err)
	})

	t.Run("named profile failure tolerated when credentials already complete", func(t *testing.T) {
		// Regression guard: when url+token are already complete, the profile
		// is only ever consulted opportunistically for the OTLP URL. An
		// explicit but nonexistent `profile` must not turn a previously
		// working env-var/CI setup into a hard failure on upgrade.
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "", otlpProfilesFixture)
		t.Setenv("DASH0_API_URL", "https://api.from-env.example.com")
		t.Setenv("DASH0_AUTH_TOKEN", "auth_from_env")

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			Profile: types.StringValue("does-not-exist"),
		})
		require.NoError(t, err)
		assert.Equal(t, "https://api.from-env.example.com", auth.url)
	})

	t.Run("named profile failure surfaced when credentials incomplete", func(t *testing.T) {
		// The converse: when the profile is actually needed to resolve
		// missing credentials, an explicit but nonexistent profile name is a
		// real error — the user asked for it by name.
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "", otlpProfilesFixture)

		_, err := resolveAuthInfo(ctx, &providerConfigModel{
			Profile: types.StringValue("does-not-exist"),
		})
		assert.Error(t, err)
	})
}

func TestDash0Provider_Configure_OtlpURLAttribute(t *testing.T) {
	// Exercises otlp_url through the real provider schema, which is what catches
	// a mismatch between providerConfigModel and the declared attributes.
	clearCredentialEnv(t)

	p := &dash0Provider{version: "1.0.0"}
	req := provider.ConfigureRequest{Config: providerTestConfigWithOtlpURL(
		strPtr("https://api.example.com"),
		strPtr("auth_test_token_123"),
		nil,
		strPtr("https://ingress.example.com"),
		nil,
	)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	assert.NotNil(t, resp.ResourceData)
	assert.NotNil(t, resp.ActionData, "actions need the client to be published as ActionData")
}

func TestDash0Provider_Configure_DowngradesInvalidOtlpURLToWarning(t *testing.T) {
	// The library appends /v1/logs itself, so a URL that already ends in a
	// signal path is rejected — but that must only break the two log-event
	// actions, not every resource and data source: Configure retries without
	// otlp_url and downgrades to a warning instead of failing outright.
	clearCredentialEnv(t)

	p := &dash0Provider{version: "1.0.0"}
	req := provider.ConfigureRequest{Config: providerTestConfigWithOtlpURL(
		strPtr("https://api.example.com"),
		strPtr("auth_test_token_123"),
		nil,
		strPtr("https://ingress.example.com/v1/logs"),
		nil,
	)}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), req, resp)

	require.False(t, resp.Diagnostics.HasError(), "diagnostics: %v", resp.Diagnostics)
	require.NotEmpty(t, resp.Diagnostics.Warnings(), "an invalid otlp_url must produce a warning")
	assert.Contains(t, resp.Diagnostics.Warnings()[0].Summary(), "Invalid Dash0 OTLP Endpoint")
	assert.NotNil(t, resp.ResourceData, "resources must not be affected by a bad otlp_url")
	assert.NotNil(t, resp.ActionData)
}

func TestDash0Provider_Actions(t *testing.T) {
	p := &dash0Provider{version: "1.0.0"}
	actions := p.Actions(context.Background())
	require.Len(t, actions, 2)

	names := map[string]bool{}
	for _, newAction := range actions {
		resp := &action.MetadataResponse{}
		newAction().Metadata(context.Background(), action.MetadataRequest{ProviderTypeName: "dash0"}, resp)
		names[resp.TypeName] = true
	}

	assert.True(t, names["dash0_log_event"], "expected dash0_log_event, got %v", names)
	assert.True(t, names["dash0_deployment_event"], "expected dash0_deployment_event, got %v", names)
}
