package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	dash0 "github.com/dash0hq/dash0-api-client-go"
	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

const evalResourceName = "dash0_eval.test"

// The eval is disabled and samples nothing, so the test never calls the judge.
func testAccEvalYaml(name string, threshold float64) string {
	return fmt.Sprintf(`kind: Dash0GenerativeAiEvaluation
metadata:
  name: %s
spec:
  enabled: false
  display:
    name: Acceptance test eval
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0
  judge:
    model: light
    systemPrompt: You judge whether an answer is relevant to the question.
    userPrompt: "Question: {{ gen_ai.input.messages }}"
    output:
      kind: numeric
      spec:
        threshold: %g`, name, threshold)
}

// TestAccEvalResource exercises the resource against a real Dash0 API.
func TestAccEvalResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	// Eval names are unique per dataset, so concurrent runs must not share one.
	name := "acc-eval-" + acctest.RandString(8)
	basicYaml := testAccEvalYaml(name, 0.5)
	updatedYaml := testAccEvalYaml(name, 0.7)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckEvalDestroyed,
		Steps: []resource.TestStep{
			// Create and Read
			{
				Config: testAccEvalResourceConfig("terraform-test", basicYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckEvalExists(evalResourceName),
					resource.TestCheckResourceAttr(evalResourceName, "dataset", "terraform-test"),
					resource.TestCheckResourceAttr(evalResourceName, evalYAMLAttribute, basicYaml),
					resource.TestMatchResourceAttr(evalResourceName, "origin", tfOriginPattern),
				),
			},
			// Idempotency: the plan immediately after apply must be empty.
			{
				Config:   testAccEvalResourceConfig("terraform-test", basicYaml),
				PlanOnly: true,
			},
			// Import
			{
				ResourceName:      evalResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccEvalImportStateIdFunc(evalResourceName),
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
					if y := states[0].Attributes[evalYAMLAttribute]; y == "" {
						return fmt.Errorf("%s attribute is missing or empty", evalYAMLAttribute)
					}
					return nil
				},
			},
			// Update
			{
				Config: testAccEvalResourceConfig("terraform-test", updatedYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckEvalExists(evalResourceName),
					resource.TestCheckResourceAttr(evalResourceName, evalYAMLAttribute, updatedYaml),
				),
			},
			// Idempotency after the update.
			{
				Config:   testAccEvalResourceConfig("terraform-test", updatedYaml),
				PlanOnly: true,
			},
		},
	})
}

func testAccEvalResourceConfig(dataset, evalYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_eval" "test" {
  dataset   = %q
  eval_yaml = %q
}
`, dataset, evalYaml)
}

func testAccEvalClient() (client.Client, error) {
	return client.NewDash0Client(
		os.Getenv("DASH0_URL"),
		dash0.StaticAuthTokenProvider(os.Getenv("DASH0_AUTH_TOKEN")),
		false,
		"test",
		3,
		"",
	)
}

func testAccCheckEvalExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		c, err := testAccEvalClient()
		if err != nil {
			return fmt.Errorf("error creating client: %s", err)
		}

		body, err := c.GetEval(context.Background(), rs.Primary.Attributes["origin"], rs.Primary.Attributes["dataset"])
		if err != nil {
			return fmt.Errorf("error retrieving eval: %s", err)
		}
		if isSoftDeleted(body) {
			return fmt.Errorf("eval %s is soft-deleted", rs.Primary.Attributes["origin"])
		}
		return nil
	}
}

// testAccCheckEvalDestroyed proves the delete server-side. Deletes are soft,
// so a destroyed eval answers GET with a tombstone; a 404 is accepted too.
func testAccCheckEvalDestroyed(s *terraform.State) error {
	c, err := testAccEvalClient()
	if err != nil {
		return fmt.Errorf("error creating client: %s", err)
	}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "dash0_eval" {
			continue
		}
		body, err := c.GetEval(context.Background(), rs.Primary.Attributes["origin"], rs.Primary.Attributes["dataset"])
		if err != nil {
			if dash0.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("could not verify deletion of eval %s: %s", rs.Primary.Attributes["origin"], err)
		}
		if !isSoftDeleted(body) {
			return fmt.Errorf("eval %s still exists and is not soft-deleted", rs.Primary.Attributes["origin"])
		}
	}
	return nil
}

func testAccEvalImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}
		return fmt.Sprintf("%s,%s", rs.Primary.Attributes["dataset"], rs.Primary.Attributes["origin"]), nil
	}
}
