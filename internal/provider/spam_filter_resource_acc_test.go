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

const spamFilterResourceName = "dash0_spam_filter.test"

const basicSpamFilterYaml = `apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SpamFilter
metadata:
  name: Drop noisy health checks
  annotations:
    dash0.com/enabled: "true"
spec:
  contexts:
    - log
  filter:
    - key: "k8s.namespace.name"
      operator: "is"
      value: "kube-system"`

const updatedSpamFilterYaml = `apiVersion: operator.dash0.com/v1alpha1
kind: Dash0SpamFilter
metadata:
  name: Drop noisy health checks (updated)
  annotations:
    dash0.com/enabled: "true"
spec:
  contexts:
    - log
  filter:
    - key: "k8s.namespace.name"
      operator: "is"
      value: "kube-system"
    - key: "k8s.pod.name"
      operator: "starts_with"
      value: "health-check-"`

func TestAccSpamFilterResource(t *testing.T) {
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
				Config: testAccSpamFilterResourceConfig("terraform-test", basicSpamFilterYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify the spam filter exists
					testAccCheckSpamFilterExists(spamFilterResourceName),
					// Verify attributes
					resource.TestCheckResourceAttr(spamFilterResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(spamFilterResourceName, "spam_filter_yaml", basicSpamFilterYaml),
					resource.TestCheckResourceAttrSet(spamFilterResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      spamFilterResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both origin and dataset to identify the spam filter
				ImportStateIdFunc: testAccSpamFilterImportStateIdFunc(spamFilterResourceName),
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

					// Verify the spam_filter_yaml attribute
					if yaml := states[0].Attributes["spam_filter_yaml"]; yaml == "" {
						return fmt.Errorf("spam_filter_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccSpamFilterResourceConfig("terraform-test", updatedSpamFilterYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSpamFilterExists(spamFilterResourceName),
					resource.TestCheckResourceAttr(spamFilterResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(spamFilterResourceName, "spam_filter_yaml", updatedSpamFilterYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccSpamFilterResourceConfig("another-dataset", updatedSpamFilterYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSpamFilterExists(spamFilterResourceName),
					resource.TestCheckResourceAttr(spamFilterResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(spamFilterResourceName, "spam_filter_yaml", updatedSpamFilterYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSpamFilterDoesNotExists(spamFilterResourceName),
				),
			},
		},
	})
}

// Test configuration for spam filter resource
func testAccSpamFilterResourceConfig(dataset, spamFilterYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_spam_filter" "test" {
  dataset          = %q
  spam_filter_yaml = %q
}
`, dataset, spamFilterYaml)
}

// Check that the spam filter exists in the API
func testAccCheckSpamFilterExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no spam filter ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new client to verify the spam filter exists
		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
		)
		if err != nil {
			return fmt.Errorf("Error creating client: %s", err)
		}

		// Attempt to retrieve the spam filter
		_, err = c.GetSpamFilter(context.Background(), origin, dataset)
		if err != nil {
			return fmt.Errorf("Error retrieving spam filter: %s", err)
		}

		return nil
	}
}

// Check that the spam filter does not exist
func testAccCheckSpamFilterDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected spam filter state not to exist: %s", resourceName)
		}
		return nil
	}
}

// Function to generate import ID for spam filter resource
func testAccSpamFilterImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine dataset and origin for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
