package provider

import (
	"context"
	"os"

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
	URL       types.String `tfsdk:"url"`
	AuthToken types.String `tfsdk:"auth_token"`
}

// Metadata returns the provider type name.
func (p *dash0Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "dash0"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *dash0Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Interact with Dash0 observability platform. Authentication can be provided via provider configuration attributes `url` and `auth_token` or via environment variables DASH0_URL and DASH0_AUTH_TOKEN.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional:    true,
				Description: "Dash0 base URL. If omitted, the DASH0_URL environment variable will be used.",
			},
			"auth_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Dash0 auth token. If omitted, the DASH0_AUTH_TOKEN environment variable will be used.",
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

	// Start with environment variables as fallback
	url := os.Getenv("DASH0_URL")
	authToken := os.Getenv("DASH0_AUTH_TOKEN")

	// If provider attributes are set, they override environment variables
	if !cfg.URL.IsNull() && !cfg.URL.IsUnknown() {
		url = cfg.URL.ValueString()
	}
	if !cfg.AuthToken.IsNull() && !cfg.AuthToken.IsUnknown() {
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

	if resp.Diagnostics.HasError() {
		return
	}

	ctx = tflog.SetField(ctx, "dash0_url", url)
	ctx = tflog.SetField(ctx, "dash0_auth_token", authToken)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "dash0_auth_token")

	tflog.Debug(ctx, "Creating Dash0 client")

	// Create dash0Client configuration for data sources and resources
	dash0Client := client.NewDash0Client(url, authToken)

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
	}
}
