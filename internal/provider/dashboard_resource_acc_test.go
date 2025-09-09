package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/dash0/terraform-provider-dash0/internal/provider/client"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const dashboardResourceName = "dash0_dashboard.test"

const basicDashboardYaml = `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: home
spec:
  duration: 30m
  display:
    description: ""
    name: Home
  layouts: []
  panels: []
  variables: []`

const updatedDashboardYaml = `
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: home
spec:
  duration: 30m
  display:
    description: ""
    name: Home (updated)
  layouts: []
  panels: []
  variables: []`

func TestAccDashboardResource(t *testing.T) {
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
				Config: testAccDashboardResourceConfig("default", basicDashboardYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify the dashboard exists
					testAccCheckDashboardExists(dashboardResourceName),
					// Verify attributes
					resource.TestCheckResourceAttr(dashboardResourceName, "dataset", "default"),
					resource.TestCheckResourceAttr(dashboardResourceName, "dashboard_yaml", basicDashboardYaml),
					resource.TestCheckResourceAttrSet(dashboardResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      dashboardResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both origin and dataset to identify the dashboard
				ImportStateIdFunc: testAccDashboardImportStateIdFunc(dashboardResourceName),
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
					if dataset := states[0].Attributes["dataset"]; dataset != "default" {
						return fmt.Errorf("expected dataset 'default', got '%s'", dataset)
					}

					// Verify the dashboard_yaml attribute
					if yaml := states[0].Attributes["dashboard_yaml"]; yaml == "" {
						return fmt.Errorf("dashboard_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccDashboardResourceConfig("default", updatedDashboardYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDashboardExists(dashboardResourceName),
					resource.TestCheckResourceAttr(dashboardResourceName, "dataset", "default"),
					resource.TestCheckResourceAttr(dashboardResourceName, "dashboard_yaml", updatedDashboardYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccDashboardResourceConfig("terraform-test", updatedDashboardYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDashboardExists(dashboardResourceName),
					resource.TestCheckResourceAttr(dashboardResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(dashboardResourceName, "dashboard_yaml", updatedDashboardYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDashboardDoesNotExists(dashboardResourceName),
				),
			},
		},
	})
}

// Test PreCheck function to validate required environment variables
func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("DASH0_URL"); v == "" {
		t.Fatal("DASH0_URL must be set for acceptance tests")
	}
	if v := os.Getenv("DASH0_AUTH_TOKEN"); v == "" {
		t.Fatal("DASH0_AUTH_TOKEN must be set for acceptance tests")
	}
}

// Configure test provider factories
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"dash0": providerserver.NewProtocol6WithError(New("test")()),
}

// Test configuration for dashboard resource
func testAccDashboardResourceConfig(dataset, dashboardYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_dashboard" "test" {
  dataset = %q
  dashboard_yaml = %q
}
`, dataset, dashboardYaml)
}

// Check that the dashboard exists in the API
func testAccCheckDashboardExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no dashboard ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new c to verify the dashboard exists
		c := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
		)

		// Attempt to retrieve the dashboard
		_, err := c.GetDashboard(context.Background(), dataset, origin)
		if err != nil {
			return fmt.Errorf("Error retrieving dashboard: %s", err)
		}

		return nil
	}
}

// Check that the dashboard exists in the API
func testAccCheckDashboardDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected dashboard state not to exist: %s", resourceName)
		}
		return nil
	}
}

// Function to generate import ID for dashboard resource
func testAccDashboardImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine origin and dataset for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
