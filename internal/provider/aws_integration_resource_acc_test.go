package provider

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const awsIntegrationResourceName = "dash0_aws_integration.test"

// TestAccAwsIntegrationResource exercises the full Create → Read → Update → Delete
// lifecycle of the dash0_aws_integration resource against a real Dash0 API.
// The role ARNs are synthetic strings — the Dash0 API validates them syntactically
// but does not assume them during the test.
func TestAccAwsIntegrationResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	// Randomize the AWS account ID per test run so collisions with leftover
	// integrations from previous runs (the Dash0 API enforces one integration
	// per AWS account ID within an org) don't cause false negatives.
	awsAccountID := fmt.Sprintf("%012d", rand.Int63n(1_000_000_000_000))

	var (
		dataset        = "default"
		externalID     = "dash0-acc-test-external-id"
		readOnlyArn    = fmt.Sprintf("arn:aws:iam::%s:role/dash0-acc-test-read-only", awsAccountID)
		instrArn       = fmt.Sprintf("arn:aws:iam::%s:role/dash0-acc-test-instrumentation", awsAccountID)
		newReadOnlyArn = fmt.Sprintf("arn:aws:iam::%s:role/dash0-acc-test-read-only-v2", awsAccountID)
	)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Create — read-only only
			{
				Config: testAccAwsIntegrationConfig(dataset, externalID, awsAccountID, readOnlyArn, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "dataset", dataset),
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "external_id", externalID),
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "aws_account_id", awsAccountID),
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "read_only_role_arn", readOnlyArn),
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "id", fmt.Sprintf("%s-%s", awsAccountID, externalID)),
					resource.TestCheckNoResourceAttr(awsIntegrationResourceName, "instrumentation_role_arn"),
				),
			},
			// Step 2: Update — add instrumentation role
			{
				Config: testAccAwsIntegrationConfig(dataset, externalID, awsAccountID, readOnlyArn, instrArn),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "read_only_role_arn", readOnlyArn),
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "instrumentation_role_arn", instrArn),
				),
			},
			// Step 3: Update — change read-only ARN (in-place update, no replacement)
			{
				Config: testAccAwsIntegrationConfig(dataset, externalID, awsAccountID, newReadOnlyArn, instrArn),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "read_only_role_arn", newReadOnlyArn),
				),
			},
			// Step 4: Update — remove instrumentation role
			{
				Config: testAccAwsIntegrationConfig(dataset, externalID, awsAccountID, newReadOnlyArn, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(awsIntegrationResourceName, "read_only_role_arn", newReadOnlyArn),
					resource.TestCheckNoResourceAttr(awsIntegrationResourceName, "instrumentation_role_arn"),
				),
			},
			// Step 5: Import
			{
				ResourceName:      awsIntegrationResourceName,
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s,%s,%s", dataset, awsAccountID, externalID),
				ImportStateVerify: true,
			},
		},
	})
}

func testAccAwsIntegrationConfig(dataset, externalID, awsAccountID, readOnlyArn, instrArn string) string {
	instrLine := ""
	if instrArn != "" {
		instrLine = fmt.Sprintf(`  instrumentation_role_arn = %q`, instrArn)
	}
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_aws_integration" "test" {
  dataset            = %q
  external_id        = %q
  aws_account_id     = %q
  read_only_role_arn = %q
%s
}
`, dataset, externalID, awsAccountID, readOnlyArn, instrLine)
}
