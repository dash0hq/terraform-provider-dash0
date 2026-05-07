package provider

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Only creates a temporary dash0 Config Directory for tests
// This directory is cleaned after automatically and you can specify what file
// from provider_test_rs directory do you want to be copied into the temporary
// dash0 config directory to do the tests, it requires the names of
func createTemporaryDash0CliConfig(t *testing.T, sourceActiveProfileFileName string, sourceProfilesJsonFileName string) string {
	tempConfigDir := t.TempDir()

	tempDash0ConfigDirPath := path.Join(tempConfigDir, ".dash0")

	tempConfigDirCreationErr := os.MkdirAll(tempDash0ConfigDirPath, 0777)
	if tempConfigDirCreationErr != nil {
		t.Error("Unable to create temporary config dir")
	}

	copyFiles := func(sourceFile string, targetFile string) {
		if sourceFile == "" {
			// if source file name is empty then do not do any operations
			return
		}
		targetFilePath := path.Join(tempDash0ConfigDirPath, targetFile)
		sourceFilePath := path.Join("provider_test_res", sourceFile)

		if sourceFileContent, sourceFileContentErr := os.ReadFile(sourceFilePath); sourceFileContentErr != nil {
			t.Errorf("Unable to read: %s to create: %s in temporary dash0 config dir", sourceFilePath, targetFile)
		} else {
			if targetProfileWriteErr := os.WriteFile(targetFilePath, sourceFileContent, 0777); targetProfileWriteErr != nil {
				t.Errorf("Error creating %s, Exception: %s", targetFile, targetProfileWriteErr.Error())
			}
		}
	}

	copyFiles(sourceActiveProfileFileName, "activeProfile")
	copyFiles(sourceProfilesJsonFileName, "profiles.json")

	return tempDash0ConfigDirPath
}

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

	// Verify schema attributes
	assert.Contains(t, resp.Schema.Attributes, "url")
	assert.Contains(t, resp.Schema.Attributes, "auth_token")

	// Check specific attribute properties
	urlAttr := resp.Schema.Attributes["url"].(schema.StringAttribute)
	assert.True(t, urlAttr.Optional)
	assert.Contains(t, urlAttr.Description, "base URL")

	authTokenAttr := resp.Schema.Attributes["auth_token"].(schema.StringAttribute)
	assert.True(t, authTokenAttr.Optional)
	assert.True(t, authTokenAttr.Sensitive)
	assert.Contains(t, authTokenAttr.Description, "auth token")

	profileAttr := resp.Schema.Attributes["profile"].(schema.StringAttribute)
	assert.True(t, profileAttr.Optional)
	assert.Contains(t, profileAttr.Description, "profile")
}

// This tests configuration of provider using only env variables
func TestDash0Provider_Configure_WithEnvironmentVariables(t *testing.T) {
	// Set environment variables
	t.Setenv("DASH0_URL", "https://api.example.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_test_token_123")

	p := &dash0Provider{}

	// Create empty config (no provider attributes set)
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of provider using only provider attributes
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
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.provider.com"),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_provider_token_456"),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of provider using env variables as well as
// profile attributes.
func TestDash0Provider_Configure_EnvironmentVariablesPrecedence(t *testing.T) {
	// Set environment variables - these should take precedence
	t.Setenv("DASH0_URL", "https://api.env.com")
	t.Setenv("DASH0_AUTH_TOKEN", "auth_env_token_789")

	p := &dash0Provider{}

	// Create config with different provider attributes
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.provider.com"),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_provider_token_456"),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of a provider with missing URL field
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
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of a provider with missing AuthToken
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
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.example.com"),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of a provider with missing both fields
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
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 2)
}

// This tests configuration of a provider with missing URL but now dash0 CLI
// profiles are present with a custom Dash0_CONFIG_DIR specified
func TestDash0Provider_Configure_MissingURL_With_Profiles(t *testing.T) {
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "profiles.json")

	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	// Setup a custom Dash0 Config Dir Path
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}
	// Create config with only auth_token
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of a provider with missing Auth Token but now dash0
// CLI profiles are present with a custom Dash0_CONFIG_DIR specified
func TestDash0Provider_Configure_MissingAuthToken_With_Profiles(t *testing.T) {
	// setup Temporary Config Dir
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "profiles.json")

	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	// Setup a custom Dash0 Config Dir Path
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}

	// Create config with only url
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, "https://api.example.com"),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// This tests configuration of a provider with missing Both URL and Token but
// now dash0 CLI profiles are present with a custom Dash0_CONFIG_DIR specified
func TestDash0Provider_Configure_MissingBoth_With_Profiles(t *testing.T) {
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "profiles.json")

	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	// Setup a custom Dash0 Config Dir Path
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}

	// Create empty config
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, nil),
			"profile":    tftypes.NewValue(tftypes.String, nil),
		}),
		Schema: providerSchema(),
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

// Using an profile name which is not the activeProfileName in the provider config
// our provider should still load the url parameter from the dummy config files
func TestDash0Provider_Configure_MissingURL_With_Profiles_ExistingProfileName(t *testing.T) {
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "profiles.json")

	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	// Setup a custom Dash0 Config Dir Path
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}
	// Create config with only auth_token
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			// This profile is not the active profile
			"profile": tftypes.NewValue(tftypes.String, "test1"),
		}),
		Schema: providerSchema(),
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

// Using an incorrect profile name in the provider config our provider should
// throw an exception with `Missing Dash0 URL`
func TestDash0Provider_Configure_MissingURL_With_Profiles_NonExistantProfileName(t *testing.T) {
	// create a temporary config directory
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "profiles.json")

	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	// use the temporary config dir as Config dir
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}
	// Create config with only auth_token
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			// This profile does not exists in dummy files
			"profile": tftypes.NewValue(tftypes.String, "unknown"),
		}),
		Schema: providerSchema(),
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
}

// This tests the case wherein a profiles.json is missing from the dash0 CLI config directory
// in the provider config our provider should throw an exception with `Missing Dash0 URL`
func TestDash0Provider_Configure_MissingURL_With_Profiles_NonExistantProfilesJson(t *testing.T) {
	// create a temporary config directory with no profiles.json file
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "")

	// Ensure no environment variables are set
	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")
	// use the temporary config dir as Config dir
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}
	// Create config with only auth_token
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			// This profile does not exists in dummy files
			"profile": tftypes.NewValue(tftypes.String, "unknown"),
		}),
		Schema: providerSchema(),
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
}

// This tests configuration of a provider with missing URL but now dash0 CLI
// profiles.json is present with an invalid schema, i.e. json.UnMarshal would fail
func TestDash0Provider_Configure_MissingURL_With_Profile_IncorrectProfileSchema(t *testing.T) {
	// Ensure no environment variables are set
	// create a temporary config directory with no profiles.json file
	tempDirPath := createTemporaryDash0CliConfig(t, "activeProfile", "profilesIncorrectSchema.json")

	t.Setenv("DASH0_URL", "")
	t.Setenv("DASH0_AUTH_TOKEN", "")

	// Set DASH0_CONFIG_DIR
	t.Setenv("DASH0_CONFIG_DIR", tempDirPath)

	p := &dash0Provider{}
	// Create config with only auth_token
	config := tfsdk.Config{
		Raw: tftypes.NewValue(tftypes.Object{
			AttributeTypes: map[string]tftypes.Type{
				"url":        tftypes.String,
				"auth_token": tftypes.String,
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			"profile":    tftypes.NewValue(tftypes.String, "test1"),
		}),
		Schema: providerSchema(),
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 1)
	t.Log(resp.Diagnostics.Errors())
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "url")
}

// This tests configuration of a provider with missing URL but now dash0 CLI
// profiles are not present but the `profile` attribute was described in provider
func TestDash0Provider_Configure_MissingURL_Without_Profile_ProfileNamePresent(t *testing.T) {
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
				"profile":    tftypes.String,
			},
		}, map[string]tftypes.Value{
			"url":        tftypes.NewValue(tftypes.String, nil),
			"auth_token": tftypes.NewValue(tftypes.String, "auth_token_only"),
			"profile":    tftypes.NewValue(tftypes.String, "test1"),
		}),
		Schema: providerSchema(),
	}

	req := provider.ConfigureRequest{
		Config: config,
	}
	resp := &provider.ConfigureResponse{}

	p.Configure(context.Background(), req, resp)

	assert.True(t, resp.Diagnostics.HasError())
	assert.Len(t, resp.Diagnostics.Errors(), 1)
	assert.Contains(t, resp.Diagnostics.Errors()[0].Summary(), "Missing Dash0 URL")
	assert.Contains(t, resp.Diagnostics.Errors()[0].Detail(), "url")
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
	assert.Len(t, resources, 7) // DashboardResource, SyntheticCheckResource, ViewResource, CheckRuleResource, RecordingRuleResource, NotificationChannelResource, SpamFilterResource
}
