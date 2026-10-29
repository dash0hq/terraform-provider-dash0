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

const samplingRuleResourceName = "dash0_sampling_rule.test"

// The rules are created disabled so the test never changes which traces the
// dataset keeps.
const basicSamplingRuleYaml = `kind: Dash0Sampling
metadata:
  name: acc-keep-errors
spec:
  enabled: false
  display:
    name: Keep error traces
  conditions:
    kind: error
    spec: {}
  rateLimit:
    rate: 1000`

const updatedSamplingRuleYaml = `kind: Dash0Sampling
metadata:
  name: acc-keep-errors
spec:
  enabled: false
  display:
    name: Keep error traces (updated)
  conditions:
    kind: and
    spec:
      conditions:
        - kind: error
          spec: {}
        - kind: probabilistic
          spec:
            rate: 0.5
  rateLimit:
    rate: 500`

// TestAccSamplingRuleResource exercises the resource against a real Dash0 API.
// Creating a sampling rule requires a token with the admin role.
func TestAccSamplingRuleResource(t *testing.T) {
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
				Config: testAccSamplingRuleResourceConfig("terraform-test", basicSamplingRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSamplingRuleExists(samplingRuleResourceName),
					resource.TestCheckResourceAttr(samplingRuleResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(samplingRuleResourceName, "sampling_rule_yaml", basicSamplingRuleYaml),
					resource.TestMatchResourceAttr(samplingRuleResourceName, "origin", tfOriginPattern),
				),
			},
			// Idempotency: the plan immediately after apply must be empty.
			{
				Config:   testAccSamplingRuleResourceConfig("terraform-test", basicSamplingRuleYaml),
				PlanOnly: true,
			},
			// Import
			{
				ResourceName:      samplingRuleResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccSamplingRuleImportStateIdFunc(samplingRuleResourceName),
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
					if y := states[0].Attributes["sampling_rule_yaml"]; y == "" {
						return fmt.Errorf("sampling_rule_yaml attribute is missing or empty")
					}
					return nil
				},
			},
			// Update
			{
				Config: testAccSamplingRuleResourceConfig("terraform-test", updatedSamplingRuleYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSamplingRuleExists(samplingRuleResourceName),
					resource.TestCheckResourceAttr(samplingRuleResourceName, "sampling_rule_yaml", updatedSamplingRuleYaml),
				),
			},
			// Destroy
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckSamplingRuleDoesNotExist(samplingRuleResourceName),
				),
			},
		},
	})
}

func testAccSamplingRuleResourceConfig(dataset, ruleYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_sampling_rule" "test" {
  dataset = %q
  sampling_rule_yaml = %q
}
`, dataset, ruleYaml)
}

func testAccCheckSamplingRuleExists(resourceName string) resource.TestCheckFunc {
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

		if _, err = c.GetSamplingRule(context.Background(), origin, dataset); err != nil {
			return fmt.Errorf("error retrieving sampling rule: %s", err)
		}
		return nil
	}
}

func testAccCheckSamplingRuleDoesNotExist(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if _, ok := s.RootModule().Resources[resourceName]; ok {
			return fmt.Errorf("expected sampling rule state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccSamplingRuleImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
