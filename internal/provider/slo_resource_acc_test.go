package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/converter"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const sloResourceName = "dash0_slo.test"

// basicSLOAccYaml is an OpenSLO v1 document within the supported subset
// (single objective, inline ratioMetric, Occurrences budgeting, rolling 28d).
const basicSLOAccYaml = `apiVersion: openslo.com/v1
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

const updatedSLOAccYaml = `apiVersion: openslo.com/v1
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

const metadataSLOAccYaml = `apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-availability
  labels:
    team: checkout
  annotations:
    dash0.com/display-name: Checkout availability (managed by Terraform)
    dash0.com/enabled: "false"
    dash0.com/folder-path: /terraform-test
    owner: checkout-oncall
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

func TestAccSLOResource(t *testing.T) {
	var origin string
	editedOutsideTerraformSLOAccYaml := strings.Replace(metadataSLOAccYaml, "(managed by Terraform)", "(edited outside Terraform)", 1)

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
					testAccCaptureSLOOrigin(sloResourceName, &origin),

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
			{
				Config: testAccSLOResourceConfig("terraform-test", metadataSLOAccYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(sloResourceName, "slo_yaml", metadataSLOAccYaml),
					testAccCheckSLOMetadata(sloResourceName,
						map[string]string{"team": "checkout"},
						map[string]string{
							"dash0.com/display-name": "Checkout availability (managed by Terraform)",
							"dash0.com/enabled":      "false",
							"dash0.com/folder-path":  "/terraform-test",
							"owner":                  "checkout-oncall",
						},
					),
				),
			},
			{
				PreConfig: testAccUpdateSLOOutsideTerraform(t, &origin, "terraform-test", editedOutsideTerraformSLOAccYaml),
				Config:    testAccSLOResourceConfig("terraform-test", metadataSLOAccYaml),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(sloResourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: testAccCheckSLOMetadata(sloResourceName, nil, map[string]string{
					"dash0.com/display-name": "Checkout availability (managed by Terraform)",
				}),
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
		c, err := newSLOAccClient()
		if err != nil {
			return err
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

func testAccCaptureSLOOrigin(resourceName string, origin *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		*origin = rs.Primary.Attributes["origin"]
		return nil
	}
}

func testAccCheckSLOMetadata(resourceName string, wantLabels, wantAnnotations map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		c, err := newSLOAccClient()
		if err != nil {
			return err
		}

		apiResponseJSON, err := c.GetSLO(context.Background(), rs.Primary.Attributes["origin"], rs.Primary.Attributes["dataset"])
		if err != nil {
			return fmt.Errorf("Error retrieving SLO: %s", err)
		}

		var slo struct {
			Metadata struct {
				Labels      map[string]interface{} `json:"labels"`
				Annotations map[string]interface{} `json:"annotations"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(apiResponseJSON), &slo); err != nil {
			return fmt.Errorf("Error parsing SLO: %s", err)
		}

		if err := checkSLOMetadataValues("label", slo.Metadata.Labels, wantLabels); err != nil {
			return err
		}
		return checkSLOMetadataValues("annotation", slo.Metadata.Annotations, wantAnnotations)
	}
}

func testAccUpdateSLOOutsideTerraform(t *testing.T, origin *string, dataset, sloYaml string) func() {
	return func() {
		sloJSON, err := converter.ConvertYAMLToJSON(sloYaml)
		if err != nil {
			t.Fatalf("Error converting SLO YAML: %s", err)
		}

		c, err := newSLOAccClient()
		if err != nil {
			t.Fatal(err)
		}

		if err := c.UpdateSLO(context.Background(), *origin, sloJSON, dataset); err != nil {
			t.Fatalf("Error updating SLO outside Terraform: %s", err)
		}
	}
}

func newSLOAccClient() (client.Client, error) {
	c, err := client.NewDash0Client(
		os.Getenv("DASH0_URL"),
		dash0.StaticAuthTokenProvider(os.Getenv("DASH0_AUTH_TOKEN")),
		false,
		"test",
		3,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("Error creating client: %s", err)
	}
	return c, nil
}

func checkSLOMetadataValues(kind string, got map[string]interface{}, want map[string]string) error {
	for key, wantValue := range want {
		if got[key] != wantValue {
			return fmt.Errorf("SLO %s %q = %v, want %q", kind, key, got[key], wantValue)
		}
	}
	return nil
}
