package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const sloResourceName = "dash0_slo.test"

// basicSLOAccYaml is an OpenSLO v1 document within the supported subset
// (single objective, inline ratioMetric, Occurrences budgeting, rolling 28d).
const basicSLOAccYaml = `apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/display-name: Checkout availability
    dash0.com/enabled: "true"
spec:
  description: 99 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99% availability
      target: 0.99`

const updatedSLOAccYaml = `apiVersion: openslo/v1
kind: SLO
metadata:
  name: checkout-availability
  annotations:
    dash0.com/display-name: Checkout availability
    dash0.com/enabled: "true"
spec:
  description: 99.5 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99.5% availability
      target: 0.995`

func TestAccSLOResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccSLOResourceConfig("terraform-test", basicSLOAccYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSLOExists(sloResourceName),

					resource.TestCheckResourceAttr(sloResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(sloResourceName, "slo_yaml", basicSLOAccYaml),
					resource.TestCheckResourceAttrSet(sloResourceName, "origin"),
					// Verify the computed URL is set and points at the web app deep link
					resource.TestMatchResourceAttr(sloResourceName, "url",
						regexp.MustCompile(`^https://app\..+/goto/.+`)),
				),
			},
			// ImportState testing
			{
				ResourceName:      sloResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both dataset and origin to identify the SLO
				ImportStateIdFunc: testAccSLOImportStateIdFunc(sloResourceName),
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

					// Verify the slo_yaml attribute
					if yaml := states[0].Attributes["slo_yaml"]; yaml == "" {
						return fmt.Errorf("slo_yaml attribute is missing or empty")
					}

					// Verify the computed url is resolved on import
					urlPattern := regexp.MustCompile(`^https://app\..+/goto/.+`)
					if u := states[0].Attributes["url"]; !urlPattern.MatchString(u) {
						return fmt.Errorf("url attribute %q does not match expected SLO deep link pattern", u)
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccSLOResourceConfig("terraform-test", updatedSLOAccYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSLOExists(sloResourceName),
					resource.TestCheckResourceAttr(sloResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(sloResourceName, "slo_yaml", updatedSLOAccYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccSLOResourceConfig("another-dataset", updatedSLOAccYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSLOExists(sloResourceName),
					resource.TestCheckResourceAttr(sloResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(sloResourceName, "slo_yaml", updatedSLOAccYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSLODoesNotExists(sloResourceName),
				),
			},
		},
	})
}

// Check that the SLO exists in the API
func testAccCheckSLOExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no SLO ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new client to verify the SLO exists
		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
			3,
		)
		if err != nil {
			return fmt.Errorf("Error creating client: %s", err)
		}

		// Attempt to retrieve the SLO
		_, err = c.GetSLO(context.Background(), origin, dataset)
		if err != nil {
			return fmt.Errorf("Error retrieving SLO: %s", err)
		}

		return nil
	}
}

// Check that the SLO does not exist in the state
func testAccCheckSLODoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected SLO state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccSLOResourceConfig(dataset string, sloYaml string) string {
	return fmt.Sprintf(`
resource "dash0_slo" "test" {
  dataset  = %[1]q
  slo_yaml = %q
}
`, dataset, sloYaml)
}

// Function to generate import ID for SLO resource
func testAccSLOImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine dataset and origin for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
