package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const recordingRuleGroupResourceName = "dash0_recording_rule_group.test"

const basicRecordingRuleGroupYaml = `
kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: true
  display:
    name: HTTP Metrics
  interval: 1m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])
      labels:
        env: production
`

const updatedRecordingRuleGroupYaml = `
kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: true
  display:
    name: HTTP Metrics Updated
  interval: 2m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])
      labels:
        env: production
    - record: http_errors_total:rate5m
      expression: rate(http_requests_total{status=~"5.."}[5m])
      labels:
        env: production
`

const updatedDatasetRecordingRuleGroupYaml = `
kind: Dash0RecordingRuleGroup
metadata:
  name: http_metrics
spec:
  enabled: true
  display:
    name: HTTP Metrics Updated
  interval: 2m
  rules:
    - record: http_requests_total:rate5m
      expression: rate(http_requests_total[5m])
      labels:
        env: production
    - record: http_errors_total:rate5m
      expression: rate(http_requests_total{status=~"5.."}[5m])
      labels:
        env: production
`

func TestAccRecordingRuleGroupResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccRecordingRuleGroupResourceConfig("terraform-test", basicRecordingRuleGroupYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleGroupExists(recordingRuleGroupResourceName),
					resource.TestCheckResourceAttr(recordingRuleGroupResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttrSet(recordingRuleGroupResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      recordingRuleGroupResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccRecordingRuleGroupImportStateIdFunc(recordingRuleGroupResourceName),
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}

					if origin := states[0].Attributes["origin"]; origin == "" {
						return fmt.Errorf("origin attribute is missing or empty")
					}

					if dataset := states[0].Attributes["dataset"]; dataset != "terraform-test" {
						return fmt.Errorf("expected dataset 'terraform-test', got '%s'", dataset)
					}

					if yaml := states[0].Attributes["recording_rule_group_yaml"]; yaml == "" {
						return fmt.Errorf("recording_rule_group_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccRecordingRuleGroupResourceConfig("terraform-test", updatedRecordingRuleGroupYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleGroupExists(recordingRuleGroupResourceName),
					resource.TestCheckResourceAttr(recordingRuleGroupResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(recordingRuleGroupResourceName, "recording_rule_group_yaml", updatedRecordingRuleGroupYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccRecordingRuleGroupResourceConfig("another-dataset", updatedDatasetRecordingRuleGroupYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleGroupExists(recordingRuleGroupResourceName),
					resource.TestCheckResourceAttr(recordingRuleGroupResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(recordingRuleGroupResourceName, "recording_rule_group_yaml", updatedDatasetRecordingRuleGroupYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleGroupDoesNotExist(recordingRuleGroupResourceName),
				),
			},
		},
	})
}

func testAccCheckRecordingRuleGroupExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no recording rule group ID is set")
		}

		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		c := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
		)

		_, err := c.GetRecordingRuleGroup(context.Background(), dataset, origin)
		if err != nil {
			return fmt.Errorf("Error retrieving recording rule group: %s", err)
		}

		return nil
	}
}

func testAccCheckRecordingRuleGroupDoesNotExist(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected recording rule group state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccRecordingRuleGroupResourceConfig(dataset string, yamlContent string) string {
	return fmt.Sprintf(`
resource "dash0_recording_rule_group" "test" {
  dataset                   = %[1]q
  recording_rule_group_yaml = %q
}
`, dataset, yamlContent)
}

func testAccRecordingRuleGroupImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
