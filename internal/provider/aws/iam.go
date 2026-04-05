package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	viewOnlyAccessPolicyArn = "arn:aws:iam::aws:policy/job-function/ViewOnlyAccess"
	customPolicyName        = "Dash0ReadOnly"

	readOnlyRoleSuffix          = "-read-only"
	instrumentationRoleSuffix   = "-instrumentation"
	instrumentationPolicySuffix = "-lambda-instrumentation"
)

// ReadOnlyRoleName returns the full read-only role name for the given prefix.
func ReadOnlyRoleName(prefix string) string {
	return prefix + readOnlyRoleSuffix
}

// InstrumentationRoleName returns the full instrumentation role name for the given prefix.
func InstrumentationRoleName(prefix string) string {
	return prefix + instrumentationRoleSuffix
}

// InstrumentationPolicyName returns the full instrumentation policy name for the given prefix.
func InstrumentationPolicyName(prefix string) string {
	return prefix + instrumentationPolicySuffix
}

// IAMClient wraps the AWS IAM and STS clients for role management.
type IAMClient struct {
	iamClient *iam.Client
	stsClient *sts.Client
}

// RoleParams holds parameters for creating IAM roles.
type RoleParams struct {
	RoleNamePrefix    string
	Dash0AwsAccountID string
	ExternalID        string
	Tags              map[string]string
}

// RoleInfo holds the output from reading/creating a role.
type RoleInfo struct {
	RoleArn  string
	RoleName string
}

// NewIAMClient creates a new AWS IAM client from the given configuration.
func NewIAMClient(ctx context.Context, region, profile, accessKey, secretKey string) (*IAMClient, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if accessKey != "" && secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	return &IAMClient{
		iamClient: iam.NewFromConfig(cfg),
		stsClient: sts.NewFromConfig(cfg),
	}, nil
}

// GetCallerAccountID returns the AWS account ID of the caller.
func (c *IAMClient) GetCallerAccountID(ctx context.Context) (string, error) {
	output, err := c.stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get caller identity: %w", err)
	}
	return *output.Account, nil
}

// buildTrustPolicy constructs the trust policy JSON for a Dash0 assume-role.
func buildTrustPolicy(dash0AwsAccountID, externalID string) (string, error) {
	policy := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"AWS": dash0AwsAccountID,
				},
				"Action": "sts:AssumeRole",
				"Condition": map[string]interface{}{
					"StringEquals": map[string]interface{}{
						"sts:ExternalId": externalID,
					},
				},
			},
		},
	}
	b, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("failed to marshal trust policy: %w", err)
	}
	return string(b), nil
}

// readOnlyCustomPolicyJSON is the pre-marshaled custom inline policy for read-only resource discovery.
var readOnlyCustomPolicyJSON = mustMarshalJSON(map[string]interface{}{
	"Version": "2012-10-17",
	"Statement": []map[string]interface{}{
		{
			"Effect": "Allow",
			"Action": []string{
				"resource-explorer-2:Search",
				"resource-explorer-2:GetView",
			},
			"Resource": "*",
		},
		{
			"Effect": "Allow",
			"Action": []string{
				"tag:GetResources",
				"tag:GetTagKeys",
				"tag:GetTagValues",
			},
			"Resource": "*",
		},
		{
			"Effect": "Allow",
			"Action": []string{
				"lambda:GetFunction",
				"lambda:GetFunctionConfiguration",
			},
			"Resource": "*",
		},
		{
			"Effect": "Allow",
			"Action": []string{
				"eks:ListClusters",
				"eks:DescribeCluster",
				"eks:ListNodegroups",
				"eks:DescribeNodegroup",
				"eks:ListFargateProfiles",
				"eks:DescribeFargateProfile",
				"eks:ListAddons",
				"eks:DescribeAddon",
			},
			"Resource": "*",
		},
		{
			"Effect": "Allow",
			"Action": []string{
				"appsync:ListGraphqlApis",
				"appsync:GetGraphqlApi",
				"appsync:GetSchemaCreationStatus",
				"appsync:GetIntrospectionSchema",
				"appsync:ListDataSources",
				"appsync:ListResolvers",
				"appsync:ListFunctions",
				"appsync:ListTagsForResource",
			},
			"Resource": "*",
		},
		{
			"Effect": "Allow",
			"Action": []string{
				"xray:GetTraceSegmentDestination",
				"xray:GetIndexingRules",
			},
			"Resource": "*",
		},
	},
})

// instrumentationPolicyJSON is the pre-marshaled policy for Lambda auto-instrumentation.
var instrumentationPolicyJSON = mustMarshalJSON(map[string]interface{}{
	"Version": "2012-10-17",
	"Statement": []map[string]interface{}{
		{
			"Effect": "Allow",
			"Action": []string{
				"lambda:GetFunctionConfiguration",
				"lambda:UpdateFunctionConfiguration",
			},
			"Resource": "arn:aws:lambda:*:*:function:*",
		},
		{
			"Effect": "Allow",
			"Action": []string{
				"ec2:DescribeRouteTables",
				"ec2:DescribeSecurityGroups",
				"ec2:DescribeVpcAttribute",
				"lambda:GetLayerVersion",
				"lambda:GetLayerVersionPolicy",
			},
			"Resource": "*",
		},
	},
})

func mustMarshalJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal static policy JSON: %s", err))
	}
	return string(b)
}

// convertTags converts a map of tags to IAM tag format.
func convertTags(tags map[string]string) []iamtypes.Tag {
	iamTags := make([]iamtypes.Tag, 0, len(tags))
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	return iamTags
}

// CreateReadOnlyRole creates the Dash0 read-only IAM role with all required policies.
func (c *IAMClient) CreateReadOnlyRole(ctx context.Context, params RoleParams) (*RoleInfo, error) {
	roleName := ReadOnlyRoleName(params.RoleNamePrefix)

	trustPolicy, err := buildTrustPolicy(params.Dash0AwsAccountID, params.ExternalID)
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating read-only IAM role: %s", roleName))

	// Create the role
	createOutput, err := c.iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
		Tags:                     convertTags(params.Tags),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create read-only role %q: %w", roleName, err)
	}

	roleArn := *createOutput.Role.Arn

	// Attach ViewOnlyAccess managed policy
	_, err = c.iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(viewOnlyAccessPolicyArn),
	})
	if err != nil {
		// Attempt cleanup on failure
		c.deleteRoleBestEffort(ctx, roleName)
		return nil, fmt.Errorf("failed to attach ViewOnlyAccess policy to role %q: %w", roleName, err)
	}

	// Attach custom inline policy
	_, err = c.iamClient.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       aws.String(roleName),
		PolicyName:     aws.String(customPolicyName),
		PolicyDocument: aws.String(readOnlyCustomPolicyJSON),
	})
	if err != nil {
		c.deleteRoleBestEffort(ctx, roleName)
		return nil, fmt.Errorf("failed to put inline policy on role %q: %w", roleName, err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Created read-only IAM role: %s (ARN: %s)", roleName, roleArn))

	return &RoleInfo{
		RoleArn:  roleArn,
		RoleName: roleName,
	}, nil
}

// CreateInstrumentationRole creates the Dash0 instrumentation IAM role.
func (c *IAMClient) CreateInstrumentationRole(ctx context.Context, params RoleParams) (*RoleInfo, error) {
	roleName := InstrumentationRoleName(params.RoleNamePrefix)

	trustPolicy, err := buildTrustPolicy(params.Dash0AwsAccountID, params.ExternalID)
	if err != nil {
		return nil, err
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating instrumentation IAM role: %s", roleName))

	// Create the role
	createOutput, err := c.iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
		Tags:                     convertTags(params.Tags),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create instrumentation role %q: %w", roleName, err)
	}

	roleArn := *createOutput.Role.Arn

	// Create the managed policy (name is prefix-scoped to avoid collisions)
	policyName := InstrumentationPolicyName(params.RoleNamePrefix)
	policyOutput, err := c.iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(policyName),
		PolicyDocument: aws.String(instrumentationPolicyJSON),
		Tags:           convertTags(params.Tags),
	})
	if err != nil {
		c.deleteRoleBestEffort(ctx, roleName)
		return nil, fmt.Errorf("failed to create instrumentation policy: %w", err)
	}

	policyArn := *policyOutput.Policy.Arn

	// Attach the policy to the role
	_, err = c.iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	})
	if err != nil {
		c.deletePolicyBestEffort(ctx, policyArn)
		c.deleteRoleBestEffort(ctx, roleName)
		return nil, fmt.Errorf("failed to attach instrumentation policy to role %q: %w", roleName, err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Created instrumentation IAM role: %s (ARN: %s)", roleName, roleArn))

	return &RoleInfo{
		RoleArn:  roleArn,
		RoleName: roleName,
	}, nil
}

// ReadRole checks if a role exists and returns its info.
func (c *IAMClient) ReadRole(ctx context.Context, roleName string) (*RoleInfo, error) {
	output, err := c.iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get role %q: %w", roleName, err)
	}

	return &RoleInfo{
		RoleArn:  *output.Role.Arn,
		RoleName: roleName,
	}, nil
}

// DeleteReadOnlyRole deletes the read-only role and its attached policies.
func (c *IAMClient) DeleteReadOnlyRole(ctx context.Context, roleNamePrefix string) error {
	roleName := ReadOnlyRoleName(roleNamePrefix)
	tflog.Debug(ctx, fmt.Sprintf("Deleting read-only IAM role: %s", roleName))

	// Delete inline policy
	_, err := c.iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
		RoleName:   aws.String(roleName),
		PolicyName: aws.String(customPolicyName),
	})
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Failed to delete inline policy %q from role %q: %s", customPolicyName, roleName, err))
	}

	// Detach ViewOnlyAccess managed policy
	_, err = c.iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(viewOnlyAccessPolicyArn),
	})
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Failed to detach ViewOnlyAccess policy from role %q: %s", roleName, err))
	}

	// Delete the role
	_, err = c.iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("failed to delete read-only role %q: %w", roleName, err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Deleted read-only IAM role: %s", roleName))
	return nil
}

// DeleteInstrumentationRole deletes the instrumentation role and its policy.
func (c *IAMClient) DeleteInstrumentationRole(ctx context.Context, roleNamePrefix string, accountID string) error {
	roleName := InstrumentationRoleName(roleNamePrefix)
	policyArn := fmt.Sprintf("arn:aws:iam::%s:policy/%s", accountID, InstrumentationPolicyName(roleNamePrefix))

	tflog.Debug(ctx, fmt.Sprintf("Deleting instrumentation IAM role: %s", roleName))

	// Detach policy from role
	_, err := c.iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyArn),
	})
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Failed to detach instrumentation policy from role %q: %s", roleName, err))
	}

	// Delete the managed policy
	_, err = c.iamClient.DeletePolicy(ctx, &iam.DeletePolicyInput{
		PolicyArn: aws.String(policyArn),
	})
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Failed to delete instrumentation policy %q: %s", policyArn, err))
	}

	// Delete the role
	_, err = c.iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("failed to delete instrumentation role %q: %w", roleName, err)
	}

	tflog.Debug(ctx, fmt.Sprintf("Deleted instrumentation IAM role: %s", roleName))
	return nil
}

// UpdateRoleTags updates tags on an IAM role.
func (c *IAMClient) UpdateRoleTags(ctx context.Context, roleName string, tags map[string]string) error {
	// First untag all existing tags
	existingTags, err := c.iamClient.ListRoleTags(ctx, &iam.ListRoleTagsInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("failed to list tags for role %q: %w", roleName, err)
	}

	if len(existingTags.Tags) > 0 {
		tagKeys := make([]string, 0, len(existingTags.Tags))
		for _, t := range existingTags.Tags {
			tagKeys = append(tagKeys, *t.Key)
		}
		_, err = c.iamClient.UntagRole(ctx, &iam.UntagRoleInput{
			RoleName: aws.String(roleName),
			TagKeys:  tagKeys,
		})
		if err != nil {
			return fmt.Errorf("failed to untag role %q: %w", roleName, err)
		}
	}

	// Apply new tags
	if len(tags) > 0 {
		_, err = c.iamClient.TagRole(ctx, &iam.TagRoleInput{
			RoleName: aws.String(roleName),
			Tags:     convertTags(tags),
		})
		if err != nil {
			return fmt.Errorf("failed to tag role %q: %w", roleName, err)
		}
	}

	return nil
}

// WaitForRolePropagation waits briefly for IAM eventual consistency.
// It respects context cancellation.
func WaitForRolePropagation(ctx context.Context) {
	select {
	case <-time.After(10 * time.Second):
	case <-ctx.Done():
	}
}

// deleteRoleBestEffort attempts to detach all policies and delete a role, logging any errors.
func (c *IAMClient) deleteRoleBestEffort(ctx context.Context, roleName string) {
	// List and detach managed policies
	attached, err := c.iamClient.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err == nil {
		for _, p := range attached.AttachedPolicies {
			_, _ = c.iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
				RoleName:  aws.String(roleName),
				PolicyArn: p.PolicyArn,
			})
		}
	}

	// List and delete inline policies
	inline, err := c.iamClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	if err == nil {
		for _, pName := range inline.PolicyNames {
			_, _ = c.iamClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
				RoleName:   aws.String(roleName),
				PolicyName: aws.String(pName),
			})
		}
	}

	_, err = c.iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Best-effort cleanup: failed to delete role %q: %s", roleName, err))
	}
}

// deletePolicyBestEffort attempts to delete a policy, logging any errors.
func (c *IAMClient) deletePolicyBestEffort(ctx context.Context, policyArn string) {
	_, err := c.iamClient.DeletePolicy(ctx, &iam.DeletePolicyInput{
		PolicyArn: aws.String(policyArn),
	})
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Best-effort cleanup: failed to delete policy %q: %s", policyArn, err))
	}
}
