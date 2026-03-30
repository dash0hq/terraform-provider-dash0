package model

import "github.com/hashicorp/terraform-plugin-framework/types"

// AwsIntegration represents the Terraform state for the dash0_aws_integration resource.
type AwsIntegration struct {
	// Computed identifier: "{aws_account_id}-{external_id}"
	ID types.String `tfsdk:"id"`

	// Dash0-side attributes
	Dataset    types.String `tfsdk:"dataset"`
	ExternalID types.String `tfsdk:"external_id"`

	// AWS IAM configuration
	IamRoleNamePrefix              types.String `tfsdk:"iam_role_name_prefix"`
	EnableResourcesInstrumentation types.Bool   `tfsdk:"enable_resources_instrumentation"`
	Dash0AwsAccountID              types.String `tfsdk:"dash0_aws_account_id"`
	Tags                           types.Map    `tfsdk:"tags"`

	// AWS credentials (optional, defaults to SDK credential chain)
	AwsRegion    types.String `tfsdk:"aws_region"`
	AwsProfile   types.String `tfsdk:"aws_profile"`
	AwsAccessKey types.String `tfsdk:"aws_access_key"`
	AwsSecretKey types.String `tfsdk:"aws_secret_key"`

	// Computed outputs
	ReadOnlyRoleArn        types.String `tfsdk:"read_only_role_arn"`
	InstrumentationRoleArn types.String `tfsdk:"instrumentation_role_arn"`
	AwsAccountID           types.String `tfsdk:"aws_account_id"`
}

// AwsIntegrationApiPayload represents the JSON payload for the Dash0 IAC integration API.
type AwsIntegrationApiPayload struct {
	Action                          string  `json:"action"`
	Source                          string  `json:"source"`
	SourceStateID                   string  `json:"sourceStateId"`
	RoleArn                         string  `json:"roleArn,omitempty"`
	ResourcesInstrumentationRoleArn *string `json:"resourcesInstrumentationRoleArn"`
	ExternalID                      string  `json:"externalId"`
	Dataset                         string  `json:"dataset,omitempty"`
}
