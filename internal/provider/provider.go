package provider

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0Profiles "github.com/dash0hq/dash0-api-client-go/profiles"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ provider.Provider = &dash0Provider{}
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &dash0Provider{
			version: version,
		}
	}
}

// dash0Provider is the provider implementation.
type dash0Provider struct {
	version string
}

// provider-level config model
type providerConfigModel struct {
	URL        types.String `tfsdk:"url"`
	AuthToken  types.String `tfsdk:"auth_token"`
	Profile    types.String `tfsdk:"profile"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

// Metadata returns the provider type name.
func (p *dash0Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "dash0"
	resp.Version = p.version
}

func providerSchema() schema.Schema {
	return schema.Schema{
		Description: "The Dash0 provider allows you to manage resources on the [Dash0](https://www.dash0.com) observability platform, including dashboards, check rules, recording rules, recording rule groups, synthetic checks, views, and teams. Credentials can be supplied via provider configuration attributes, via the DASH0_API_URL and DASH0_AUTH_TOKEN environment variables, or via a dash0 CLI profile.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "The base URL of the Dash0 API (e.g. \"https://api.us-west-2.aws.dash0.com\"). If omitted, the DASH0_API_URL environment variable is used. DASH0_URL is accepted as a deprecated fallback.",
			},
			"auth_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The API auth token for Dash0. Static tokens (prefixed `auth_`) can be created in [Dash0 Settings > Auth Tokens](https://app.dash0.com/settings/auth-tokens). OAuth access tokens (prefixed `dash0_at_`) are obtained via `dash0 auth login`. If omitted, the DASH0_AUTH_TOKEN environment variable is used.",
			},
			"profile": schema.StringAttribute{
				Optional:    true,
				Description: "The name of a [dash0 CLI](https://github.com/dash0hq/dash0-cli) profile to load credentials from when `url`/`auth_token` are not supplied via attributes or environment variables. If unset, the active profile in the dash0 CLI configuration directory is used. The directory defaults to `~/.dash0` and can be overridden with the DASH0_CONFIG_DIR environment variable.",
			},
			"max_retries": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of retries for failed API requests (0–5). If omitted, the DASH0_MAX_RETRIES environment variable is used. Defaults to 3.",
			},
		},
	}
}

// Schema defines the provider-level schema for configuration data.
func (p *dash0Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerSchema()
}

// getEnvURL reads the Dash0 API URL from the environment, preferring
// DASH0_API_URL and falling back to the deprecated DASH0_URL.
func getEnvURL() string {
	if v := os.Getenv("DASH0_API_URL"); v != "" {
		return v
	}
	return os.Getenv("DASH0_URL")
}

// loadProfileConfiguration resolves a dash0 CLI profile to a Configuration.
// If profileName is empty, the active profile from the CLI config directory is
// used. Profile lookup is delegated to dash0-api-client-go's profiles package,
// which honors the DASH0_CONFIG_DIR environment variable and falls back to
// `~/.dash0`.
//
// When the resolved profile uses OAuth, the access token is transparently
// refreshed (if close to expiry) for the active-profile path. Named profiles
// that are not active cannot be refreshed because the library does not expose a
// public per-name refresh API; the provider will use whatever token is on disk.
func loadProfileConfiguration(ctx context.Context, profileName string) (*dash0Profiles.Configuration, error) {
	store, err := dash0Profiles.NewStore()
	if err != nil {
		return nil, err
	}
	if profileName == "" {
		// GetActiveConfigurationContext handles OAuth token refresh internally.
		cfg, err := store.GetActiveConfigurationContext(ctx)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}
	profiles, err := store.GetProfiles()
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if p.Name == profileName {
			return &p.Configuration, nil
		}
	}
	return nil, fmt.Errorf("profile %q not found in dash0 CLI configuration", profileName)
}

// authInfo holds the resolved Dash0 URL, auth token, and whether the token
// originated from an OAuth-enabled CLI profile (in which case the auth_
// prefix validation is skipped).
type authInfo struct {
	url     string
	token   string
	isOAuth bool
}

// resolveAuthInfo computes the Dash0 URL and auth token according to the
// documented precedence:
//
//  1. DASH0_API_URL / DASH0_AUTH_TOKEN environment variables (DASH0_URL is
//     accepted as a deprecated fallback for the URL).
//  2. Provider attributes (`url`, `auth_token`).
//  3. dash0 CLI profile — the one named by the `profile` attribute, or the
//     active profile if `profile` is empty.
//
// Errors loading the CLI profile are surfaced when the user asked for a
// specific profile or when an unexpected error (e.g. malformed profiles file)
// occurs. ErrNoActiveProfile with no explicit profile is treated as "no CLI
// profile configured" and silently ignored — the caller is then expected to
// emit a "missing credentials" diagnostic.
func resolveAuthInfo(ctx context.Context, cfg *providerConfigModel) (authInfo, error) {
	var attrURL, attrAuthToken string
	if !cfg.URL.IsNull() && !cfg.URL.IsUnknown() {
		attrURL = cfg.URL.ValueString()
	}
	if !cfg.AuthToken.IsNull() && !cfg.AuthToken.IsUnknown() {
		attrAuthToken = cfg.AuthToken.ValueString()
	}

	url := cmp.Or(getEnvURL(), attrURL)
	authToken := cmp.Or(os.Getenv("DASH0_AUTH_TOKEN"), attrAuthToken)

	if url != "" && authToken != "" {
		return authInfo{url: url, token: authToken}, nil
	}

	var profileName string
	var profileExplicit bool
	if !cfg.Profile.IsNull() && !cfg.Profile.IsUnknown() {
		profileName = cfg.Profile.ValueString()
		profileExplicit = profileName != ""
	}

	profileCfg, err := loadProfileConfiguration(ctx, profileName)
	if err != nil {
		if !profileExplicit && errors.Is(err, dash0Profiles.ErrNoActiveProfile) {
			return authInfo{url: url, token: authToken}, nil
		}
		return authInfo{url: url, token: authToken}, err
	}

	isOAuth := false
	if url == "" {
		url = profileCfg.ApiUrl
	}
	if authToken == "" {
		authToken = profileCfg.AuthToken
		isOAuth = profileCfg.OAuth != nil
	}
	return authInfo{url: url, token: authToken, isOAuth: isOAuth}, nil
}

// Configure prepares a Dash0 API client for data sources and resources.
func (p *dash0Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	// Read provider config that may be set in the provider block
	var cfg providerConfigModel
	diags := req.Config.Get(ctx, &cfg)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if os.Getenv("DASH0_API_URL") == "" && os.Getenv("DASH0_URL") != "" {
		tflog.Warn(ctx, "DASH0_URL is deprecated; please switch to DASH0_API_URL")
	}

	auth, err := resolveAuthInfo(ctx, &cfg)
	if err != nil {
		if errors.Is(err, dash0Profiles.ErrReauthenticationRequired) {
			resp.Diagnostics.AddError(
				"OAuth re-authentication required",
				"The OAuth session for your dash0 CLI profile has expired. "+
					"Run `dash0 auth login` to re-authenticate, then re-run your Terraform command.",
			)
		} else {
			resp.Diagnostics.AddError(
				"Unable to load credentials from dash0 CLI profile",
				err.Error(),
			)
		}
	}

	if auth.url == "" {
		resp.Diagnostics.AddError(
			"Missing Dash0 URL",
			"The provider cannot create the Dash0 API client because no Dash0 URL was provided. "+
				"Set the `url` attribute in the provider block, set the DASH0_API_URL environment "+
				"variable, or configure a dash0 CLI profile (referenced via the `profile` attribute, "+
				"or as the active profile in `~/.dash0`).",
		)
	}
	if auth.token == "" {
		resp.Diagnostics.AddError(
			"Missing Dash0 Auth Token",
			"The provider cannot create the Dash0 API client because no Dash0 auth token was provided. "+
				"Set the `auth_token` attribute in the provider block, set the DASH0_AUTH_TOKEN "+
				"environment variable, or configure a dash0 CLI profile (referenced via the `profile` "+
				"attribute, or as the active profile in `~/.dash0`).",
		)
	}
	if auth.token != "" && !strings.HasPrefix(auth.token, "auth_") && !strings.HasPrefix(auth.token, "dash0_at_") {
		resp.Diagnostics.AddError(
			"Invalid Dash0 Auth Token",
			"The auth token must start with 'auth_' or 'dash0_at_'. Check your DASH0_AUTH_TOKEN environment variable or provider configuration.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve max retries: env var > provider attribute > default (3)
	maxRetries := 3
	maxRetriesSource := ""
	if maxRetriesStr := os.Getenv("DASH0_MAX_RETRIES"); maxRetriesStr != "" {
		parsed, err := strconv.Atoi(maxRetriesStr)
		if err != nil {
			resp.Diagnostics.AddError(
				"Invalid DASH0_MAX_RETRIES",
				"The DASH0_MAX_RETRIES environment variable must be a valid integer: "+err.Error(),
			)
			return
		}
		maxRetries = parsed
		maxRetriesSource = "DASH0_MAX_RETRIES environment variable"
	} else if !cfg.MaxRetries.IsNull() && !cfg.MaxRetries.IsUnknown() {
		maxRetries = int(cfg.MaxRetries.ValueInt64())
		maxRetriesSource = "max_retries provider attribute"
	}
	if maxRetries < 0 || maxRetries > 5 {
		detail := fmt.Sprintf("max_retries must be between 0 and 5, got: %d", maxRetries)
		if maxRetriesSource != "" {
			detail += " (from " + maxRetriesSource + ")"
		}
		resp.Diagnostics.AddError("Invalid max_retries", detail)
		return
	}

	ctx = tflog.SetField(ctx, "dash0_url", auth.url)
	ctx = tflog.SetField(ctx, "dash0_auth_token", auth.token)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "dash0_auth_token")

	tflog.Debug(ctx, "Creating Dash0 client")

	// Create dash0Client configuration for data sources and resources
	dash0Client, err := client.NewDash0Client(auth.url, auth.token, p.version, maxRetries)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Dash0 API Client",
			"An unexpected error occurred when creating the Dash0 API client: "+err.Error(),
		)
		return
	}

	resp.DataSourceData = dash0Client
	resp.ResourceData = dash0Client

	tflog.Info(ctx, "Configured Dash0 client", map[string]any{"success": true})
}

// DataSources defines the data sources implemented in the provider.
func (p *dash0Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// Resources defines the resources implemented in the provider.
func (p *dash0Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDashboardResource,
		NewSyntheticCheckResource,
		NewViewResource,
		NewCheckRuleResource,
		NewRecordingRuleResource,
		NewNotificationChannelResource,
		NewSpamFilterResource,
		NewTeamResource,
	}
}
