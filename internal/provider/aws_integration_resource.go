package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/model"
)

var (
	_ resource.Resource                = &AwsIntegrationResource{}
	_ resource.ResourceWithConfigure   = &AwsIntegrationResource{}
	_ resource.ResourceWithImportState = &AwsIntegrationResource{}
)

func NewAwsIntegrationResource() resource.Resource {
	return &AwsIntegrationResource{}
}

type AwsIntegrationResource struct {
	client client.Client
}

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
		Description: "Registers an AWS integration with the Dash0 API. This resource does NOT create " +
			"IAM roles — you manage them separately and pass the role ARNs here. Three supported paths: " +
			"(1) Use the turnkey Terraform module shipped at `modules/aws_integration` in this repo " +
			"(recommended; consume via `source = \"git::https://github.com/dash0hq/terraform-provider-dash0.git//modules/aws_integration?ref=...\"`); " +
			"(2) Create the roles yourself with the `hashicorp/aws` provider for full control; " +
			"(3) Pass ARNs of pre-existing roles created by your platform team. Keeping IAM " +
			"as first-class `aws_iam_role` resources enables `default_tags` cascade, `lifecycle` rules, " +
			"cross-resource references, and centralized IAM workflows.",
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
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"external_id": schema.StringAttribute{
				Description: "The Dash0 organization technical ID (also referred to as the organization ID in " +
					"Dash0's UI). Used as the STS AssumeRole external ID in the IAM trust policy — the field name " +
					"matches AWS terminology.",
				Required:   true,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"aws_account_id": schema.StringAttribute{
				Description: "The AWS account ID that hosts the IAM roles.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"read_only_role_arn": schema.StringAttribute{
				Description: "The ARN of the Dash0 read-only IAM role.",
				Required:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"instrumentation_role_arn": schema.StringAttribute{
				Description: "The ARN of the Dash0 resources instrumentation IAM role (e.g., for Lambda auto-instrumentation). Omit (null) when not using resources instrumentation — empty string is not accepted.",
				Optional:    true,
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *AwsIntegrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model.AwsIntegration
	if diags := req.Plan.Get(ctx, &plan); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	r.upsert(ctx, plan, "register", &resp.State, &resp.Diagnostics)
}

func (r *AwsIntegrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model.AwsIntegration
	if diags := req.Plan.Get(ctx, &plan); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	r.upsert(ctx, plan, "update", &resp.State, &resp.Diagnostics)
}

// upsert is the shared body of Create and Update: the Dash0 integrations API uses PUT for both.
func (r *AwsIntegrationResource) upsert(ctx context.Context, plan model.AwsIntegration, verb string, state *tfsdk.State, diags *diag.Diagnostics) {
	plan.ID = types.StringValue(model.AwsIntegrationID(plan.AwsAccountID.ValueString(), plan.ExternalID.ValueString()))

	if err := r.client.CreateOrUpdateAwsIntegration(ctx, plan); err != nil {
		diags.AddError("Dash0 API Error",
			fmt.Sprintf("Unable to %s AWS integration with Dash0 API: %s", verb, err))
		return
	}
	tflog.Trace(ctx, fmt.Sprintf("%sd AWS integration resource", verb))

	diags.Append(state.Set(ctx, plan)...)
}

func (r *AwsIntegrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model.AwsIntegration
	if diags := req.State.Get(ctx, &state); diags.HasError() {
		resp.Diagnostics.Append(diags...)
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

	state.Dataset = types.StringValue(apiResp.Dataset)
	state.AwsAccountID = types.StringValue(apiResp.AccountID)

	var readOnlyArn, instrArn string
	for _, role := range apiResp.Roles {
		switch role.PermissionType {
		case model.PermissionTypeReadOnly:
			readOnlyArn = role.Arn
		case model.PermissionTypeResourcesInstrumentation:
			instrArn = role.Arn
		default:
			tflog.Warn(ctx, fmt.Sprintf("Ignoring unknown AWS integration role permission_type %q", role.PermissionType))
		}
	}
	if readOnlyArn == "" {
		resp.Diagnostics.AddError("Dash0 API Drift",
			"Dash0 returned an AWS integration without a read-only role. The integration must be re-created or "+
				"repaired in the Dash0 UI. Run 'terraform apply' to re-register with the current configuration.")
		return
	}
	state.ReadOnlyRoleArn = types.StringValue(readOnlyArn)
	if instrArn != "" {
		state.InstrumentationRoleArn = types.StringValue(instrArn)
	} else {
		state.InstrumentationRoleArn = types.StringNull()
	}

	state.ID = types.StringValue(model.AwsIntegrationID(state.AwsAccountID.ValueString(), state.ExternalID.ValueString()))

	tflog.Trace(ctx, "read AWS integration resource")

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *AwsIntegrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model.AwsIntegration
	if diags := req.State.Get(ctx, &state); diags.HasError() {
		resp.Diagnostics.Append(diags...)
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

// ImportState accepts IDs in the format "dataset,aws_account_id,external_id".
func (r *AwsIntegrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ",")
	if len(parts) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in the format 'dataset,aws_account_id,external_id'. Got: %s", req.ID),
		)
		return
	}

	dataset, accountID, externalID := parts[0], parts[1], parts[2]
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dataset"), dataset)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("aws_account_id"), accountID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("external_id"), externalID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.AwsIntegrationID(accountID, externalID))...)
}
