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

const checkRuleResourceName = "dash0_check_rule.test"

const basicCheckRuleYaml = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: testalerts---testservicedown
spec:
  groups:
    - name: TestAlerts
      interval: 1m0s
      rules:
        - alert: TestServiceDown
          expr: up{job="test-service"} == 0
          for: 5m0s
          annotations:
            summary: 'Test service is down'
            description: 'Test service has been down for more than 5 minutes'
          labels:
            severity: critical`

const updatedCheckRuleYaml = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: testalerts---testservicedown
spec:
  groups:
    - name: TestAlerts
      interval: 1m0s
      rules:
        - alert: TestServiceDown
          expr: up{job="test-service"} == 0
          for: 10m0s
          annotations:
            summary: 'Test service is down (updated)'
            description: 'Test service has been down for more than 10 minutes'
            dash0-enabled: "false"
          labels:
            severity: critical`

func TestAccCheckRuleResource(t *testing.T) {
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
				Config: testAccCheckRuleResourceConfig("terraform-test", basicCheckRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify the check rule exists
					testAccCheckCheckRuleExists(checkRuleResourceName),
					// Verify attributes
					resource.TestCheckResourceAttr(checkRuleResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(checkRuleResourceName, "check_rule_yaml", basicCheckRuleYaml),
					resource.TestCheckResourceAttrSet(checkRuleResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      checkRuleResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both origin and dataset to identify the check rule
				ImportStateIdFunc: testAccCheckRuleImportStateIdFunc(checkRuleResourceName),
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

					// Verify the check_rule_yaml attribute
					if yaml := states[0].Attributes["check_rule_yaml"]; yaml == "" {
						return fmt.Errorf("check_rule_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccCheckRuleResourceConfig("terraform-test", updatedCheckRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleExists(checkRuleResourceName),
					resource.TestCheckResourceAttr(checkRuleResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(checkRuleResourceName, "check_rule_yaml", updatedCheckRuleYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccCheckRuleResourceConfig("another-dataset", updatedCheckRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleExists(checkRuleResourceName),
					resource.TestCheckResourceAttr(checkRuleResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(checkRuleResourceName, "check_rule_yaml", updatedCheckRuleYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleDoesNotExists(checkRuleResourceName),
				),
			},
		},
	})
}

// Test YAML with explicit zero threshold annotations
// This tests that users can include zero thresholds and the plan will be idempotent
const checkRuleYamlWithZeroThresholds = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: testalerts---testservicedown
spec:
  groups:
    - name: TestAlerts
      interval: 1m0s
      rules:
        - alert: TestServiceDown
          expr: up{job="test-service"} == 0
          for: 5m0s
          annotations:
            dash0-threshold-critical: "0"
            dash0-threshold-degraded: "0"
            summary: 'Test service is down'
            description: 'Test service has been down for more than 5 minutes'
          labels:
            severity: critical`

// Test YAML with non-zero threshold annotations
const checkRuleYamlWithNonZeroThresholds = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: testalerts---testservicedown
spec:
  groups:
    - name: TestAlerts
      interval: 1m0s
      rules:
        - alert: TestServiceDown
          expr: up{job="test-service"} == 0
          for: 5m0s
          annotations:
            dash0-threshold-critical: "50"
            dash0-threshold-degraded: "30"
            summary: 'Test service is down'
            description: 'Test service has been down for more than 5 minutes'
          labels:
            severity: critical`

// TestAccCheckRuleResource_WithZeroThresholds verifies that configs with explicit
// zero threshold annotations are idempotent (no plan diff after apply)
func TestAccCheckRuleResource_WithZeroThresholds(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with zero thresholds
			{
				Config: testAccCheckRuleResourceConfig("terraform-test", checkRuleYamlWithZeroThresholds),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleExists(checkRuleResourceName),
					resource.TestCheckResourceAttr(checkRuleResourceName, "dataset", "terraform-test"),
				),
			},
			// Verify idempotency - re-apply same config should show no changes
			{
				Config:   testAccCheckRuleResourceConfig("terraform-test", checkRuleYamlWithZeroThresholds),
				PlanOnly: true,
			},
			// Cleanup
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleDoesNotExists(checkRuleResourceName),
				),
			},
		},
	})
}

// TestAccCheckRuleResource_WithNonZeroThresholds verifies that configs with non-zero
// threshold annotations are idempotent (no plan diff after apply)
func TestAccCheckRuleResource_WithNonZeroThresholds(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with non-zero thresholds
			{
				Config: testAccCheckRuleResourceConfig("terraform-test", checkRuleYamlWithNonZeroThresholds),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleExists(checkRuleResourceName),
					resource.TestCheckResourceAttr(checkRuleResourceName, "dataset", "terraform-test"),
				),
			},
			// Verify idempotency - re-apply same config should show no changes
			{
				Config:   testAccCheckRuleResourceConfig("terraform-test", checkRuleYamlWithNonZeroThresholds),
				PlanOnly: true,
			},
			// Cleanup
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCheckRuleDoesNotExists(checkRuleResourceName),
				),
			},
		},
	})
}

// Test configuration for check rule resource
func testAccCheckRuleResourceConfig(dataset, checkRuleYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_check_rule" "test" {
  dataset = %q
  check_rule_yaml = %q
}
`, dataset, checkRuleYaml)
}

// Check that the check rule exists in the API
func testAccCheckCheckRuleExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no check rule ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new client to verify the check rule exists
		c := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
		)

		// Attempt to retrieve the check rule
		_, err := c.GetCheckRule(context.Background(), dataset, origin)
		if err != nil {
			return fmt.Errorf("Error retrieving check rule: %s", err)
		}

		return nil
	}
}

// Check that the check rule does not exist
func testAccCheckCheckRuleDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected check rule state not to exist: %s", resourceName)
		}
		return nil
	}
}

// Function to generate import ID for check rule resource
func testAccCheckRuleImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine dataset and origin for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
