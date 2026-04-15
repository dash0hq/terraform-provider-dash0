package model

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// AwsIntegration represents the Terraform state for the dash0_aws_integration resource.
type AwsIntegration struct {
	ID types.String `tfsdk:"id"`

	Dataset                types.String `tfsdk:"dataset"`
	ExternalID             types.String `tfsdk:"external_id"`
	AwsAccountID           types.String `tfsdk:"aws_account_id"`
	ReadOnlyRoleArn        types.String `tfsdk:"read_only_role_arn"`
	InstrumentationRoleArn types.String `tfsdk:"instrumentation_role_arn"`
}

// API contract constants used when building the IntegrationDefinition payload.
const (
	integrationKindDash0 = "Dash0Integration"
	integrationKindAWS   = "aws"
	aiAccessNone         = "none"

	PermissionTypeReadOnly                 = "read_only"
	PermissionTypeResourcesInstrumentation = "resources_instrumentation"
)

// AwsIntegrationID composes the resource's Terraform state ID.
func AwsIntegrationID(accountID, externalID string) string {
	return accountID + "-" + externalID
}

// AwsIntegrationOrigin computes the deterministic origin for the integrations API.
// Format: "terraform-<sha1_uuid(dataset + "-" + accountID + "-" + externalID)>"
func AwsIntegrationOrigin(dataset, accountID, externalID string) string {
	return "terraform-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(dataset+"-"+accountID+"-"+externalID)).String()
}

// IntegrationDefinition is the top-level envelope for PUT/GET /api/integrations/{origin}.
type IntegrationDefinition struct {
	Kind     string              `json:"kind"`
	Metadata IntegrationMetadata `json:"metadata"`
	Spec     IntegrationSpec     `json:"spec"`
}

type IntegrationMetadata struct {
	Name   string            `json:"name"`
	Labels IntegrationLabels `json:"labels,omitempty"`
}

type IntegrationLabels struct {
	Origin string `json:"dash0.com/origin,omitempty"`
}

type IntegrationSpec struct {
	Enabled     bool               `json:"enabled"`
	Display     IntegrationDisplay `json:"display"`
	AI          IntegrationAI      `json:"ai"`
	Integration IntegrationInner   `json:"integration"`
}

type IntegrationDisplay struct {
	Name string `json:"name"`
}

type IntegrationAI struct {
	Access string `json:"access"`
}

type IntegrationInner struct {
	Kind string             `json:"kind"`
	Spec AwsIntegrationSpec `json:"spec"`
}

type AwsIntegrationSpec struct {
	Dataset   string               `json:"dataset"`
	AccountID string               `json:"accountId"`
	Roles     []AwsIntegrationRole `json:"roles"`
}

type AwsIntegrationRole struct {
	Arn            string `json:"arn"`
	ExternalID     string `json:"externalId"`
	PermissionType string `json:"permissionType"`
}

// BuildAwsIntegrationDefinition constructs the IntegrationDefinition for PUT /api/integrations/{origin}.
func BuildAwsIntegrationDefinition(integration AwsIntegration, origin string) IntegrationDefinition {
	accountID := integration.AwsAccountID.ValueString()
	displayName := fmt.Sprintf("AWS %s (terraform)", accountID)

	roles := []AwsIntegrationRole{
		{
			Arn:            integration.ReadOnlyRoleArn.ValueString(),
			ExternalID:     integration.ExternalID.ValueString(),
			PermissionType: PermissionTypeReadOnly,
		},
	}

	if !integration.InstrumentationRoleArn.IsNull() && !integration.InstrumentationRoleArn.IsUnknown() && integration.InstrumentationRoleArn.ValueString() != "" {
		roles = append(roles, AwsIntegrationRole{
			Arn:            integration.InstrumentationRoleArn.ValueString(),
			ExternalID:     integration.ExternalID.ValueString(),
			PermissionType: PermissionTypeResourcesInstrumentation,
		})
	}

	return IntegrationDefinition{
		Kind: integrationKindDash0,
		Metadata: IntegrationMetadata{
			Name:   displayName,
			Labels: IntegrationLabels{Origin: origin},
		},
		Spec: IntegrationSpec{
			Enabled: true,
			Display: IntegrationDisplay{Name: displayName},
			AI:      IntegrationAI{Access: aiAccessNone},
			Integration: IntegrationInner{
				Kind: integrationKindAWS,
				Spec: AwsIntegrationSpec{
					Dataset:   integration.Dataset.ValueString(),
					AccountID: accountID,
					Roles:     roles,
				},
			},
		},
	}
}
