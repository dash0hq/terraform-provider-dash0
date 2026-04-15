package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

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
		Description: "Registers an AWS integration with the Dash0 API. The user is responsible for " +
			"creating the IAM roles (either directly via the hashicorp/aws provider, via the official " +
			"Dash0 AWS integration Terraform module, or centrally by a platform team) and passing the " +
			"role ARNs to this resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Composite identifier in the format '{aws_account_id}-{external_id}'.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dataset": schema.StringAttribute{
				Description: "The Dash0 dataset slug to associate with this integration.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"external_id": schema.StringAttribute{
				Description: "The Dash0 organization technical ID, used as the STS AssumeRole external ID.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"aws_account_id": schema.StringAttribute{
				Description: "The AWS account ID that hosts the IAM roles.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"read_only_role_arn": schema.StringAttribute{
				Description: "The ARN of the Dash0 read-only IAM role.",
				Required:    true,
			},
			"instrumentation_role_arn": schema.StringAttribute{
				Description: "The ARN of the Dash0 resources instrumentation IAM role (e.g., for Lambda auto-instrumentation). Omit if not using resources instrumentation.",
				Optional:    true,
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

	plan.ID = types.StringValue(fmt.Sprintf("%s-%s", plan.AwsAccountID.ValueString(), plan.ExternalID.ValueString()))

	if err := r.client.CreateOrUpdateAwsIntegration(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Dash0 API Error",
			fmt.Sprintf("Unable to register AWS integration with Dash0 API: %s", err))
		return
	}

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

	apiResp, err := r.client.GetAwsIntegration(ctx,
		state.Dataset.ValueString(),
		state.AwsAccountID.ValueString(),
		state.ExternalID.ValueString(),
	)
	if err != nil {
		if client.IsNotFound(err) {
			tflog.Warn(ctx, fmt.Sprintf("AWS integration not found in Dash0 API, removing from state: %s", err))
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Dash0 API Error",
			fmt.Sprintf("Unable to read AWS integration from Dash0 API: %s", err))
		return
	}

	// Reconcile state with API response (drift detection).
	state.Dataset = types.StringValue(apiResp.Dataset)
	state.AwsAccountID = types.StringValue(apiResp.AccountID)

	var readOnlyArn, instrArn string
	for _, role := range apiResp.Roles {
		switch role.PermissionType {
		case model.PermissionTypeReadOnly:
			readOnlyArn = role.Arn
		case model.PermissionTypeResourcesInstrumentation:
			instrArn = role.Arn
		}
	}
	state.ReadOnlyRoleArn = types.StringValue(readOnlyArn)
	if instrArn != "" {
		state.InstrumentationRoleArn = types.StringValue(instrArn)
	} else {
		state.InstrumentationRoleArn = types.StringNull()
	}

	state.ID = types.StringValue(fmt.Sprintf("%s-%s", state.AwsAccountID.ValueString(), state.ExternalID.ValueString()))

	tflog.Trace(ctx, "read AWS integration resource")

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *AwsIntegrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model.AwsIntegration
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s-%s", plan.AwsAccountID.ValueString(), plan.ExternalID.ValueString()))

	if err := r.client.CreateOrUpdateAwsIntegration(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Dash0 API Error",
			fmt.Sprintf("Unable to update AWS integration registration: %s", err))
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

	err := r.client.DeleteAwsIntegration(ctx,
		state.Dataset.ValueString(),
		state.AwsAccountID.ValueString(),
		state.ExternalID.ValueString(),
	)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Dash0 API Error",
			fmt.Sprintf("Unable to delete AWS integration from Dash0 API: %s", err))
		return
	}

	tflog.Trace(ctx, "deleted AWS integration resource")
}

// ImportState handles terraform import for existing AWS integrations.
// Import ID format: "dataset,aws_account_id,external_id"
func (r *AwsIntegrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in the format 'dataset,aws_account_id,external_id'. Got: %s", req.ID),
		)
		return
	}

	dataset := idParts[0]
	accountID := idParts[1]
	externalID := idParts[2]

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("aws_account_id"), accountID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("external_id"), externalID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s-%s", accountID, externalID))...)
}
