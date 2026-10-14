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

const datasetResourceName = "dash0_dataset.test"

// Destroying a dataset soft-deletes it for 30 days, and re-creating the same
// slug restores it, so reruns with the same slug are safe.
const basicDatasetYaml = `kind: Dash0Dataset
metadata:
  name: Terraform Acceptance
  labels:
    dash0.com/id: tf-acc-dataset
spec:
  tracing:
    retention: 30d`

const updatedDatasetYaml = `kind: Dash0Dataset
metadata:
  name: Terraform Acceptance Updated
  labels:
    dash0.com/id: tf-acc-dataset
spec:
  tracing:
    retention: 30d`

func TestAccDatasetResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDatasetResourceConfig(basicDatasetYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatasetExists(datasetResourceName),
					resource.TestCheckResourceAttr(datasetResourceName, "id", "tf-acc-dataset"),
					resource.TestCheckResourceAttrSet(datasetResourceName, "origin"),
					resource.TestCheckResourceAttr(datasetResourceName, "dataset_yaml", basicDatasetYaml),
				),
			},
			// Import by slug.
			{
				ResourceName:      datasetResourceName,
				ImportState:       true,
				ImportStateId:     "tf-acc-dataset",
				ImportStateVerify: false,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					if states[0].Attributes["dataset_yaml"] == "" {
						return fmt.Errorf("dataset_yaml attribute is missing or empty")
					}
					return nil
				},
			},
			{
				Config: testAccDatasetResourceConfig(updatedDatasetYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatasetExists(datasetResourceName),
					resource.TestCheckResourceAttr(datasetResourceName, "dataset_yaml", updatedDatasetYaml),
				),
			},
			// Idempotency: server-defaulted spec fields cause no diff.
			{
				Config:   testAccDatasetResourceConfig(updatedDatasetYaml),
				PlanOnly: true,
			},
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatasetDoesNotExist(datasetResourceName),
				),
			},
		},
	})
}

func testAccDatasetResourceConfig(datasetYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_dataset" "test" {
  dataset_yaml = %q
}
`, datasetYaml)
}

func testAccCheckDatasetExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		slug := rs.Primary.Attributes["id"]
		if slug == "" {
			return fmt.Errorf("no dataset slug is set")
		}

		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			dash0.StaticAuthTokenProvider(os.Getenv("DASH0_AUTH_TOKEN")),
			false,
			"test",
			3,
			"",
		)
		if err != nil {
			return fmt.Errorf("Error creating client: %s", err)
		}
		if _, err := c.GetDataset(context.Background(), slug); err != nil {
			return fmt.Errorf("Error retrieving dataset: %s", err)
		}
		return nil
	}
}

func testAccCheckDatasetDoesNotExist(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if _, ok := s.RootModule().Resources[resourceName]; ok {
			return fmt.Errorf("expected dataset state not to exist: %s", resourceName)
		}
		return nil
	}
}
