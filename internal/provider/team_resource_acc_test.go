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

const teamResourceName = "dash0_team.test"

// basicTeamYaml mirrors the shared create.yaml fixture used across the four
// IaC facilities. Members reference organization users by email; the server
// resolves them during reconciliation. Emails are substituted from the
// DASH0_ACC_TEAM_MEMBER_{1,2,3}_EMAIL environment variables at test time so the
// test can target the real organization behind DASH0_URL/DASH0_AUTH_TOKEN.
const basicTeamYamlTemplate = `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - %s
    - %s`

// updatedTeamYamlTemplate mirrors update.yaml: renamed description and a
// membership shift (drop the second member, add the third).
const updatedTeamYamlTemplate = `kind: Dash0Team
metadata:
  name: backend-team
spec:
  display:
    name: Backend Team
    description: Owns backend services, the data platform, and the on-call rotation.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - %s
    - %s`

func TestAccTeamResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	member1 := os.Getenv("DASH0_ACC_TEAM_MEMBER_1_EMAIL")
	member2 := os.Getenv("DASH0_ACC_TEAM_MEMBER_2_EMAIL")
	member3 := os.Getenv("DASH0_ACC_TEAM_MEMBER_3_EMAIL")
	if member1 == "" || member2 == "" || member3 == "" {
		t.Skip("Acceptance test needs three real org member emails; set DASH0_ACC_TEAM_MEMBER_{1,2,3}_EMAIL to run")
	}

	basicTeamYaml := fmt.Sprintf(basicTeamYamlTemplate, member1, member2)
	updatedTeamYaml := fmt.Sprintf(updatedTeamYamlTemplate, member1, member3)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create + verify state matches the fixture.
			{
				Config: testAccTeamResourceConfig(basicTeamYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTeamExists(teamResourceName),
					resource.TestCheckResourceAttr(teamResourceName, "team_yaml", basicTeamYaml),
					resource.TestCheckResourceAttrSet(teamResourceName, "origin"),
				),
			},
			// Import (by origin).
			{
				ResourceName:      teamResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				ImportStateIdFunc: testAccTeamImportStateIdFunc(teamResourceName),
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}
					if origin := states[0].Attributes["origin"]; origin == "" {
						return fmt.Errorf("origin attribute is missing or empty")
					}
					if yaml := states[0].Attributes["team_yaml"]; yaml == "" {
						return fmt.Errorf("team_yaml attribute is missing or empty")
					}
					return nil
				},
			},
			// Update (change display description + membership).
			{
				Config: testAccTeamResourceConfig(updatedTeamYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTeamExists(teamResourceName),
					resource.TestCheckResourceAttr(teamResourceName, "team_yaml", updatedTeamYaml),
				),
			},
			// Idempotency: re-applying the same config yields no diff.
			{
				Config:   testAccTeamResourceConfig(updatedTeamYaml),
				PlanOnly: true,
			},
			// Destroy.
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckTeamDoesNotExists(teamResourceName),
				),
			},
		},
	})
}

func testAccTeamResourceConfig(teamYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_team" "test" {
  team_yaml = %q
}
`, teamYaml)
}

func testAccCheckTeamExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("no team ID is set")
		}

		origin := rs.Primary.Attributes["origin"]

		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
			3,
		)
		if err != nil {
			return fmt.Errorf("Error creating client: %s", err)
		}

		if _, err := c.GetTeam(context.Background(), origin); err != nil {
			return fmt.Errorf("Error retrieving team: %s", err)
		}
		return nil
	}
}

func testAccCheckTeamDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if _, ok := s.RootModule().Resources[resourceName]; ok {
			return fmt.Errorf("expected team state not to exist: %s", resourceName)
		}
		return nil
	}
}

func testAccTeamImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}
		return rs.Primary.Attributes["origin"], nil
	}
}
