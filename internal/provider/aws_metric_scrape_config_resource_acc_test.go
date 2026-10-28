package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const awsMetricScrapeConfigResourceName = "dash0_aws_metric_scrape_config.test"

const basicAwsMetricScrapeConfigYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0AwsMetricScrapeConfig
spec:
  namespaces:
    - namespace: AWS/Lambda
      metrics:
        - Invocations
        - Errors`

const updatedAwsMetricScrapeConfigYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0AwsMetricScrapeConfig
spec:
  namespaces:
    - namespace: AWS/Lambda
      metrics:
        - Invocations
        - Errors
    - namespace: AWS/SQS
      metrics:
        - NumberOfMessagesSent`

// TestAccAwsMetricScrapeConfigResource overwrites the scrape config of the
// AWS integration in DASH0_AWS_INTEGRATION_ID and clears it at the end, so
// point it at a test integration only.
func TestAccAwsMetricScrapeConfigResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}
	integrationID := os.Getenv("DASH0_AWS_INTEGRATION_ID")
	if integrationID == "" {
		t.Skip("DASH0_AWS_INTEGRATION_ID must be set to the id of a test AWS integration")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckAwsMetricScrapeConfigCleared(integrationID),
		Steps: []resource.TestStep{
			{
				Config: testAccAwsMetricScrapeConfigResourceConfig(integrationID, basicAwsMetricScrapeConfigYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(awsMetricScrapeConfigResourceName, "integration_id", integrationID),
					resource.TestCheckResourceAttr(awsMetricScrapeConfigResourceName, "aws_metric_scrape_config_yaml", basicAwsMetricScrapeConfigYaml),
					resource.TestCheckResourceAttrSet(awsMetricScrapeConfigResourceName, "origin"),
				),
			},
			{
				ResourceName:                         awsMetricScrapeConfigResourceName,
				ImportState:                          true,
				ImportStateId:                        integrationID,
				ImportStateVerifyIdentifierAttribute: "integration_id",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					if got := states[0].Attributes["integration_id"]; got != integrationID {
						return fmt.Errorf("expected integration_id %q, got %q", integrationID, got)
					}
					return nil
				},
			},
			{
				Config: testAccAwsMetricScrapeConfigResourceConfig(integrationID, updatedAwsMetricScrapeConfigYaml),
				Check:  resource.TestCheckResourceAttr(awsMetricScrapeConfigResourceName, "aws_metric_scrape_config_yaml", updatedAwsMetricScrapeConfigYaml),
			},
		},
	})
}

func testAccAwsMetricScrapeConfigResourceConfig(integrationID, cfgYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_aws_metric_scrape_config" "test" {
  integration_id                = %q
  aws_metric_scrape_config_yaml = %q
}
`, integrationID, cfgYaml)
}

func testAccCheckAwsMetricScrapeConfigCleared(integrationID string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			dash0.StaticAuthTokenProvider(os.Getenv("DASH0_AUTH_TOKEN")),
			false,
			"test",
			3,
			"",
		)
		if err != nil {
			return fmt.Errorf("error creating client: %w", err)
		}
		cfgJSON, origin, err := c.GetAwsMetricScrapeConfig(context.Background(), integrationID)
		if err != nil {
			return fmt.Errorf("error retrieving AWS metric scrape config: %w", err)
		}
		if origin != "" || hasAwsScrapeNamespaces(cfgJSON) {
			return fmt.Errorf("expected the scrape config to be cleared, got origin %q and %s", origin, cfgJSON)
		}
		return nil
	}
}
