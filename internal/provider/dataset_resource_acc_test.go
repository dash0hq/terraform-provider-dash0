package provider

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const datasetResourceName = "dash0_dataset.test"

// Destroying a dataset soft-deletes it for 30 days, so every run uses a unique slug.
func testAccDatasetYaml(slug, name string) string {
	return fmt.Sprintf(`apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: %s
  labels:
    dash0.com/id: %s
spec:
  retention:
    traces: 30d`, name, slug)
}

func TestAccDatasetResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	slug := fmt.Sprintf("tf-acc-ds-%d-%d", time.Now().Unix(), rand.IntN(100000))
	basicDatasetYaml := testAccDatasetYaml(slug, "Terraform Acceptance")
	updatedDatasetYaml := testAccDatasetYaml(slug, "Terraform Acceptance Updated")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatasetDestroyed(slug),
		Steps: []resource.TestStep{
			{
				Config: testAccDatasetResourceConfig(basicDatasetYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatasetExists(datasetResourceName),
					resource.TestCheckResourceAttr(datasetResourceName, "id", slug),
					resource.TestCheckResourceAttrSet(datasetResourceName, "origin"),
					resource.TestCheckResourceAttr(datasetResourceName, "dataset_yaml", basicDatasetYaml),
				),
			},
			// Import by slug.
			{
				ResourceName:      datasetResourceName,
				ImportState:       true,
				ImportStateId:     slug,
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

// testAccCheckDatasetDestroyed proves the server answers 404 for the slug.
func testAccCheckDatasetDestroyed(slug string) resource.TestCheckFunc {
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
			return fmt.Errorf("Error creating client: %s", err)
		}
		_, err = c.GetDataset(context.Background(), slug)
		if err == nil {
			return fmt.Errorf("dataset %s still exists after destroy", slug)
		}
		if !dash0.IsNotFound(err) {
			return fmt.Errorf("expected 404 for dataset %s, got: %w", slug, err)
		}
		return nil
	}
}

// TestAccDatasetResource_Permissions covers spec.permissions: added to an
// existing dataset, order-insensitive, a permissions-only change, drift made
// outside Terraform, and the write grant rejected at plan time.
func TestAccDatasetResource_Permissions(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	slug := fmt.Sprintf("tf-acc-dp-%d-%d", time.Now().Unix(), rand.IntN(100000))
	withPermissions := func(actions string) string {
		return testAccDatasetYaml(slug, "Terraform Acceptance Permissions") +
			"\n  permissions:\n    - role: basic_member\n      actions: " + actions
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatasetDestroyed(slug),
		Steps: []resource.TestStep{
			// First, so the post-test destroy runs with a valid config.
			{
				Config:      testAccDatasetResourceConfig(withPermissions("[dataset:read, dataset:write]")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`grants dataset:write`),
			},
			{
				Config: testAccDatasetResourceConfig(testAccDatasetYaml(slug, "Terraform Acceptance Permissions")),
			},
			// Adding permissions to an existing dataset.
			{
				Config: testAccDatasetResourceConfig(withPermissions("[dataset:read, dataset:createCheckRule]")),
				Check:  testAccCheckDatasetPermissions(slug, `"dataset:createCheckRule"`),
			},
			{
				Config:   testAccDatasetResourceConfig(withPermissions("[dataset:createCheckRule, dataset:read]")),
				PlanOnly: true,
			},
			// Permissions-only change.
			{
				Config: testAccDatasetResourceConfig(withPermissions("[dataset:read, dataset:createSLO]")),
				Check:  testAccCheckDatasetPermissions(slug, `"dataset:createSLO"`),
			},
			// Change made outside Terraform shows as drift.
			{
				PreConfig: func() {
					body, err := converter.ConvertYAMLToJSON(withPermissions("[dataset:read]"))
					if err != nil {
						t.Fatalf("converting YAML: %s", err)
					}
					c, err := newSLOAccClient()
					if err == nil {
						err = c.UpdateDataset(context.Background(), slug, "", body)
					}
					if err != nil {
						t.Fatalf("out-of-band update failed: %s", err)
					}
				},
				Config:             testAccDatasetResourceConfig(withPermissions("[dataset:read, dataset:createSLO]")),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// testAccCheckDatasetPermissions checks that the dataset contains want. Only
// spec.permissions can: GetDataset strips spec.permittedActions.
func testAccCheckDatasetPermissions(slug, want string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		c, err := newSLOAccClient()
		if err != nil {
			return err
		}
		got, err := c.GetDataset(context.Background(), slug)
		if err != nil {
			return fmt.Errorf("Error retrieving dataset: %s", err)
		}
		if !strings.Contains(got, want) {
			return fmt.Errorf("dataset %s does not contain %s: %s", slug, want, got)
		}
		return nil
	}
}

// TestAccDatasetResource_DerivedSlug creates a dataset without a slug: the
// server derives it from the name, and a later name change updates in place.
func TestAccDatasetResource_DerivedSlug(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	// The derived slug is at most 26 characters.
	suffix := fmt.Sprintf("%d %d", time.Now().Unix()%1000000, rand.IntN(100000))
	slug := "tf-acc-" + strings.ReplaceAll(suffix, " ", "-")
	withName := func(name string) string {
		return fmt.Sprintf("apiVersion: dash0.com/v1alpha1\nkind: Dash0Dataset\nmetadata:\n  name: %s", name)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatasetDestroyed(slug),
		Steps: []resource.TestStep{
			{
				Config: testAccDatasetResourceConfig(withName("TF Acc " + suffix)),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatasetExists(datasetResourceName),
					resource.TestCheckResourceAttr(datasetResourceName, "id", slug),
				),
			},
			{
				Config: testAccDatasetResourceConfig(withName("TF Acc Renamed " + suffix)),
				Check:  resource.TestCheckResourceAttr(datasetResourceName, "id", slug),
			},
			{
				Config:   testAccDatasetResourceConfig(withName("TF Acc Renamed " + suffix)),
				PlanOnly: true,
			},
		},
	})
}
