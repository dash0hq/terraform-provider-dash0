package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dash0 "github.com/dash0hq/dash0-api-client-go"
)

// oauthTokenServer stands in for the Dash0 authorization server and records how
// often a refresh-grant exchange happened.
type oauthTokenServer struct {
	*httptest.Server

	mu    sync.Mutex
	calls int
}

func newOAuthTokenServer(t *testing.T) *oauthTokenServer {
	t.Helper()
	s := &oauthTokenServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.mu.Lock()
		s.calls++
		call := s.calls
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("dash0_at_refreshed-%d", call),
			"refresh_token": fmt.Sprintf("rt-%d", call),
			"token_type":    "Bearer",
			"expires_in":    int64((15 * time.Minute).Seconds()),
		})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *oauthTokenServer) exchanges() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// oauthProfilesJSON builds a profiles.json holding one OAuth profile and one
// static profile, with the OAuth access token expiring at the given offset.
func oauthProfilesJSON(apiUrl string, expiresAt time.Time) string {
	return fmt.Sprintf(`{
  "profiles": [
    {
      "name": "oauth-profile",
      "configuration": {
        "apiUrl": %q,
        "authToken": "dash0_at_stale",
        "oauth": {
          "clientId": "cid",
          "refreshToken": "rt-0",
          "expiresAt": %q
        }
      }
    },
    {
      "name": "static-profile",
      "configuration": {
        "apiUrl": %q,
        "authToken": "auth_static_token"
      }
    }
  ]
}`, apiUrl, expiresAt.UTC().Format(time.RFC3339Nano), apiUrl)
}

// TestResolveAuthInfo_RecordsProfileName pins the plumbing the refresh depends
// on: without a profile name the token provider has no profile to write
// refreshed tokens back to.
func TestResolveAuthInfo_RecordsProfileName(t *testing.T) {
	ctx := context.Background()
	server := newOAuthTokenServer(t)
	fixture := oauthProfilesJSON(server.URL, time.Now().Add(1*time.Hour))

	t.Run("active profile", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "oauth-profile", fixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		require.NotNil(t, auth.profileCfg)
		assert.Equal(t, "oauth-profile", auth.profileCfg.ProfileName)
	})

	t.Run("named profile", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "static-profile", fixture)

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{
			Profile: types.StringValue("oauth-profile"),
		})
		require.NoError(t, err)
		require.NotNil(t, auth.profileCfg)
		assert.Equal(t, "oauth-profile", auth.profileCfg.ProfileName)
	})
}

// TestAuthInfoTokenProvider_NamedOAuthProfileRefreshes is the regression test for
// the named-profile half of #161.
//
// Before the fix, `provider "dash0" { profile = "production" }` used whatever
// access token was on disk without ever refreshing it, so it failed as soon as
// that token was more than 15 minutes old. Only the active-profile path
// refreshed.
//
// The refresh happens in the token provider, not while loading the profile, so
// a run that never uses the profile's credentials never spends an exchange.
func TestAuthInfoTokenProvider_NamedOAuthProfileRefreshes(t *testing.T) {
	ctx := context.Background()
	server := newOAuthTokenServer(t)
	// Already expired, which is the normal state of a stored token: they live
	// for 15 minutes and Terraform runs are not that frequent.
	fixture := oauthProfilesJSON(server.URL, time.Now().Add(-1*time.Hour))

	clearCredentialEnv(t)
	setupCLIConfigDir(t, "static-profile", fixture)

	auth, err := resolveAuthInfo(ctx, &providerConfigModel{
		Profile: types.StringValue("oauth-profile"),
	})
	require.NoError(t, err)
	require.True(t, auth.isOAuth)
	// Loading the profile is a disk read. It reports the stale token and does
	// not exchange anything, so a run that authenticates with an auth_token
	// from the provider block never rotates the profile's refresh token.
	assert.Equal(t, "dash0_at_stale", auth.token,
		"loading a profile must not exchange a token")
	assert.Equal(t, 0, server.exchanges(), "expected no refresh-grant exchange at load time")

	// The provider is what refreshes, and it does so before the first request
	// rather than leaving Terraform with the expired token.
	provider := auth.tokenProvider()
	authToken, err := provider.AuthToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "dash0_at_refreshed-1", authToken,
		"a named OAuth profile must refresh its expired access token")
	assert.Equal(t, 1, server.exchanges(), "expected exactly one refresh-grant exchange")

	// The fresh token is then served from memory.
	authToken, err = provider.AuthToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "dash0_at_refreshed-1", authToken)
	assert.Equal(t, 1, server.exchanges(), "the fresh token must not trigger a second exchange")
}

// TestAuthInfoTokenProvider_RefreshesForTheWholeRun covers the long-apply half of
// #161: the provider is configured once, but the token it authenticates with
// must not be frozen at that moment.
func TestAuthInfoTokenProvider_RefreshesForTheWholeRun(t *testing.T) {
	ctx := context.Background()
	server := newOAuthTokenServer(t)
	// Inside the five-minute refresh threshold.
	fixture := oauthProfilesJSON(server.URL, time.Now().Add(1*time.Minute))

	clearCredentialEnv(t)
	setupCLIConfigDir(t, "oauth-profile", fixture)

	auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
	require.NoError(t, err)

	provider := auth.tokenProvider()
	refresher, ok := provider.(dash0.RefreshingAuthTokenProvider)
	require.True(t, ok, "an OAuth profile must yield a refreshing provider so a 401 can be recovered from")

	first, err := provider.AuthToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "dash0_at_refreshed-1", first)

	// A second request inside the new token's validity must reuse it rather than
	// exchange again, or a long apply would hammer the token endpoint.
	second, err := provider.AuthToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, server.exchanges())

	// A 401 forces a replacement even though the current token looks healthy.
	// Passing the token that was rejected is what lets the provider tell this
	// apart from a sibling request that already rotated it.
	forced, err := refresher.ForceRefreshAuthToken(ctx, second)
	require.NoError(t, err)
	assert.Equal(t, "dash0_at_refreshed-2", forced)
	assert.Equal(t, 2, server.exchanges())

	// A caller rejected while holding an already-superseded token gets the
	// current one back rather than triggering another rotation.
	deduped, err := refresher.ForceRefreshAuthToken(ctx, second)
	require.NoError(t, err)
	assert.Equal(t, "dash0_at_refreshed-2", deduped)
	assert.Equal(t, 2, server.exchanges(), "a superseded stale token must not rotate again")
}

// TestAuthInfoTokenProvider_StaticCredentialsAreNotRefreshed guards the
// non-OAuth paths: a static token must be served as-is, with no refresh attempt
// and no dependency on a CLI profile being present.
func TestAuthInfoTokenProvider_StaticCredentialsAreNotRefreshed(t *testing.T) {
	ctx := context.Background()
	server := newOAuthTokenServer(t)

	t.Run("environment credentials", func(t *testing.T) {
		clearCredentialEnv(t)
		t.Setenv("DASH0_API_URL", server.URL)
		t.Setenv("DASH0_AUTH_TOKEN", "auth_from_env")

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.False(t, auth.isOAuth)

		authToken, err := auth.tokenProvider().AuthToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, "auth_from_env", authToken)
		assert.Equal(t, 0, server.exchanges())
	})

	t.Run("static CLI profile", func(t *testing.T) {
		clearCredentialEnv(t)
		setupCLIConfigDir(t, "static-profile", oauthProfilesJSON(server.URL, time.Now().Add(-1*time.Hour)))

		auth, err := resolveAuthInfo(ctx, &providerConfigModel{})
		require.NoError(t, err)
		assert.False(t, auth.isOAuth)

		authToken, err := auth.tokenProvider().AuthToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, "auth_static_token", authToken)
		assert.Equal(t, 0, server.exchanges())
	})
}
