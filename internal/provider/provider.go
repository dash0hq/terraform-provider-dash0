package provider

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

const (
	configDirNotExistsErrMsg          string = "dash0 CLI config dir does not exists"
	emptyActiveProfileErrMsg          string = "activeProfile contains empty string"
	profileNotFoundInJsonErrMsg       string = "profile does not exists"
	noDash0CLIConfigDirProvidedErrMsg string = "no dash0 CLI config dir provided, cannot fetch profile credentials"
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
		Description: "The Dash0 provider allows you to manage resources on the [Dash0](https://www.dash0.com) observability platform, including dashboards, check rules, recording rules, recording rule groups, synthetic checks, and views. Authentication can be provided via provider configuration attributes or via the DASH0_API_URL and DASH0_AUTH_TOKEN environment variables.",
		Attributes: map[string]schema.Attribute{
			"profile": schema.StringAttribute{
				Optional: true,
				Description: "The `profile` attribute is used only when `url` or `auth_token` are not provided via environment variables or provider configuration. " +
					"When needed, the provider loads missing credentials from the dash0 CLI config files using the following logic:\n" +
					" - If `profile` is set, credentials are loaded from that named profile in the dash0 CLI config files. If the profile is not found, an error is raised.\n" +
					" - If `profile` is not set, the provider falls back to the profile specified in `~/.dash0/activeProfile`.\n" +
					" - If neither approach yields credentials, an error is raised.",
			},
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "The base URL of the Dash0 API (e.g. \"https://api.us-west-2.aws.dash0.com\"). If omitted, the DASH0_API_URL environment variable will be used. (DASH0_URL is configured as a fallback and will be deprecated, please use DASH0_API_URL instead)",
			},
			"auth_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The API auth token for Dash0. Tokens can be created in [Dash0 Settings > Auth Tokens](https://app.dash0.com/settings/auth-tokens). If omitted, the DASH0_AUTH_TOKEN environment variable will be used.",
			},
			"max_retries": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of retries for failed API requests (0–5). If omitted, the DASH0_MAX_RETRIES environment variable will be used. Defaults to 3.",
			},
		},
	}
}

// Schema defines the provider-level schema for configuration data.
func (p *dash0Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerSchema()
}

// Loads DASH0_API_URL if defined, if it is not defined then
// loads DASH0_URL
func getDash0APIUrlFromEnv() string {
	dash0ApiUrl, apiUrlEnvFound := os.LookupEnv("DASH0_API_URL")
	if apiUrlEnvFound {
		return dash0ApiUrl
	}
	dash0url, dash0UrlFound := os.LookupEnv("DASH0_URL")
	if dash0UrlFound {
		return dash0url
	}
	return ""
}

// compute dash0 CLI config dir and return its value -
// load DASH0_CONFIG_DIR if it is defined
// return path if it exists
// check if $HOME/.dash0 or $USERPROFILE/.dash0 exists or not
// if it exists return the value or return empty string
func resolveDash0ConfigDir() string {
	envDefinedConfigDir, isConfigDirEnvDefined := os.LookupEnv("DASH0_CONFIG_DIR")
	if !isConfigDirEnvDefined {
		homeDir, homeDirErr := os.UserHomeDir()
		if homeDirErr == nil {
			defaultDash0ConfigDir := filepath.Join(homeDir, ".dash0")
			_, defaultDash0ConfigDirStatErr := os.Stat(defaultDash0ConfigDir)
			if defaultDash0ConfigDirStatErr != nil {
				return ""
			}
			return defaultDash0ConfigDir
		}
		return ""
	}
	_, envDefinedConfigDirStat := os.Stat(envDefinedConfigDir)
	if envDefinedConfigDirStat != nil {
		return ""
	}
	return envDefinedConfigDir
}

// load activeProfile name from `$DASH0_CONFIG_DIR` or return a non-nil error
func loadActiveProfileFromFile(dash0ConfigDir string) (string, error) {
	activeProfileFilePath := filepath.Join(dash0ConfigDir, "activeProfile")

	_, activeProfileFileExistsErr := os.Stat(activeProfileFilePath)
	if activeProfileFileExistsErr != nil {
		// error possibly the file does not exists
		return "", activeProfileFileExistsErr
	}

	activeProfileFileContent,
		activeProfileFileContentErr := os.ReadFile(activeProfileFilePath)
	if activeProfileFileContentErr != nil {
		// error reading activeProfileFilePath
		// can be permissions error
		return "", activeProfileFileContentErr
	}
	// trimming space because many editors put a new line automatically
	// and having a new line in profile name breaks auth to dash0 APIs
	profile := strings.TrimSpace(string(activeProfileFileContent))
	return profile, nil
}

// load configuration from dash0Config or return a non-nil error
func loadUrlAndTokenFromProfiles(dash0ConfigDir string, profile string) (dash0Profiles.Configuration, error) {
	// If a config dir is specified, make sure that the path exists
	if dash0ConfigDir == "" {
		return dash0Profiles.Configuration{}, fmt.Errorf(noDash0CLIConfigDirProvidedErrMsg)
	}

	// Profile name is not provided in the provider configuration, see if there is an activeProfile
	// file defined in the dash0 CLI config directory
	if profile == "" {
		activeProfile, dash0ActiveProfileErr := loadActiveProfileFromFile(dash0ConfigDir)
		if dash0ActiveProfileErr != nil {
			return dash0Profiles.Configuration{}, dash0ActiveProfileErr
		}
		if len(activeProfile) == 0 {
			return dash0Profiles.Configuration{}, fmt.Errorf(emptyActiveProfileErrMsg)
		}
		profile = activeProfile
	}

	dash0ProfilesFilePath := filepath.Join(dash0ConfigDir, "profiles.json")
	_, dash0ProfilesFileExistsErr := os.Stat(dash0ProfilesFilePath)
	if dash0ProfilesFileExistsErr != nil {
		return dash0Profiles.Configuration{}, dash0ProfilesFileExistsErr
	}

	dash0ProfilesFileContent,
		dash0ProfilesFileContentReadErr := os.ReadFile(dash0ProfilesFilePath)
	if dash0ProfilesFileContentReadErr != nil {
		return dash0Profiles.Configuration{}, fmt.Errorf(
			"reading %s failed with exception: %s", dash0ProfilesFilePath, dash0ProfilesFileContentReadErr,
		)
	}

	var profilesConfigFile dash0Profiles.ProfilesFile
	profileJsonUnmarshalErr := json.Unmarshal(dash0ProfilesFileContent, &profilesConfigFile)
	if profileJsonUnmarshalErr != nil {
		return dash0Profiles.Configuration{}, fmt.Errorf(
			"parsing %s failed with exception: %s", dash0ProfilesFilePath, profileJsonUnmarshalErr,
		)
	}

	for _, profileData := range profilesConfigFile.Profiles {
		if profileData.Name == profile {
			return profileData.Configuration, nil
		}
	}

	return dash0Profiles.Configuration{}, fmt.Errorf(
		"%s, using: %s, looking for profile: %s ", profileNotFoundInJsonErrMsg, dash0ProfilesFilePath, profile,
	)
}

func resolveAuthInfo(cfg *providerConfigModel) (string, string, error) {
	// Start with environment variables
	urlEnv := getDash0APIUrlFromEnv()
	dash0ConfigDir := resolveDash0ConfigDir()
	authTokenEnv := os.Getenv("DASH0_AUTH_TOKEN")

	if urlEnv != "" && authTokenEnv != "" {
		return urlEnv, authTokenEnv, nil
	}

	var urlProvider, authTokenProvider string
	if !cfg.URL.IsNull() && !cfg.URL.IsUnknown() {
		urlProvider = cfg.URL.ValueString()
	}

	if !cfg.AuthToken.IsNull() && !cfg.AuthToken.IsUnknown() {
		authTokenProvider = cfg.AuthToken.ValueString()
	}

	url := cmp.Or(urlEnv, urlProvider)
	authToken := cmp.Or(authTokenEnv, authTokenProvider)

	if url != "" && authToken != "" {
		return url, authToken, nil
	}

	var profileToLoad string
	if !cfg.Profile.IsNull() && !cfg.Profile.IsUnknown() {
		profileToLoad = cfg.Profile.ValueString()
	}

	// no dash0CLIConfigDir exists, cannot load values from config dir,
	// return an error if profile was provided, if not then silently skip
	// the process of getting values from config dir
	if dash0ConfigDir == "" {
		if profileToLoad == "" {
			return url, authToken, nil
		}
		if profileToLoad != "" {
			return url, authToken, fmt.Errorf(configDirNotExistsErrMsg)
		}
	}
	configModel, configModelErr := loadUrlAndTokenFromProfiles(dash0ConfigDir, profileToLoad)

	var urlConfig, authTokenConfig string
	if len(strings.TrimSpace(configModel.ApiUrl)) > 0 {
		urlConfig = strings.TrimSpace(configModel.ApiUrl)
	}
	if len(strings.TrimSpace(configModel.AuthToken)) > 0 {
		authTokenConfig = strings.TrimSpace(configModel.AuthToken)
	}

	url = cmp.Or(urlEnv, urlProvider, urlConfig)
	authToken = cmp.Or(authTokenEnv, authTokenProvider, authTokenConfig)

	// if one of either url or authToken is still empty then return
	// the exception as well, that will be provided to user
	if url == "" || authToken == "" {
		return url, authToken, configModelErr
	}

	return url, authToken, nil
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

	url, authToken, err := resolveAuthInfo(&cfg)

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to load credentials from dash0 CLI config dir",
			err.Error(),
		)
	}

	// Validate
	if url == "" {
		resp.Diagnostics.AddError(
			"Missing Dash0 URL",
			"The provider cannot create the Dash0 API client, because no Dash0 URL was"+
				" provided. Set the `url` attribute in the provider block or set the"+
				" DASH0_API_URL environment variable. You can even"+
				" use a dash0 CLI profile and provide the profile name as `profile`"+
				" attribute to the provider. If `profile` is not defined, the"+
				" current `activeProfile` of dash0 CLI will be used."+
				" DASH0_URL is the legacy way of defining DASH0_API_URL will be deprecated"+
				" in future releases.",
		)
	}

	if authToken == "" {
		resp.Diagnostics.AddError(
			"Missing Dash0 Auth Token",
			"The provider cannot create the Dash0 API client because no"+
				"Dash0 Auth Token was provided. Set the `auth_token` attribute in the"+
				"provider block or set the DASH0_AUTH_TOKEN environment variable. You can"+
				"even configure a dash0 CLI profile and provide the profile name as"+
				"`profile` attribute to the provider. If `profile` is not defined, the"+
				"current `activeProfile` of dash0 CLI will be used.",
		)
	}

	if !strings.HasPrefix(authToken, "auth_") && authToken != "" {
		resp.Diagnostics.AddError(
			"Invalid Dash0 Auth Token",
			"The auth token must start with 'auth_'. Check your DASH0_AUTH_TOKEN environment variable or provider configuration.",
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

	ctx = tflog.SetField(ctx, "dash0_url", url)
	ctx = tflog.SetField(ctx, "dash0_auth_token", authToken)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "dash0_auth_token")

	tflog.Debug(ctx, "Creating Dash0 client")

	// Create dash0Client configuration for data sources and resources
	dash0Client, err := client.NewDash0Client(url, authToken, p.version, maxRetries)
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
	}
}
