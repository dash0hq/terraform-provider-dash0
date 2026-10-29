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

const customSkillResourceName = "dash0_custom_skill.test"

const basicCustomSkillYaml = `kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: tf-acc-incident-response
        description: Triage incidents with the team runbook
        ---
        Follow the runbook in references/runbook.md.
    - relativePath: references/runbook.md
      content: |
        1. Check the error rate.`

const updatedCustomSkillYaml = `kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: tf-acc-incident-response
        description: Triage incidents with the team runbook
        ---
        Follow the runbook in references/runbook.md, then post a summary.
    - relativePath: references/runbook.md
      content: |
        1. Check the error rate.
        2. Check recent deployments.
  permissions:
    - role: basic_member
      actions: [custom_skill:read]`

func TestAccCustomSkillResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCustomSkillResourceConfig(basicCustomSkillYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCustomSkillExists(customSkillResourceName),
					resource.TestCheckResourceAttr(customSkillResourceName, "custom_skill_yaml", basicCustomSkillYaml),
					resource.TestCheckResourceAttrSet(customSkillResourceName, "origin"),
					resource.TestCheckResourceAttrSet(customSkillResourceName, "id"),
				),
			},
			{
				ResourceName:      customSkillResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccCustomSkillImportStateIdFunc(customSkillResourceName),
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					for _, attr := range []string{"origin", "id", "custom_skill_yaml"} {
						if states[0].Attributes[attr] == "" {
							return fmt.Errorf("%s attribute is missing or empty", attr)
						}
					}
					return nil
				},
			},
			{
				Config: testAccCustomSkillResourceConfig(updatedCustomSkillYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCustomSkillExists(customSkillResourceName),
					resource.TestCheckResourceAttr(customSkillResourceName, "custom_skill_yaml", updatedCustomSkillYaml),
				),
			},
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckCustomSkillDoesNotExist(customSkillResourceName),
				),
			},
		},
	})
}

func testAccCustomSkillResourceConfig(skillYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_custom_skill" "test" {
  custom_skill_yaml = %q
}
`, skillYaml)
}

func testAccCheckCustomSkillExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		origin := rs.Primary.Attributes["origin"]
		if origin == "" {
			return fmt.Errorf("no custom skill origin is set")
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

		if _, err = c.GetCustomSkill(context.Background(), origin); err != nil {
			return fmt.Errorf("Error retrieving custom skill: %s", err)
		}
		return nil
	}
}

func testAccCheckCustomSkillDoesNotExist(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if _, ok := s.RootModule().Resources[resourceName]; ok {
			return fmt.Errorf("expected custom skill state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccCustomSkillImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}
		return rs.Primary.Attributes["origin"], nil
	}
}
