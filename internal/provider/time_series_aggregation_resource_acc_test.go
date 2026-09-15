package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const timeSeriesAggregationResourceName = "dash0_time_series_aggregation.test"

// tfOriginPattern pins the provider's origin convention: the server derives
// dash0.com/source: terraform from this prefix.
var tfOriginPattern = regexp.MustCompile(`^tf_`)

const basicTimeSeriesAggregationYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: acc-http-server-request-duration
spec:
  enabled: true
  display:
    name: HTTP server request duration rollup
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m`

const updatedTimeSeriesAggregationYaml = `apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: acc-http-server-request-duration
spec:
  enabled: true
  display:
    name: HTTP server request duration rollup (updated)
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 30m`

// TestAccTimeSeriesAggregationResource exercises the resource against a real
// Dash0 API. Note that every time series aggregation endpoint requires the
// organization-admin role, which is stricter than any other asset type: a
// non-admin token fails here with a 403 naming the role.
func TestAccTimeSeriesAggregationResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read
			{
				Config: testAccTimeSeriesAggregationResourceConfig("terraform-test", basicTimeSeriesAggregationYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTimeSeriesAggregationExists(timeSeriesAggregationResourceName),
					resource.TestCheckResourceAttr(timeSeriesAggregationResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(timeSeriesAggregationResourceName, "time_series_aggregation_yaml", basicTimeSeriesAggregationYaml),
					resource.TestCheckResourceAttrSet(timeSeriesAggregationResourceName, "origin"),
					resource.TestMatchResourceAttr(timeSeriesAggregationResourceName, "origin", tfOriginPattern),
				),
			},
			// Idempotency: the plan immediately after apply must be empty.
			{
				Config:   testAccTimeSeriesAggregationResourceConfig("terraform-test", basicTimeSeriesAggregationYaml),
				PlanOnly: true,
			},
			// Import
			{
				ResourceName:      timeSeriesAggregationResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccTimeSeriesAggregationImportStateIdFunc(timeSeriesAggregationResourceName),
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					if origin := states[0].Attributes["origin"]; origin == "" {
						return fmt.Errorf("origin attribute is missing or empty")
					}
					if dataset := states[0].Attributes["dataset"]; dataset != "terraform-test" {
						return fmt.Errorf("expected dataset 'terraform-test', got '%s'", dataset)
					}
					if y := states[0].Attributes["time_series_aggregation_yaml"]; y == "" {
						return fmt.Errorf("time_series_aggregation_yaml attribute is missing or empty")
					}
					return nil
				},
			},
			// Update
			{
				Config: testAccTimeSeriesAggregationResourceConfig("terraform-test", updatedTimeSeriesAggregationYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTimeSeriesAggregationExists(timeSeriesAggregationResourceName),
					resource.TestCheckResourceAttr(timeSeriesAggregationResourceName, "time_series_aggregation_yaml", updatedTimeSeriesAggregationYaml),
				),
			},
			// Dataset change forces recreation under a new origin.
			{
				Config: testAccTimeSeriesAggregationResourceConfig("another-dataset", updatedTimeSeriesAggregationYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTimeSeriesAggregationExists(timeSeriesAggregationResourceName),
					resource.TestCheckResourceAttr(timeSeriesAggregationResourceName, "dataset", "another-dataset"),
				),
			},
			// Destroy
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTimeSeriesAggregationDoesNotExist(timeSeriesAggregationResourceName),
				),
			},
		},
	})
}

func testAccTimeSeriesAggregationResourceConfig(dataset, tsaYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_time_series_aggregation" "test" {
  dataset = %q
  time_series_aggregation_yaml = %q
}
`, dataset, tsaYaml)
}

func testAccCheckTimeSeriesAggregationExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		origin := rs.Primary.Attributes["origin"]
		dataset := rs.Primary.Attributes["dataset"]

		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			dash0.StaticAuthTokenProvider(os.Getenv("DASH0_AUTH_TOKEN")),
			false,
			"test",
			3,
			"",
		)
		if err != nil {
			return fmt.Errorf("error creating client: %s", err)
		}

		if _, err = c.GetTimeSeriesAggregation(context.Background(), origin, dataset); err != nil {
			return fmt.Errorf("error retrieving time series aggregation: %s", err)
		}
		return nil
	}
}

func testAccCheckTimeSeriesAggregationDoesNotExist(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if _, ok := s.RootModule().Resources[resourceName]; ok {
			return fmt.Errorf("expected time series aggregation state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccTimeSeriesAggregationImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
