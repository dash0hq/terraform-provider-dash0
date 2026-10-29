package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"gopkg.in/yaml.v3"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	customplanmodifier "github.com/dash0hq/terraform-provider-dash0/internal/provider/planmodifier"
)

var (
	_ resource.Resource                   = &AwsMetricScrapeConfigResource{}
	_ resource.ResourceWithConfigure      = &AwsMetricScrapeConfigResource{}
	_ resource.ResourceWithImportState    = &AwsMetricScrapeConfigResource{}
	_ resource.ResourceWithValidateConfig = &AwsMetricScrapeConfigResource{}
)

// awsMetricScrapeConfigIgnoredFields are always ignored in drift detection:
// the server ignores metadata.name on write and returns the integration's
// display name instead.
var awsMetricScrapeConfigIgnoredFields = []string{"metadata.name"}

// NewAwsMetricScrapeConfigResource is a helper function to simplify the provider implementation.
func NewAwsMetricScrapeConfigResource() resource.Resource {
	return &AwsMetricScrapeConfigResource{}
}

// AwsMetricScrapeConfigResource manages the CloudWatch metric scrape config of one AWS integration.
type AwsMetricScrapeConfigResource struct {
	client client.Client
}

type awsMetricScrapeConfigModel struct {
	IntegrationID             types.String `tfsdk:"integration_id"`
	Origin                    types.String `tfsdk:"origin"`
	AwsMetricScrapeConfigYaml types.String `tfsdk:"aws_metric_scrape_config_yaml"`
}

// Configure adds the provider configured client to the resource.
func (r *AwsMetricScrapeConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(resourceProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected provider.resourceProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = data.client
}

func (r *AwsMetricScrapeConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_metric_scrape_config"
}

func (r *AwsMetricScrapeConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the CloudWatch metric scrape configuration of a Dash0 AWS integration: " +
			"which CloudWatch namespaces and metrics Dash0 collects, optionally filtered by resource.\n\n" +
			"Each AWS integration has exactly one scrape configuration. " +
			"Declare at most one `dash0_aws_metric_scrape_config` per integration: " +
			"two resources, configurations or stacks that manage the same integration overwrite each other on every apply. " +
			"The configuration belongs to the integration's dataset, so the resource has no `dataset` attribute.\n\n" +
			"The `spec.namespaces` list is the complete set of namespaces to scrape. " +
			"An empty list (`namespaces: []`) stops all scraping for the integration.\n\n" +
			"Destroying the resource clears the scrape configuration, so Dash0 stops scraping CloudWatch metrics for the integration. " +
			"The AWS integration itself is kept.\n\n" +
			"Dash0 records the first client that writes a scrape configuration as its owner (the `origin`) and keeps that owner until the configuration is cleared. " +
			"If another API client or Terraform configuration already manages the integration's scrape configuration, " +
			"this resource takes over its content but Dash0 keeps showing the previous owner. " +
			"To make Terraform the owner, clear the existing scrape configuration once before the first apply. " +
			"A scrape configuration created in the Dash0 UI has no owner. After you import it, Terraform manages its content but does not set its own origin.\n\n" +
			"See [About AWS Monitoring](https://dash0.com/docs/dash0/infrastructure/aws/about-aws) for how the AWS integration works.",

		Attributes: map[string]schema.Attribute{
			"integration_id": schema.StringAttribute{
				Description: "The id (UUID) of the Dash0 AWS integration whose scrape configuration this resource manages. Changing this value forces the resource to be recreated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"origin": schema.StringAttribute{
				Description: "The `dash0.com/origin` label of the scrape configuration, which records who manages it. Generated on creation as `tf_<uuid>` and refreshed from Dash0 on read. Null for a configuration imported without an origin.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"aws_metric_scrape_config_yaml": schema.StringAttribute{
				Description: "The scrape configuration in YAML format, with `apiVersion: dash0.com/v1alpha1`, `kind: Dash0AwsMetricScrapeConfig` " +
					"and a `spec.namespaces` list. Each entry names a CloudWatch `namespace`, the `metrics` to collect from it, and an optional `filter`. " +
					"`spec.integrationFilter` optionally applies to all namespaces. " +
					"`metadata.name`, labels and annotations are managed by Dash0 and ignored, " +
					"and the order of namespaces and metrics does not matter.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					customplanmodifier.YAMLSemanticEqualWith(awsMetricScrapeConfigIgnoredFields),
				},
			},
		},
	}
}

// ValidateConfig checks integration_id and the YAML syntax at plan time.
func (r *AwsMetricScrapeConfigResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model awsMetricScrapeConfigModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if id := model.IntegrationID; !id.IsNull() && !id.IsUnknown() && !isUUID(id.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("integration_id"), "Invalid integration_id",
			fmt.Sprintf("integration_id must be the UUID of a Dash0 AWS integration, got %q.", id.ValueString()))
	}

	if y := model.AwsMetricScrapeConfigYaml; !y.IsNull() && !y.IsUnknown() {
		var parsed map[string]interface{}
		if err := yaml.Unmarshal([]byte(y.ValueString()), &parsed); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("aws_metric_scrape_config_yaml"), "Invalid YAML",
				fmt.Sprintf("aws_metric_scrape_config_yaml is not valid YAML: %s", err))
			return
		}
		if _, err := parseAwsScrapeYAML(y.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("aws_metric_scrape_config_yaml"), "Invalid AWS metric scrape config",
				err.Error())
		}
	}
}

func isUUID(s string) bool {
	// uuid.Parse also accepts braced, URN and undashed forms; require the canonical one.
	_, err := uuid.Parse(s)
	return err == nil && len(s) == 36
}

func (r *AwsMetricScrapeConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model awsMetricScrapeConfigModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Origin = types.StringValue("tf_" + uuid.New().String())
	if !r.put(ctx, &model, &resp.Diagnostics) {
		return
	}

	tflog.Trace(ctx, "created an AWS metric scrape config resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *AwsMetricScrapeConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state, plan awsMetricScrapeConfigModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The origin is planned from state, so an imported config without one keeps none.
	plan.Origin = state.Origin
	if !r.put(ctx, &plan, &resp.Diagnostics) {
		return
	}

	tflog.Trace(ctx, "updated an AWS metric scrape config resource")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// put replaces the integration's scrape config with the model's YAML.
func (r *AwsMetricScrapeConfigResource) put(ctx context.Context, model *awsMetricScrapeConfigModel, diags *diag.Diagnostics) bool {
	jsonBody, err := converter.ConvertYAMLToJSON(model.AwsMetricScrapeConfigYaml.ValueString())
	if err != nil {
		diags.AddError("Invalid YAML", fmt.Sprintf("Unable to convert AWS metric scrape config YAML to JSON: %s", err))
		return false
	}
	if err := r.client.UpdateAwsMetricScrapeConfig(ctx, model.IntegrationID.ValueString(), model.Origin.ValueString(), jsonBody); err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to write AWS metric scrape config, got error: %s", err))
		return false
	}
	return true
}

func (r *AwsMetricScrapeConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsMetricScrapeConfigModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	integrationID := state.IntegrationID.ValueString()
	apiJSON, origin, err := r.client.GetAwsMetricScrapeConfig(ctx, integrationID)
	if removedOutOfBand(ctx, resp, "AWS metric scrape config of integration", integrationID, apiJSON, err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read AWS metric scrape config, got error: %s", err))
		return
	}

	// The API never returns 404 for an existing integration: a config cleared
	// outside Terraform comes back without origin and without namespaces. The
	// server also drops the origin when Terraform itself writes no namespaces
	// and no integration filter, so keep the resource when the state asks for that.
	serverCleared := origin == "" && !hasAwsScrapeContent(apiJSON)
	if serverCleared && !isEmptyAwsScrapeYAML(state.AwsMetricScrapeConfigYaml.ValueString()) {
		tflog.Debug(ctx, fmt.Sprintf("AWS metric scrape config of integration %s was cleared; removing from state", integrationID))
		resp.State.RemoveResource(ctx)
		return
	}
	if !serverCleared {
		state.Origin = stringOrNull(origin)
	}
	equivalent, err := converter.ResourceYAMLEquivalent(state.AwsMetricScrapeConfigYaml.ValueString(), apiJSON, awsMetricScrapeConfigIgnoredFields, nil)
	if err != nil {
		resp.Diagnostics.AddWarning("AWS Metric Scrape Config Comparison Error",
			fmt.Sprintf("Error comparing AWS metric scrape configs: %s. Using API response as source of truth.", err))
		state.AwsMetricScrapeConfigYaml = types.StringValue(apiJSON)
	} else if !equivalent {
		tflog.Debug(ctx, "AWS metric scrape config has changed, updating state")
		state.AwsMetricScrapeConfigYaml = types.StringValue(apiJSON)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// hasAwsScrapeContent reports whether the config has namespaces or an integration filter.
func hasAwsScrapeContent(cfgJSON string) bool {
	var cfg dash0.AwsMetricScrapeConfigDefinition
	return json.Unmarshal([]byte(cfgJSON), &cfg) == nil && !isEmptyAwsScrapeConfig(&cfg)
}

// isEmptyAwsScrapeConfig reports whether the config has no namespaces and no
// integration filter, the shape the server stores without an origin. The
// server drops an empty filter, so [] counts as no filter.
func isEmptyAwsScrapeConfig(cfg *dash0.AwsMetricScrapeConfigDefinition) bool {
	return len(cfg.Spec.Namespaces) == 0 && (cfg.Spec.IntegrationFilter == nil || len(*cfg.Spec.IntegrationFilter) == 0)
}

func parseAwsScrapeYAML(cfgYAML string) (*dash0.AwsMetricScrapeConfigDefinition, error) {
	cfgJSON, err := converter.ConvertYAMLToJSON(cfgYAML)
	if err != nil {
		return nil, err
	}
	return client.ParseAwsMetricScrapeConfig(cfgJSON)
}

func isEmptyAwsScrapeYAML(cfgYAML string) bool {
	cfg, err := parseAwsScrapeYAML(cfgYAML)
	return err == nil && isEmptyAwsScrapeConfig(cfg)
}

func (r *AwsMetricScrapeConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsMetricScrapeConfigModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAwsMetricScrapeConfig(ctx, state.IntegrationID.ValueString()); err != nil {
		if dash0.IsNotFound(err) {
			tflog.Debug(ctx, fmt.Sprintf("AWS integration %s is gone; treating delete as successful", state.IntegrationID.ValueString()))
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to clear AWS metric scrape config, got error: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted an AWS metric scrape config resource")
}

// ImportState imports the scrape config of the AWS integration whose id is the import ID.
func (r *AwsMetricScrapeConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	integrationID := req.ID
	if !isUUID(integrationID) {
		resp.Diagnostics.AddError("Invalid Import ID",
			fmt.Sprintf("Expected the UUID of a Dash0 AWS integration as import ID. Got: %s", integrationID))
		return
	}

	apiJSON, origin, err := r.client.GetAwsMetricScrapeConfig(ctx, integrationID)
	if err != nil {
		resp.Diagnostics.AddError("Error Importing AWS Metric Scrape Config",
			fmt.Sprintf("Could not get AWS metric scrape config of integration %s: %s", integrationID, err))
		return
	}
	if origin == "" && !hasAwsScrapeContent(apiJSON) {
		resp.Diagnostics.AddError("Error Importing AWS Metric Scrape Config",
			fmt.Sprintf("AWS integration %s has no metric scrape config to import. Create the resource instead.", integrationID))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("integration_id"), integrationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("origin"), stringOrNull(origin))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("aws_metric_scrape_config_yaml"), apiJSON)...)
}
