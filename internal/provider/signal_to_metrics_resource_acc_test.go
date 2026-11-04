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

const signalToMetricsResourceName = "dash0_signal_to_metrics.test"

// The rules are created disabled, and their filter matches no span, so the
// test never produces metrics in the dataset.
const basicSignalToMetricsYaml = `kind: Dash0SignalToMetrics
metadata:
  name: acc-matches-nothing
spec:
  enabled: false
  display:
    name: Acceptance test rule
  match:
    signal: spans
    filters:
      - key: service.name
        operator: is
        value: dash0-terraform-acceptance-matches-nothing
  output:
    name: dash0_terraform_acceptance_matches_nothing
    interval: 60s`

const updatedSignalToMetricsYaml = `kind: Dash0SignalToMetrics
metadata:
  name: acc-matches-nothing
spec:
  enabled: false
  display:
    name: Acceptance test rule (updated)
  match:
    signal: spans
    filters:
      - key: service.name
        operator: is
        value: dash0-terraform-acceptance-matches-nothing
  output:
    name: dash0_terraform_acceptance_matches_nothing
    interval: 5m
    type: sum
    value: "1"`

// TestAccSignalToMetricsResource exercises the resource against a real Dash0 API.
func TestAccSignalToMetricsResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSignalToMetricsDestroyed,
		Steps: []resource.TestStep{
			// Create and Read
			{
				Config: testAccSignalToMetricsResourceConfig("terraform-test", basicSignalToMetricsYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSignalToMetricsExists(signalToMetricsResourceName),
					resource.TestCheckResourceAttr(signalToMetricsResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(signalToMetricsResourceName, "signal_to_metrics_yaml", basicSignalToMetricsYaml),
					resource.TestMatchResourceAttr(signalToMetricsResourceName, "origin", tfOriginPattern),
				),
			},
			// Idempotency: the plan immediately after apply must be empty.
			{
				Config:   testAccSignalToMetricsResourceConfig("terraform-test", basicSignalToMetricsYaml),
				PlanOnly: true,
			},
			// Import
			{
				ResourceName:      signalToMetricsResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccSignalToMetricsImportStateIdFunc(signalToMetricsResourceName),
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
					if y := states[0].Attributes["signal_to_metrics_yaml"]; y == "" {
						return fmt.Errorf("signal_to_metrics_yaml attribute is missing or empty")
					}
					return nil
				},
			},
			// Update
			{
				Config: testAccSignalToMetricsResourceConfig("terraform-test", updatedSignalToMetricsYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSignalToMetricsExists(signalToMetricsResourceName),
					resource.TestCheckResourceAttr(signalToMetricsResourceName, "signal_to_metrics_yaml", updatedSignalToMetricsYaml),
				),
			},
			// Idempotency after the update.
			{
				Config:   testAccSignalToMetricsResourceConfig("terraform-test", updatedSignalToMetricsYaml),
				PlanOnly: true,
			},
		},
	})
}

func testAccSignalToMetricsResourceConfig(dataset, ruleYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_signal_to_metrics" "test" {
  dataset = %q
  signal_to_metrics_yaml = %q
}
`, dataset, ruleYaml)
}

func testAccSignalToMetricsClient() (client.Client, error) {
	return client.NewDash0Client(
		os.Getenv("DASH0_URL"),
		dash0.StaticAuthTokenProvider(os.Getenv("DASH0_AUTH_TOKEN")),
		false,
		"test",
		3,
		"",
	)
}

func testAccCheckSignalToMetricsExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		c, err := testAccSignalToMetricsClient()
		if err != nil {
			return fmt.Errorf("error creating client: %s", err)
		}

		body, err := c.GetSignalToMetrics(context.Background(), rs.Primary.Attributes["origin"], rs.Primary.Attributes["dataset"])
		if err != nil {
			return fmt.Errorf("error retrieving signal-to-metrics rule: %s", err)
		}
		if isSoftDeleted(body) {
			return fmt.Errorf("signal-to-metrics rule %s is soft-deleted", rs.Primary.Attributes["origin"])
		}
		return nil
	}
}

// testAccCheckSignalToMetricsDestroyed proves the delete server-side. Deletes
// are soft, so a destroyed rule answers GET with a tombstone; a 404 is
// accepted too.
func testAccCheckSignalToMetricsDestroyed(s *terraform.State) error {
	c, err := testAccSignalToMetricsClient()
	if err != nil {
		return fmt.Errorf("error creating client: %s", err)
	}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "dash0_signal_to_metrics" {
			continue
		}
		body, err := c.GetSignalToMetrics(context.Background(), rs.Primary.Attributes["origin"], rs.Primary.Attributes["dataset"])
		if err != nil {
			if dash0.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("could not verify deletion of signal-to-metrics rule %s: %s", rs.Primary.Attributes["origin"], err)
		}
		if !isSoftDeleted(body) {
			return fmt.Errorf("signal-to-metrics rule %s still exists and is not soft-deleted", rs.Primary.Attributes["origin"])
		}
	}
	return nil
}

func testAccSignalToMetricsImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
