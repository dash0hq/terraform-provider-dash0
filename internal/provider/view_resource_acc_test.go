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

const viewResourceName = "dash0_view.test"

const basicViewYaml = `
kind: Dash0View
metadata:
  name: sync-jobs
  labels: 
    "dash0.com/dataset": "terraform-test"
spec:
  display:
    name: Sync Jobs
    folder: []
  type: spans
  permissions:
    - actions: 
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
  filter:
  - key: dash0.span.name
    operator: is
    value: sync
  table:
    columns:
    - colSize: minmax(auto, 2fr)
      key: dash0.span.name
      label: Name
    - colSize: min-content
      key: service.name
      label: Service
    - colSize: min-content
      key: otel.span.start_time
      label: Start Time
    - colSize: 8.5rem
      key: otel.span.duration
      label: Duration
    - colSize: 6rem
      key: otel.parent.id
      label: Root
    - colSize: 8rem
      key: dash0.span.type
      label: Type
    - colSize: 7rem
      key: otel.span.kind
      label: Kind
    - colSize: minmax(5rem, max-content)
      key: dash0.view.builtin.span_events
      label: Span events
    - colSize: minmax(5rem, max-content)
      key: dash0.view.builtin.span_links
      label: Links
    sort:
    - direction: ascending
      key: otel.span.duration
`
const updatedViewYaml = `
kind: Dash0View
metadata:
  name: sync-jobs
  labels: 
    "dash0.com/dataset": "terraform-test"
spec:
  display:
    name: Sync Jobs
    folder: []
  type: spans
  permissions:
    - actions: 
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
  filter:
  - key: dash0.span.name
    operator: is
    value: sync
  table:
    columns:
    - colSize: minmax(auto, 2fr)
      key: dash0.span.name
      label: Name
    - colSize: min-content
      key: service.name
      label: Service
    - colSize: min-content
      key: otel.span.start_time
      label: Start Time
    - colSize: 8.5rem
      key: otel.span.duration
      label: Duration
    - colSize: 6rem
      key: otel.parent.id
      label: Root
    - colSize: 8rem
      key: dash0.span.type
      label: Type
    - colSize: 7rem
      key: otel.span.kind
      label: Kind
    - colSize: minmax(5rem, max-content)
      key: dash0.view.builtin.span_events
      label: Span events
    sort:
    - direction: ascending
      key: otel.span.duration
`

const updatedDatasetViewYaml = `
kind: Dash0View
metadata:
  name: sync-jobs
  labels: 
    "dash0.com/dataset": "another-dataset"
spec:
  display:
    name: Sync Jobs
    folder: []
  type: spans
  permissions:
    - actions: 
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
  filter:
  - key: dash0.span.name
    operator: is
    value: sync
  table:
    columns:
    - colSize: minmax(auto, 2fr)
      key: dash0.span.name
      label: Name
    - colSize: min-content
      key: service.name
      label: Service
    - colSize: min-content
      key: otel.span.start_time
      label: Start Time
    - colSize: 8.5rem
      key: otel.span.duration
      label: Duration
    - colSize: 6rem
      key: otel.parent.id
      label: Root
    - colSize: 8rem
      key: dash0.span.type
      label: Type
    - colSize: 7rem
      key: otel.span.kind
      label: Kind
    - colSize: minmax(5rem, max-content)
      key: dash0.view.builtin.span_events
      label: Span events
    sort:
    - direction: ascending
      key: otel.span.duration
`

func TestAccViewResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccViewResourceConfig("terraform-test", basicViewYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckViewExists(viewResourceName),

					resource.TestCheckResourceAttr(viewResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttrSet(viewResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      viewResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// The import uses both origin and dataset to identify the dashboard
				ImportStateIdFunc: testAccViewImportStateIdFunc(viewResourceName),
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

					// Verify the view_yaml attribute
					if yaml := states[0].Attributes["view_yaml"]; yaml == "" {
						return fmt.Errorf("view_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccViewResourceConfig("terraform-test", updatedViewYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckViewExists(viewResourceName),
					resource.TestCheckResourceAttr(viewResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(viewResourceName, "view_yaml", updatedViewYaml),
				),
			},
			// Test changing dataset (should force recreation)
			{
				Config: testAccViewResourceConfig("another-dataset", updatedDatasetViewYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckViewExists(viewResourceName),
					resource.TestCheckResourceAttr(viewResourceName, "dataset", "another-dataset"),
					resource.TestCheckResourceAttr(viewResourceName, "view_yaml", updatedDatasetViewYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckViewDoesNotExists(viewResourceName),
				),
			},
		},
	})
}

// Check that the view exists in the API
func testAccCheckViewExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no view ID is set")
		}

		// Extract origin and dataset from state
		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		// Create a new client to verify the view exists
		client := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
		)

		// Attempt to retrieve the view
		_, err := client.GetView(context.Background(), dataset, origin)
		if err != nil {
			return fmt.Errorf("Error retrieving view: %s", err)
		}

		return nil
	}
}

// Check that the view exists in the API
func testAccCheckViewDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected view state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccViewResourceConfig(dataset string, viewYaml string) string {
	return fmt.Sprintf(`
resource "dash0_view" "test" {
  dataset = %[1]q
  view_yaml = %q
}
`, dataset, viewYaml)
}

// Function to generate import ID for view resource
func testAccViewImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Combine origin and dataset for import ID
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
