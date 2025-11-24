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

const syntheticCheckResourceName = "dash0_synthetic_check.test"

const basicSyntheticCheckYaml = `
kind: Dash0SyntheticCheck
metadata:
  name: test-check
spec:
  enabled: true
  notifications:
    channels: []
  plugin:
    display:
      name: test.example.com
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
          - kind: timing
            spec:
              type: response
              value: 5000ms
              operator: lte
        degradedAssertions:
          - kind: timing
            spec:
              type: response
              value: 2000ms
              operator: lte
      request:
        method: get
        url: https://test.example.com
        queryParameters: []
        headers: []
        redirects: follow
        tls:
          allowInsecure: false
        tracing:
          addTracingHeaders: true
  retries:
    kind: fixed
    spec:
      attempts: 3
      delay: 1s
  schedule:
    interval: 5m
    locations:
      - gcp-us-west1
    strategy: all_locations`

const updatedSyntheticCheckYaml = `
kind: Dash0SyntheticCheck
metadata:
  name: test-check
spec:
  enabled: true
  notifications:
    channels: []
  plugin:
    display:
      name: test.example.com
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
          - kind: timing
            spec:
              type: response
              value: 5000ms
              operator: lte
        degradedAssertions:
          - kind: timing
            spec:
              type: response
              value: 2000ms
              operator: lte
      request:
        method: post
        url: https://example.com
        queryParameters: []
        headers: []
        redirects: follow
        tls:
          allowInsecure: false
        tracing:
          addTracingHeaders: true
  retries:
    kind: fixed
    spec:
      attempts: 3
      delay: 1s
  schedule:
    interval: 5m
    locations:
      - gcp-us-west1
    strategy: all_locations`

func TestAccSyntheticCheckResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccSyntheticCheckResourceConfig("terraform-test", basicSyntheticCheckYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSyntheticCheckExists(syntheticCheckResourceName),

					resource.TestCheckResourceAttr(syntheticCheckResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(syntheticCheckResourceName, "synthetic_check_yaml", basicSyntheticCheckYaml),
					resource.TestCheckResourceAttrSet(syntheticCheckResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      syntheticCheckResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both origin and dataset to identify the dashboard
				ImportStateIdFunc: testAccSyntheticCheckImportStateIdFunc(syntheticCheckResourceName),
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

					// Verify the synthetic_check_yaml attribute
					if yaml := states[0].Attributes["synthetic_check_yaml"]; yaml == "" {
						return fmt.Errorf("synthetic_check_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccSyntheticCheckResourceConfig("terraform-test", updatedSyntheticCheckYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSyntheticCheckExists(syntheticCheckResourceName),
					resource.TestCheckResourceAttr(syntheticCheckResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(syntheticCheckResourceName, "synthetic_check_yaml", updatedSyntheticCheckYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccSyntheticCheckResourceConfig("another-dataset", updatedSyntheticCheckYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSyntheticCheckExists(syntheticCheckResourceName),
					resource.TestCheckResourceAttr(syntheticCheckResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(syntheticCheckResourceName, "synthetic_check_yaml", updatedSyntheticCheckYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSyntheticCheckDoesNotExists(syntheticCheckResourceName),
				),
			},
		},
	})
}

// Check that the synthetic check exists in the API
func testAccCheckSyntheticCheckExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no synthetic check ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new client to verify the synthetic check exists
		client := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
		)

		// Attempt to retrieve the synthetic check
		_, err := client.GetSyntheticCheck(context.Background(), dataset, origin)
		if err != nil {
			return fmt.Errorf("Error retrieving synthetic check: %s", err)
		}

		return nil
	}
}

// Check that the synthetic check exists in the API
func testAccCheckSyntheticCheckDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected synthetic check state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccSyntheticCheckResourceConfig(dataset string, syntheticCheckYaml string) string {
	return fmt.Sprintf(`
resource "dash0_synthetic_check" "test" {
  dataset = %[1]q
  synthetic_check_yaml = %q
}
`, dataset, syntheticCheckYaml)
}

// Function to generate import ID for synthetic check resource
func testAccSyntheticCheckImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine origin and dataset for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
