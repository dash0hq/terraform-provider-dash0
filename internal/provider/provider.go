package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	URL       types.String `tfsdk:"url"`
	AuthToken types.String `tfsdk:"auth_token"`
	Profile   types.String `tfsdk:"profile"`
}

// Metadata returns the provider type name.
func (p *dash0Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "dash0"
	resp.Version = p.version
}

func providerSchema() schema.Schema {
	return schema.Schema{
		Description: `The Dash0 provider allows you to manage resources on the [Dash0](https://www.dash0.com) observability platform, including dashboards, check rules, recording rules, recording rule groups, synthetic checks, and views. Authentication can be provided via provider configuration attributes or via the DASH0_URL and DASH0_AUTH_TOKEN environment variables.`,
		Attributes: map[string]schema.Attribute{
			"profile": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "If the values of both url & auth_token are found either on the env variables or in the provider configuration value of [profile] has no effect on working of the provider." +
					" The value of [profile] variable only comes into action when either url or auth_token (i.e. [user/auth_token]) are not found." +
					" In such the case the provider client is created with the following logic -\n" +
					" - if a [profile] is specified and the [url/auth_token] are not the provider will try to read the values of [url/auth_token] from the specified profile in the dash0-cli config files. \n" +
					" - If a [profile] is specified and provider is unable to find definition of such in the dash0 cli config files, an exception will be thrown." +
					" - If none of the [profile/url/auth_token] are specified then provider considers the profile mentioned in ~/.dash0/activeProfile as the profile and loads values of [url/auth_token] from it. \n" +
					" - If none of the [profile/url/auth_token] are specified and provider also is unable to find the credentials from an activeProfile of dash0 CLI config files, an exception will be thrown",
			},
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "The base URL of the Dash0 API (e.g. \"https://api.us-west-2.aws.dash0.com\"). If omitted, the DASH0_URL environment variable will be used.",
			},
			"auth_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The API auth token for Dash0. Tokens can be created in [Dash0 Settings > Auth Tokens](https://app.dash0.com/settings/auth-tokens). If omitted, the DASH0_AUTH_TOKEN environment variable will be used.",
			},
		},
	}
}

// Schema defines the provider-level schema for configuration data.
func (p *dash0Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerSchema()
}

func unableToCreateDash0ApiClientErrorMessage(err error) string {
	return fmt.Sprintf("The provider cannot create the Dash0 API client because no Dash0 URL or"+
		" Dash0 Auth Token was provided and Dash0 CLI Config was also not found."+
		" You can set up the url and auth_token values in provider configuration"+
		" or configure a Dash0 CLI with an authenticated profile ref:"+
		" https://github.com/dash0hq/dash0-cli#configuration-storage"+
		// This error will include the filepath which our provider was unable
		// to load, enabling faster debugging for developers
		" Error: %s", err.Error())
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

	// Start with environment variables
	url := os.Getenv("DASH0_URL")
	authToken := os.Getenv("DASH0_AUTH_TOKEN")

	// only if environment variables are not set, use config values
	if url == "" && !cfg.URL.IsNull() && !cfg.URL.IsUnknown() {
		url = cfg.URL.ValueString()
	}
	if authToken == "" && !cfg.AuthToken.IsNull() && !cfg.AuthToken.IsUnknown() {
		authToken = cfg.AuthToken.ValueString()
	}

	profile := ""
	// try to load profile name from provider configuration
	if !cfg.Profile.IsNull() && !cfg.Profile.IsUnknown() {
		profile = cfg.Profile.ValueString()
	}

	// Check if url or authToken are still missing a value
	if url == "" || authToken == "" {
		// Inorder to load dash0Config dir we need user home dir
		// load that using go std libs
		homeDir, homeDirErr := os.UserHomeDir()
		if homeDirErr != nil {
			// Unable to locate home direct, we will not be able to build an api client
			resp.Diagnostics.AddError(
				"Unable to authenticate to Dash0 APIs",
				unableToCreateDash0ApiClientErrorMessage(homeDirErr),
			)
		} else {
			// Try to load values from dash0 CLI config files
			if profile == "" {
				dash0ConfigDirPath := fmt.Sprintf("%s/.dash0", homeDir)
				dash0ActiveProfileFilePath := fmt.Sprintf(
					"%s/.dash0/activeProfile",
					dash0ConfigDirPath,
				)
				_, dash0ActiveProfileFilePathExistsErr := os.Stat(dash0ActiveProfileFilePath)
				if dash0ActiveProfileFilePathExistsErr != nil {
					// error stating activeProfileFilePath
					resp.Diagnostics.AddError(
						"Unable to authenticate to Dash0 APIs",
						unableToCreateDash0ApiClientErrorMessage(
							dash0ActiveProfileFilePathExistsErr,
						),
					)
				} else {
					activeProfileFileContent,
						activeProfileFileContentErr := os.ReadFile(dash0ActiveProfileFilePath)
					if activeProfileFileContentErr != nil {
						// error reading activeProfileFilePath
						resp.Diagnostics.AddError(
							"Unable to authenticate to Dash0 APIs",
							unableToCreateDash0ApiClientErrorMessage(dash0ActiveProfileFilePathExistsErr),
						)
					}
					profile = string(activeProfileFileContent)
				}
			}

			// We consider that the profile variable now consists of the profile name
			// it can be that the activeProfileFile is still empty, but not considering
			// that situation

			dash0ProfilesFilePath := fmt.Sprintf("%s/.dash0/profiles.json", homeDir)
			_, dash0ProfilesFileExistsErr := os.Stat(dash0ProfilesFilePath)
			if dash0ProfilesFileExistsErr != nil {
				resp.Diagnostics.AddError(
					"Unable to authenticate to Dash0 APIs",
					unableToCreateDash0ApiClientErrorMessage(dash0ProfilesFileExistsErr),
				)
			} else {
				dash0ProfilesFileContent,
					dash0ProfilesFileContentReadErr := os.ReadFile(dash0ProfilesFilePath)
				if dash0ProfilesFileContentReadErr != nil {
					resp.Diagnostics.AddError(
						"Unable to authenticate to Dash0 APIs",
						unableToCreateDash0ApiClientErrorMessage(dash0ProfilesFileExistsErr),
					)
				}
				var profiles []dash0Profiles.Profile
				profileJsonUnmarshalErr := json.Unmarshal(dash0ProfilesFileContent, &profiles)
				if profileJsonUnmarshalErr != nil {
					resp.Diagnostics.AddError(
						"Unable to authenticate to Dash0 APIs",
						unableToCreateDash0ApiClientErrorMessage(profileJsonUnmarshalErr),
					)
				} else {
					profileFound := false
					for _, profileData := range profiles {
						if profileData.Name == profile {
							url = profileData.Configuration.ApiUrl
							authToken = profileData.Configuration.AuthToken
							profileFound = true
							break
						}
					}
					if !profileFound {
						profileNotFoundError := fmt.Errorf(
							"Unable to find %s profile in %s",
							profile,
							dash0ProfilesFilePath,
						)
						resp.Diagnostics.AddError(
							"Unable to authenticate to Dash0 APIs",
							unableToCreateDash0ApiClientErrorMessage(profileNotFoundError),
						)
					}
				}
			}
		}
	}

	// Validate
	if url == "" {
		resp.Diagnostics.AddError(
			"Missing Dash0 URL",
			"The provider cannot create the Dash0 API client because no Dash0 URL was provided. "+
				"Set the `url` attribute in the provider block or set the DASH0_URL environment variable.",
		)
	}

	if authToken == "" {
		resp.Diagnostics.AddError(
			"Missing Dash0 Auth Token",
			"The provider cannot create the Dash0 API client because no Dash0 auth token was provided. "+
				"Set the `auth_token` attribute in the provider block or set the DASH0_AUTH_TOKEN environment variable.",
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

	ctx = tflog.SetField(ctx, "dash0_url", url)
	ctx = tflog.SetField(ctx, "dash0_auth_token", authToken)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "dash0_auth_token")

	tflog.Debug(ctx, "Creating Dash0 client")

	// Create dash0Client configuration for data sources and resources
	dash0Client, err := client.NewDash0Client(url, authToken, p.version)
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
	}
}
