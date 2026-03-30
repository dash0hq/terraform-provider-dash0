package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	awsclient "github.com/dash0hq/terraform-provider-dash0/internal/provider/aws"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource                = &AwsIntegrationResource{}
	_ resource.ResourceWithConfigure   = &AwsIntegrationResource{}
	_ resource.ResourceWithImportState = &AwsIntegrationResource{}
)

// NewAwsIntegrationResource is a helper function to simplify the provider implementation.
func NewAwsIntegrationResource() resource.Resource {
	return &AwsIntegrationResource{}
}

// AwsIntegrationResource is the resource implementation.
type AwsIntegrationResource struct {
	client client.Client
}

// Configure adds the provider configured client to the resource.
func (r *AwsIntegrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = c
}

func (r *AwsIntegrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_integration"
}

func (r *AwsIntegrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Dash0 AWS integration. Creates IAM roles for resource discovery and monitoring, " +
			"and registers the integration with the Dash0 API. Optionally creates an instrumentation role for " +
			"Lambda auto-instrumentation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Composite identifier in the format '{aws_account_id}-{external_id}'.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			// Dash0-side attributes
			"dataset": schema.StringAttribute{
				Description: "The Dash0 dataset slug to associate with this integration.",
				Required:    true,
			},
			"external_id": schema.StringAttribute{
				Description: "The Dash0 organization technical ID, used as the STS AssumeRole external ID.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// AWS IAM configuration
			"iam_role_name_prefix": schema.StringAttribute{
				Description: "Prefix for the IAM role names. Defaults to 'dash0'.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("dash0"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"enable_resources_instrumentation": schema.BoolAttribute{
				Description: "Whether to create an additional IAM role for resources instrumentation (e.g., Lambda auto-instrumentation).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"dash0_aws_account_id": schema.StringAttribute{
				Description: "The Dash0 AWS account ID that will assume the IAM roles (used in the trust policy). Defaults to '115813213817'.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("115813213817"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.MapAttribute{
				Description: "Tags to apply to all IAM resources created by this resource.",
				Optional:    true,
				ElementType: types.StringType,
			},

			// AWS credentials (optional)
			"aws_region": schema.StringAttribute{
				Description: "AWS region. Defaults to the AWS SDK default credential chain.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"aws_profile": schema.StringAttribute{
				Description: "AWS shared config profile name.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"aws_access_key": schema.StringAttribute{
				Description: "AWS access key ID. If omitted, the default AWS SDK credential chain is used.",
				Optional:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"aws_secret_key": schema.StringAttribute{
				Description: "AWS secret access key. If omitted, the default AWS SDK credential chain is used.",
				Optional:    true,
				Sensitive:   true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			// Computed outputs
			"read_only_role_arn": schema.StringAttribute{
				Description: "The ARN of the Dash0 read-only IAM role.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"instrumentation_role_arn": schema.StringAttribute{
				Description: "The ARN of the Dash0 resources instrumentation IAM role (empty if not enabled).",
				Computed:    true,
			},
			"aws_account_id": schema.StringAttribute{
				Description: "The AWS account ID where the integration was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *AwsIntegrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model.AwsIntegration
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamClient, err := r.newIAMClient(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to create AWS client: %s", err))
		return
	}

	accountID, err := iamClient.GetCallerAccountID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("AWS Error", fmt.Sprintf("Unable to get AWS account ID: %s", err))
		return
	}

	params := r.extractRoleParams(ctx, plan)

	// Create read-only role
	readOnlyRole, err := iamClient.CreateReadOnlyRole(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("AWS IAM Error", fmt.Sprintf("Unable to create read-only IAM role: %s", err))
		return
	}

	plan.ReadOnlyRoleArn = types.StringValue(readOnlyRole.RoleArn)

	// Create instrumentation role (optional)
	var instrRoleArn *string
	if plan.EnableResourcesInstrumentation.ValueBool() {
		instrRole, err := iamClient.CreateInstrumentationRole(ctx, params)
		if err != nil {
			_ = iamClient.DeleteReadOnlyRole(ctx, params.RoleNamePrefix)
			resp.Diagnostics.AddError("AWS IAM Error",
				fmt.Sprintf("Unable to create instrumentation IAM role (read-only role %q was cleaned up): %s",
					readOnlyRole.RoleArn, err))
			return
		}
		instrRoleArn = &instrRole.RoleArn
		plan.InstrumentationRoleArn = types.StringValue(instrRole.RoleArn)
	} else {
		plan.InstrumentationRoleArn = types.StringValue("")
	}

	// Wait for IAM propagation before registering with Dash0
	awsclient.WaitForRolePropagation()

	// Register with Dash0 API
	sourceStateID := fmt.Sprintf("%s-%s", accountID, plan.ExternalID.ValueString())
	payload := model.AwsIntegrationApiPayload{
		SourceStateID:                   sourceStateID,
		RoleArn:                         readOnlyRole.RoleArn,
		ResourcesInstrumentationRoleArn: instrRoleArn,
		ExternalID:                      plan.ExternalID.ValueString(),
		Dataset:                         plan.Dataset.ValueString(),
	}

	err = r.client.CreateOrUpdateAwsIntegration(ctx, payload)
	if err != nil {
		// Store partial state so destroy can clean up IAM roles
		plan.ID = types.StringValue(sourceStateID)
		plan.AwsAccountID = types.StringValue(accountID)
		resp.State.Set(ctx, plan)
		resp.Diagnostics.AddError("Dash0 API Error",
			fmt.Sprintf("IAM roles were created successfully, but failed to register integration with Dash0 API: %s. "+
				"Run 'terraform destroy' to clean up the IAM roles, or 'terraform apply' to retry the registration.", err))
		return
	}

	plan.ID = types.StringValue(sourceStateID)
	plan.AwsAccountID = types.StringValue(accountID)

	tflog.Trace(ctx, "created AWS integration resource")

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *AwsIntegrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model.AwsIntegration
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamClient, err := r.newIAMClient(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to create AWS client: %s", err))
		return
	}

	prefix := state.IamRoleNamePrefix.ValueString()

	// Verify read-only role exists
	readOnlyRoleName := awsclient.ReadOnlyRoleName(prefix)
	readOnlyRole, err := iamClient.ReadRole(ctx, readOnlyRoleName)
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Read-only IAM role %q not found, removing resource from state: %s", readOnlyRoleName, err))
		resp.State.RemoveResource(ctx)
		return
	}
	state.ReadOnlyRoleArn = types.StringValue(readOnlyRole.RoleArn)

	// Verify instrumentation role if enabled
	if state.EnableResourcesInstrumentation.ValueBool() {
		instrRoleName := awsclient.InstrumentationRoleName(prefix)
		instrRole, err := iamClient.ReadRole(ctx, instrRoleName)
		if err != nil {
			tflog.Warn(ctx, fmt.Sprintf("Instrumentation IAM role %q not found, removing resource from state: %s", instrRoleName, err))
			resp.State.RemoveResource(ctx)
			return
		}
		state.InstrumentationRoleArn = types.StringValue(instrRole.RoleArn)
	}

	tflog.Trace(ctx, "read AWS integration resource")

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *AwsIntegrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state model.AwsIntegration
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var plan model.AwsIntegration
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Preserve computed values from state
	plan.ID = state.ID
	plan.AwsAccountID = state.AwsAccountID
	plan.ReadOnlyRoleArn = state.ReadOnlyRoleArn

	iamClient, err := r.newIAMClient(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to create AWS client: %s", err))
		return
	}

	params := r.extractRoleParams(ctx, plan)
	prefix := plan.IamRoleNamePrefix.ValueString()

	// Handle tags update on existing roles
	err = iamClient.UpdateRoleTags(ctx, awsclient.ReadOnlyRoleName(prefix), params.Tags)
	if err != nil {
		resp.Diagnostics.AddError("AWS IAM Error", fmt.Sprintf("Unable to update tags on read-only role: %s", err))
		return
	}

	// Handle instrumentation toggle
	wasEnabled := state.EnableResourcesInstrumentation.ValueBool()
	isEnabled := plan.EnableResourcesInstrumentation.ValueBool()

	if !wasEnabled && isEnabled {
		instrRole, err := iamClient.CreateInstrumentationRole(ctx, params)
		if err != nil {
			resp.Diagnostics.AddError("AWS IAM Error", fmt.Sprintf("Unable to create instrumentation IAM role: %s", err))
			return
		}
		plan.InstrumentationRoleArn = types.StringValue(instrRole.RoleArn)
	} else if wasEnabled && !isEnabled {
		err := iamClient.DeleteInstrumentationRole(ctx, prefix, plan.AwsAccountID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("AWS IAM Error", fmt.Sprintf("Unable to delete instrumentation IAM role: %s", err))
			return
		}
		plan.InstrumentationRoleArn = types.StringValue("")
	} else if isEnabled {
		err = iamClient.UpdateRoleTags(ctx, awsclient.InstrumentationRoleName(prefix), params.Tags)
		if err != nil {
			resp.Diagnostics.AddError("AWS IAM Error", fmt.Sprintf("Unable to update tags on instrumentation role: %s", err))
			return
		}
		plan.InstrumentationRoleArn = state.InstrumentationRoleArn
	} else {
		plan.InstrumentationRoleArn = types.StringValue("")
	}

	// Re-register with Dash0 API
	var instrRoleArn *string
	if isEnabled && plan.InstrumentationRoleArn.ValueString() != "" {
		arn := plan.InstrumentationRoleArn.ValueString()
		instrRoleArn = &arn
	}

	payload := model.AwsIntegrationApiPayload{
		SourceStateID:                   plan.ID.ValueString(),
		RoleArn:                         plan.ReadOnlyRoleArn.ValueString(),
		ResourcesInstrumentationRoleArn: instrRoleArn,
		ExternalID:                      plan.ExternalID.ValueString(),
		Dataset:                         plan.Dataset.ValueString(),
	}

	err = r.client.CreateOrUpdateAwsIntegration(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Dash0 API Error", fmt.Sprintf("Unable to update AWS integration registration: %s", err))
		return
	}

	tflog.Trace(ctx, "updated AWS integration resource")

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *AwsIntegrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model.AwsIntegration
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	iamClient, err := r.newIAMClient(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("AWS Configuration Error", fmt.Sprintf("Unable to create AWS client for cleanup: %s", err))
		return
	}

	prefix := state.IamRoleNamePrefix.ValueString()

	// Delete IAM roles first (avoid orphaned AWS resources if Dash0 API call fails)
	if state.EnableResourcesInstrumentation.ValueBool() {
		err = iamClient.DeleteInstrumentationRole(ctx, prefix, state.AwsAccountID.ValueString())
		if err != nil {
			tflog.Warn(ctx, fmt.Sprintf("Failed to delete instrumentation IAM role: %s", err))
		}
	}

	err = iamClient.DeleteReadOnlyRole(ctx, prefix)
	if err != nil {
		resp.Diagnostics.AddError("AWS IAM Error", fmt.Sprintf("Unable to delete read-only IAM role: %s", err))
		return
	}

	// Unregister from Dash0 API
	sourceStateID := state.ID.ValueString()
	err = r.client.DeleteAwsIntegration(ctx, sourceStateID, state.ExternalID.ValueString())
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Failed to delete AWS integration from Dash0 API: %s. IAM roles were cleaned up successfully.", err))
	}

	tflog.Trace(ctx, "deleted AWS integration resource")
}

// ImportState handles terraform import for existing AWS integrations.
// Import ID format: "dataset,external_id,iam_role_name_prefix" (prefix is optional, defaults to "dash0")
func (r *AwsIntegrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) < 2 || len(idParts) > 3 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in the format 'dataset,external_id[,iam_role_name_prefix]'. Got: %s", req.ID),
		)
		return
	}

	dataset := idParts[0]
	externalID := idParts[1]
	prefix := "dash0"
	if len(idParts) == 3 {
		prefix = idParts[2]
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("external_id"), externalID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("iam_role_name_prefix"), prefix)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dash0_aws_account_id"), "115813213817")...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enable_resources_instrumentation"), false)...)
}

// newIAMClient creates a new AWS IAM client from the resource state/plan.
func (r *AwsIntegrationResource) newIAMClient(ctx context.Context, m model.AwsIntegration) (*awsclient.IAMClient, error) {
	return awsclient.NewIAMClient(ctx,
		optionalString(m.AwsRegion),
		optionalString(m.AwsProfile),
		optionalString(m.AwsAccessKey),
		optionalString(m.AwsSecretKey),
	)
}

// extractRoleParams builds RoleParams from the model.
func (r *AwsIntegrationResource) extractRoleParams(ctx context.Context, m model.AwsIntegration) awsclient.RoleParams {
	return awsclient.RoleParams{
		RoleNamePrefix:    m.IamRoleNamePrefix.ValueString(),
		Dash0AwsAccountID: m.Dash0AwsAccountID.ValueString(),
		ExternalID:        m.ExternalID.ValueString(),
		Tags:              r.extractTags(ctx, m),
	}
}

// extractTags converts the plan's tags map to a Go map[string]string.
func (r *AwsIntegrationResource) extractTags(ctx context.Context, m model.AwsIntegration) map[string]string {
	tags := make(map[string]string)
	if !m.Tags.IsNull() && !m.Tags.IsUnknown() {
		diags := m.Tags.ElementsAs(ctx, &tags, false)
		if diags.HasError() {
			tflog.Warn(ctx, "Failed to extract tags from plan, using empty tags")
			return make(map[string]string)
		}
	}
	return tags
}

// optionalString extracts a string value from a types.String, returning empty string if null/unknown.
func optionalString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}
