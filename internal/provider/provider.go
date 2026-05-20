package provider

import (
	"context"
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
	MaxRetries types.Int64  `tfsdk:"max_retries"`
}

// Metadata returns the provider type name.
func (p *dash0Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "dash0"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *dash0Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The Dash0 provider allows you to manage resources on the [Dash0](https://www.dash0.com) observability platform, including dashboards, check rules, recording rules, recording rule groups, synthetic checks, and views. Authentication can be provided via provider configuration attributes or via the DASH0_URL and DASH0_AUTH_TOKEN environment variables.`,
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "The base URL of the Dash0 API (e.g. \"https://api.us-west-2.aws.dash0.com\"). If omitted, the DASH0_URL environment variable will be used.",
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
