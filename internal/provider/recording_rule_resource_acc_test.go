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

const recordingRuleResourceName = "dash0_recording_rule.test"

const basicRecordingRuleYaml = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-recording-rules
spec:
  groups:
    - name: TestRecordingGroup
      interval: 1m0s
      rules:
        - record: job:http_requests_total:rate5m
          expr: sum by (job) (rate(http_requests_total[5m]))
          labels:
            env: test`

const updatedRecordingRuleYaml = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: test-recording-rules
spec:
  groups:
    - name: TestRecordingGroup
      interval: 1m0s
      rules:
        - record: job:http_requests_total:rate10m
          expr: sum by (job) (rate(http_requests_total[10m]))
          labels:
            env: staging`

func TestAccRecordingRuleResource(t *testing.T) {
	// Skip if TF_ACC is not set to "1"
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccRecordingRuleResourceConfig("terraform-test", basicRecordingRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify the recording rule exists
					testAccCheckRecordingRuleExists(recordingRuleResourceName),
					// Verify attributes
					resource.TestCheckResourceAttr(recordingRuleResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(recordingRuleResourceName, "recording_rule_yaml", basicRecordingRuleYaml),
					resource.TestCheckResourceAttrSet(recordingRuleResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      recordingRuleResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both origin and dataset to identify the recording rule
				ImportStateIdFunc: testAccRecordingRuleImportStateIdFunc(recordingRuleResourceName),
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					// Verify we have exactly one state
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}

					// Verify the origin attribute
					if origin := states[0].Attributes["origin"]; origin == "" {
						return fmt.Errorf("origin attribute is missing or empty")
					}

					// Verify the dataset attribute
					if dataset := states[0].Attributes["dataset"]; dataset != "terraform-test" {
						return fmt.Errorf("expected dataset 'terraform-test', got '%s'", dataset)
					}

					// Verify the recording_rule_yaml attribute
					if yaml := states[0].Attributes["recording_rule_yaml"]; yaml == "" {
						return fmt.Errorf("recording_rule_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccRecordingRuleResourceConfig("terraform-test", updatedRecordingRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleExists(recordingRuleResourceName),
					resource.TestCheckResourceAttr(recordingRuleResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(recordingRuleResourceName, "recording_rule_yaml", updatedRecordingRuleYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccRecordingRuleResourceConfig("another-dataset", updatedRecordingRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleExists(recordingRuleResourceName),
					resource.TestCheckResourceAttr(recordingRuleResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(recordingRuleResourceName, "recording_rule_yaml", updatedRecordingRuleYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckRecordingRuleDoesNotExists(recordingRuleResourceName),
				),
			},
		},
	})
}

// Test configuration for recording rule resource
func testAccRecordingRuleResourceConfig(dataset, recordingRuleYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_recording_rule" "test" {
  dataset = %q
  recording_rule_yaml = %q
}
`, dataset, recordingRuleYaml)
}

// Check that the recording rule exists in the API
func testAccCheckRecordingRuleExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no recording rule ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new client to verify the recording rule exists
		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
		)
		if err != nil {
			return fmt.Errorf("Error creating client: %s", err)
		}

		// Attempt to retrieve the recording rule
		_, err = c.GetRecordingRule(context.Background(), origin, dataset)
		if err != nil {
			return fmt.Errorf("Error retrieving recording rule: %s", err)
		}

		return nil
	}
}

// Check that the recording rule does not exist
func testAccCheckRecordingRuleDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected recording rule state not to exist: %s", resourceName)
		}
		return nil
	}
}

// Function to generate import ID for recording rule resource
func testAccRecordingRuleImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine dataset and origin for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
