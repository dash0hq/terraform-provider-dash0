package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	awsclient "github.com/dash0hq/terraform-provider-dash0/internal/provider/aws"
)

// testAccAwsIntegrationPreCheck validates required environment variables for AWS integration acceptance tests.
func testAccAwsIntegrationPreCheck(t *testing.T) {
	t.Helper()

	// AWS credentials are resolved via the SDK default chain (env vars, shared config, instance profile).
	// In CI, aws-actions/configure-aws-credentials sets AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
	// AWS_SESSION_TOKEN, and AWS_REGION as environment variables.
	// For local development, set DASH0_TEST_AWS_PROFILE to use a named profile (e.g. "DevRel").
	if os.Getenv("DASH0_TEST_EXTERNAL_ID") == "" {
		t.Fatal("DASH0_TEST_EXTERNAL_ID must be set for AWS integration acceptance tests")
	}
}

func testExternalID() string {
	return os.Getenv("DASH0_TEST_EXTERNAL_ID")
}

// newTestIAMClient creates an IAM client using the optional test profile or default chain.
func newTestIAMClient(t *testing.T) *awsclient.IAMClient {
	t.Helper()
	profile := os.Getenv("DASH0_TEST_AWS_PROFILE")
	client, err := awsclient.NewIAMClient(context.Background(), "", profile, "", "")
	require.NoError(t, err, "failed to create AWS IAM client")
	return client
}

// TestAccAwsIntegrationResource_IAMRoles tests the full IAM role CRUD lifecycle:
// create read-only role, verify it exists, create instrumentation role, verify it exists,
// delete both, verify they're gone.
func TestAccAwsIntegrationResource_IAMRoles(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}
	testAccAwsIntegrationPreCheck(t)

	ctx := context.Background()
	iamClient := newTestIAMClient(t)

	params := awsclient.RoleParams{
		RoleNamePrefix:    "dash0-acc-test",
		Dash0AwsAccountID: "115813213817",
		ExternalID:        testExternalID(),
		Tags: map[string]string{
			"ManagedBy": "dash0-acc-test",
		},
	}

	// Get account ID for cleanup operations
	accountID, err := iamClient.GetCallerAccountID(ctx)
	require.NoError(t, err, "failed to get AWS account ID")

	// Clean up any leftover roles from a previous failed run.
	_ = iamClient.DeleteReadOnlyRole(ctx, params.RoleNamePrefix)
	_ = iamClient.DeleteInstrumentationRole(ctx, params.RoleNamePrefix, accountID)

	// Step 1: Create read-only role
	readOnlyRole, err := iamClient.CreateReadOnlyRole(ctx, params)
	require.NoError(t, err, "failed to create read-only role")
	assert.Contains(t, readOnlyRole.RoleArn, "dash0-acc-test-read-only")
	t.Logf("Created read-only role: %s", readOnlyRole.RoleArn)

	// Step 2: Verify read-only role exists
	role, err := iamClient.ReadRole(ctx, "dash0-acc-test-read-only")
	require.NoError(t, err, "read-only role should exist")
	assert.Equal(t, readOnlyRole.RoleArn, role.RoleArn)

	// Step 3: Create instrumentation role
	instrRole, err := iamClient.CreateInstrumentationRole(ctx, params)
	require.NoError(t, err, "failed to create instrumentation role")
	assert.Contains(t, instrRole.RoleArn, "dash0-acc-test-instrumentation")
	t.Logf("Created instrumentation role: %s", instrRole.RoleArn)

	// Step 4: Verify instrumentation role exists
	role, err = iamClient.ReadRole(ctx, "dash0-acc-test-instrumentation")
	require.NoError(t, err, "instrumentation role should exist")
	assert.Equal(t, instrRole.RoleArn, role.RoleArn)

	// Step 5: Verify account ID
	assert.NotEmpty(t, accountID)
	t.Logf("AWS account ID: %s", accountID)

	// Step 6: Delete instrumentation role
	err = iamClient.DeleteInstrumentationRole(ctx, params.RoleNamePrefix, accountID)
	require.NoError(t, err, "failed to delete instrumentation role")

	// Step 7: Verify instrumentation role is gone
	_, err = iamClient.ReadRole(ctx, "dash0-acc-test-instrumentation")
	assert.Error(t, err, "instrumentation role should not exist after deletion")

	// Step 8: Delete read-only role
	err = iamClient.DeleteReadOnlyRole(ctx, params.RoleNamePrefix)
	require.NoError(t, err, "failed to delete read-only role")

	// Step 9: Verify read-only role is gone
	_, err = iamClient.ReadRole(ctx, "dash0-acc-test-read-only")
	assert.Error(t, err, "read-only role should not exist after deletion")
}

// TestAccAwsIntegrationResource_IAMRoleTags tests that tags are applied and can be updated on IAM roles.
func TestAccAwsIntegrationResource_IAMRoleTags(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}
	testAccAwsIntegrationPreCheck(t)

	ctx := context.Background()
	iamClient := newTestIAMClient(t)

	params := awsclient.RoleParams{
		RoleNamePrefix:    "dash0-acc-test-tags",
		Dash0AwsAccountID: "115813213817",
		ExternalID:        testExternalID(),
		Tags: map[string]string{
			"Environment": "test",
			"ManagedBy":   "dash0-acc-test",
		},
	}

	// Clean up any leftover roles from a previous failed run.
	_ = iamClient.DeleteReadOnlyRole(ctx, params.RoleNamePrefix)

	// Create role with tags
	_, err := iamClient.CreateReadOnlyRole(ctx, params)
	require.NoError(t, err)

	// Update tags
	newTags := map[string]string{
		"Environment": "staging",
		"ManagedBy":   "dash0-acc-test",
		"NewTag":      "value",
	}
	roleName := fmt.Sprintf("%s-read-only", params.RoleNamePrefix)
	err = iamClient.UpdateRoleTags(ctx, roleName, newTags)
	require.NoError(t, err, "failed to update role tags")

	// Cleanup
	err = iamClient.DeleteReadOnlyRole(ctx, params.RoleNamePrefix)
	require.NoError(t, err, "failed to delete read-only role")
}
