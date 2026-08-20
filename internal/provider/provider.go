package provider

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	dash0Profiles "github.com/dash0hq/dash0-api-client-go/profiles"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ provider.Provider            = &dash0Provider{}
	_ provider.ProviderWithActions = &dash0Provider{}
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
	OtlpURL    types.String `tfsdk:"otlp_url"`
	Profile    types.String `tfsdk:"profile"`
	Dataset    types.String `tfsdk:"dataset"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

// resourceProviderData is what Configure stores as resp.ResourceData. It
// bundles the API client with the provider-level default dataset so
// dataset-scoped resources can inherit it when their own `dataset` attribute
// is omitted, without every resource re-deriving the default itself.
type resourceProviderData struct {
	client         client.Client
	defaultDataset string
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
				Description: "The base URL of the Dash0 API (e.g. \"https://api.us-west-2.aws.dash0.com\"). Find yours on the [API endpoint settings page](https://app.dash0.com/goto/settings/endpoints?endpoint_type=api_http). If omitted, the DASH0_API_URL environment variable is used. DASH0_URL is accepted as a deprecated fallback.",
			},
			"auth_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The API auth token for Dash0. Static tokens (prefixed `auth_`) can be created in [Dash0 Settings > Auth Tokens](https://app.dash0.com/goto/settings/auth-tokens). OAuth access tokens (prefixed `dash0_at_`) are obtained via `dash0 auth login`. If omitted, the DASH0_AUTH_TOKEN environment variable is used.",
			},
			"otlp_url": schema.StringAttribute{
				Optional:    true,
				Description: "The base URL of the Dash0 OTLP/HTTP ingress endpoint (e.g. \"https://ingress.us-west-2.aws.dash0.com\"). Find yours on the [OTLP endpoint settings page](https://app.dash0.com/goto/settings/endpoints?endpoint_type=otlp_http). This is a different host from `url`, which addresses the Dash0 API. It is only required by the `dash0_log_event` and `dash0_deployment_event` actions; all resources work without it. If omitted, the DASH0_OTLP_URL environment variable is used, followed by the OTLP URL of the dash0 CLI profile. Signal-specific paths such as `/v1/logs` are appended automatically and must not be included.",
			},
			"profile": schema.StringAttribute{
				Optional:    true,
				Description: "The name of a [dash0 CLI](https://github.com/dash0hq/dash0-cli) profile to load credentials from when `url`/`auth_token`/`otlp_url` are not supplied via attributes or environment variables. If unset, the active profile in the dash0 CLI configuration directory is used. The directory defaults to `~/.dash0` and can be overridden with the DASH0_CONFIG_DIR environment variable.",
			},
			"dataset": schema.StringAttribute{
				Optional:    true,
				Description: "The default [Dash0 dataset](https://dash0.com/docs/dash0/miscellaneous/glossary/datasets) used by dataset-scoped resources (dashboards, check rules, recording rules, spam filters, synthetic checks, and views) that omit their own `dataset` attribute. If omitted, the DASH0_DATASET environment variable is used, then the dataset configured on the resolved dash0 CLI profile, then \"default\". A resource can still override this per resource via its own `dataset` attribute.",
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
// Both paths record which profile the configuration came from, which is what
// lets the client refresh its OAuth access token per request. A hand-rolled
// search through GetProfiles drops that, leaving a configuration that can only
// serve whatever token happens to sit on disk.
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
	cfg, err := store.GetConfigurationForProfile(ctx, profileName)
	if errors.Is(err, dash0Profiles.ErrProfileNotFound) {
		return nil, fmt.Errorf("profile %q not found in dash0 CLI configuration", profileName)
	}
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// authInfo holds the resolved Dash0 URL, auth token, OTLP endpoint, and
// whether the token originated from an OAuth-enabled CLI profile (in which
// case the auth_ prefix validation is skipped).
//
// profileCfg is the CLI profile the credentials came from, or nil when they came
// from the environment or the provider block. It is retained so the client can
// be built around a refreshing token provider instead of the token snapshot in
// the token field, which is only used for validation and logging.
type authInfo struct {
	url        string
	token      string
	otlpURL    string
	isOAuth    bool
	profileCfg *dash0Profiles.Configuration
}

// tokenProvider returns how the API client should authenticate.
//
// An OAuth profile yields a provider that refreshes the access token as it nears
// expiry, so a long plan or apply is not cut off mid-run. Everything else is a
// credential that does not expire, so it is served as-is.
func (a authInfo) tokenProvider() dash0.AuthTokenProvider {
	if a.isOAuth && a.profileCfg != nil {
		return a.profileCfg.AuthTokenProvider()
	}
	return dash0.StaticAuthTokenProvider(a.token)
}

// resolveAuthInfo computes the Dash0 URL, auth token, and OTLP endpoint
// according to the documented precedence:
//
//  1. DASH0_API_URL / DASH0_AUTH_TOKEN / DASH0_OTLP_URL environment variables
//     (DASH0_URL is accepted as a deprecated fallback for the API URL).
//  2. Provider attributes (`url`, `auth_token`, `otlp_url`).
//  3. dash0 CLI profile — the one named by the `profile` attribute, or the
//     active profile if `profile` is empty.
//
// Errors loading the CLI profile are surfaced when the user asked for a
// specific profile or when an unexpected error (e.g. malformed profiles file)
// occurs. ErrNoActiveProfile with no explicit profile is treated as "no CLI
// profile configured" and silently ignored — the caller is then expected to
// emit a "missing credentials" diagnostic.
//
// The OTLP endpoint is deliberately not part of the "credentials are complete"
// test: it is only needed by the log-event actions, so a configuration that
// supplies the API URL and auth token but no OTLP URL must keep working exactly
// as before. See the comment on the profile lookup below.
func resolveAuthInfo(ctx context.Context, cfg *providerConfigModel) (authInfo, error) {
	var attrURL, attrAuthToken, attrOtlpURL string
	if !cfg.URL.IsNull() && !cfg.URL.IsUnknown() {
		attrURL = cfg.URL.ValueString()
	}
	if !cfg.AuthToken.IsNull() && !cfg.AuthToken.IsUnknown() {
		attrAuthToken = cfg.AuthToken.ValueString()
	}
	if !cfg.OtlpURL.IsNull() && !cfg.OtlpURL.IsUnknown() {
		attrOtlpURL = cfg.OtlpURL.ValueString()
	}

	url := cmp.Or(getEnvURL(), attrURL)
	authToken := cmp.Or(os.Getenv("DASH0_AUTH_TOKEN"), attrAuthToken)
	otlpURL := cmp.Or(os.Getenv("DASH0_OTLP_URL"), attrOtlpURL)

	if url != "" && authToken != "" && otlpURL != "" {
		return authInfo{url: url, token: authToken, otlpURL: otlpURL, isOAuth: isOAuthAccessToken(authToken)}, nil
	}

	// Whether the credentials were already complete before consulting the CLI
	// profile decides how a profile-load failure is treated. When they were, the
	// only thing the profile could still contribute is the OTLP URL, which most
	// configurations do not need — so a broken or absent profiles file must not
	// turn a previously working env-var-only setup into a hard error.
	credentialsComplete := url != "" && authToken != ""

	var profileName string
	var profileExplicit bool
	if !cfg.Profile.IsNull() && !cfg.Profile.IsUnknown() {
		profileName = cfg.Profile.ValueString()
		profileExplicit = profileName != ""
	}

	profileCfg, err := loadProfileConfiguration(ctx, profileName)
	if err != nil {
		// Whenever url+token were already complete, the profile was only ever
		// being consulted opportunistically for the OTLP URL — a broken or
		// explicitly-wrong `profile` must not turn a previously working
		// env-var/attribute-only setup into a hard error. Only when
		// credentials are still incomplete does profileExplicit matter: an
		// explicit-but-bad profile name is then a real error, but an absent
		// active profile (with no explicit profile requested) is not.
		if credentialsComplete || (!profileExplicit && errors.Is(err, dash0Profiles.ErrNoActiveProfile)) {
			if credentialsComplete && !errors.Is(err, dash0Profiles.ErrNoActiveProfile) {
				tflog.Debug(ctx, "Ignoring dash0 CLI profile error; credentials already resolved", map[string]any{
					"error": err.Error(),
				})
			}
			return authInfo{url: url, token: authToken, otlpURL: otlpURL, isOAuth: isOAuthAccessToken(authToken)}, nil
		}
		return authInfo{url: url, token: authToken, otlpURL: otlpURL, isOAuth: isOAuthAccessToken(authToken)}, err
	}

	// urlFromProfile must be captured before url is possibly overwritten below:
	// the profile's OTLP URL is only adopted when the API URL also came from
	// that same profile, otherwise env/attribute credentials for one region
	// could be paired with a different profile's OTLP endpoint for another.
	urlFromProfile := url == ""
	if url == "" {
		url = profileCfg.ApiUrl
	}
	if authToken == "" {
		authToken = profileCfg.AuthToken
	}
	if otlpURL == "" && urlFromProfile {
		otlpURL = profileCfg.OtlpUrl
	}
	// isOAuth is derived from the final resolved token's own prefix rather
	// than from profileCfg.OAuth, so it stays correct regardless of whether
	// the token came from env vars, a provider attribute, or the profile:
	// a `dash0_at_`-prefixed token pasted directly into DASH0_AUTH_TOKEN or
	// `auth_token` is just as much an OAuth access token as one resolved via
	// a CLI profile. profileCfg is still retained so tokenProvider() can
	// refresh it when it came from an OAuth-enabled profile.
	return authInfo{url: url, token: authToken, otlpURL: otlpURL, isOAuth: isOAuthAccessToken(authToken), profileCfg: profileCfg}, nil
}

// resolveDataset computes the provider-level default dataset that
// dataset-scoped resources inherit when their own `dataset` attribute is
// omitted. Dataset is not an auth concern, so this is a sibling of
// resolveAuthInfo rather than folded into it, and it follows the same
// precedence pattern:
//
//  1. DASH0_DATASET environment variable.
//  2. The `dataset` provider attribute.
//  3. The dash0 CLI profile named by `profile` (or the active profile).
//  4. "default".
//
// profileCfg is the configuration resolveAuthInfo already loaded, when
// available, so the profile file is not read twice in the common case. It is
// nil when resolveAuthInfo never consulted a profile (credentials were
// already complete from env vars/attributes); a profile is then loaded here
// as a best-effort second attempt. Unlike resolveAuthInfo, a profile load
// failure here is silently ignored: dataset resolution never fails provider
// configuration, it just falls through to "default".
func resolveDataset(ctx context.Context, cfg *providerConfigModel, profileCfg *dash0Profiles.Configuration) string {
	var attrDataset string
	if !cfg.Dataset.IsNull() && !cfg.Dataset.IsUnknown() {
		attrDataset = cfg.Dataset.ValueString()
	}
	if dataset := cmp.Or(os.Getenv("DASH0_DATASET"), attrDataset); dataset != "" {
		return dataset
	}

	if profileCfg == nil {
		var profileName string
		if !cfg.Profile.IsNull() && !cfg.Profile.IsUnknown() {
			profileName = cfg.Profile.ValueString()
		}
		profileCfg, _ = loadProfileConfiguration(ctx, profileName)
	}
	if profileCfg != nil && profileCfg.Dataset != "" {
		return profileCfg.Dataset
	}

	return "default"
}

// isOAuthAccessToken reports whether token is an OAuth access token
// (`dash0_at_` prefix) rather than a static token (`auth_` prefix). This is
// the sole place that inspects the token's prefix; the Dash0 client trusts
// authInfo.isOAuth instead of re-deriving it.
func isOAuthAccessToken(token string) bool {
	return strings.HasPrefix(token, "dash0_at_")
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

	defaultDataset := resolveDataset(ctx, &cfg, auth.profileCfg)

	ctx = tflog.SetField(ctx, "dash0_url", auth.url)
	ctx = tflog.SetField(ctx, "dash0_auth_token", auth.token)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "dash0_auth_token")

	tflog.Debug(ctx, "Creating Dash0 client")

	dash0Client, err := client.NewDash0Client(auth.url, auth.tokenProvider(), auth.isOAuth, p.version, maxRetries, auth.otlpURL)
	if err != nil && auth.otlpURL != "" {
		// A malformed otlp_url only needs to break the dash0_log_event and
		// dash0_deployment_event actions, not every resource and data source.
		// Retry without it and downgrade to a warning; if construction still
		// fails, the error is unrelated to OTLP and must surface below as before.
		resp.Diagnostics.AddWarning(
			"Invalid Dash0 OTLP Endpoint",
			fmt.Sprintf(
				"The configured OTLP endpoint is invalid and will be ignored: %s\n\n"+
					"Resources and data sources are unaffected, but the dash0_log_event and "+
					"dash0_deployment_event actions will fail if invoked.",
				err,
			),
		)
		dash0Client, err = client.NewDash0Client(auth.url, auth.tokenProvider(), auth.isOAuth, p.version, maxRetries, "")
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Dash0 API Client",
			"An unexpected error occurred when creating the Dash0 API client: "+err.Error(),
		)
		return
	}

	resp.DataSourceData = dash0Client
	resp.ResourceData = resourceProviderData{client: dash0Client, defaultDataset: defaultDataset}
	resp.ActionData = dash0Client

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

// Actions defines the actions implemented in the provider. Actions are
// point-in-time operations that do not manage state; they require Terraform
// 1.14 or later.
func (p *dash0Provider) Actions(_ context.Context) []func() action.Action {
	return []func() action.Action{
		NewLogEventAction,
		NewDeploymentEventAction,
	}
}
