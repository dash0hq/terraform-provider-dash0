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

const authTokenResourceName = "dash0_auth_token.test"

func testAccAuthTokenYaml(name string) string {
	return fmt.Sprintf(`apiVersion: dash0.com/v1alpha1
kind: Dash0AuthToken
metadata:
  name: %s
spec:
  actions:
    - dashboards:read`, name)
}

// Managing auth tokens needs an admin user or an unrestricted "*" token.
func TestAccAuthTokenResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	basicYaml := testAccAuthTokenYaml("tf-acc-auth-token")
	updatedYaml := testAccAuthTokenYaml("tf-acc-auth-token-updated")
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	var id, origin, token string
	remember := func(s *terraform.State) error {
		attrs := s.RootModule().Resources[authTokenResourceName].Primary.Attributes
		id, origin, token = attrs["id"], attrs["origin"], attrs["token"]
		return nil
	}
	unchanged := func(s *terraform.State) error {
		attrs := s.RootModule().Resources[authTokenResourceName].Primary.Attributes
		if attrs["id"] != id || attrs["origin"] != origin || attrs["token"] != token {
			return fmt.Errorf("id, origin or token changed across the update")
		}
		return nil
	}
	// The API returns the secret only on create, so an imported token has a null secret.
	importCheck := func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("expected 1 state, got %d", len(states))
		}
		if states[0].Attributes["id"] != id {
			return fmt.Errorf("imported id does not match the created auth token")
		}
		if v, ok := states[0].Attributes["token"]; ok && v != "" {
			return fmt.Errorf("an imported auth token must have a null token")
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t); testAccAuthTokensAPIPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckAuthTokenDestroyed(&id),
		Steps: []resource.TestStep{
			{
				Config: testAccAuthTokenResourceConfig(basicYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(authTokenResourceName, "id", uuid),
					resource.TestCheckResourceAttrSet(authTokenResourceName, "origin"),
					resource.TestCheckResourceAttrSet(authTokenResourceName, "token"),
					resource.TestCheckResourceAttr(authTokenResourceName, "auth_token_yaml", basicYaml),
					remember,
				),
			},
			{
				Config: testAccAuthTokenResourceConfig(updatedYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(authTokenResourceName, "auth_token_yaml", updatedYaml),
					unchanged,
				),
			},
			// Idempotency: server-defaulted spec fields cause no diff.
			{
				Config:   testAccAuthTokenResourceConfig(updatedYaml),
				PlanOnly: true,
			},
			// Import by origin, then by id. The imported YAML is the API's, not the
			// config's, and an imported token has no secret, so only the checks apply.
			{
				ResourceName:      authTokenResourceName,
				ImportState:       true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return origin, nil },
				ImportStateCheck:  importCheck,
			},
			{
				ResourceName:      authTokenResourceName,
				ImportState:       true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return id, nil },
				ImportStateCheck:  importCheck,
			},
		},
	})
}

func testAccAuthTokenResourceConfig(authTokenYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_auth_token" "test" {
  auth_token_yaml = %q
}
`, authTokenYaml)
}

// testAccCheckAuthTokenDestroyed proves the server answers 404 for the id.
func testAccCheckAuthTokenDestroyed(id *string) resource.TestCheckFunc {
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
		_, _, err = c.GetAuthToken(context.Background(), *id)
		if err == nil {
			return fmt.Errorf("auth token %s still exists after destroy", *id)
		}
		if !dash0.IsNotFound(err) {
			return fmt.Errorf("expected 404 for auth token %s, got: %w", *id, err)
		}
		return nil
	}
}

// testAccAuthTokensAPIPreCheck skips the test while the target Dash0 API does
// not serve /api/auth-tokens yet (404).
func testAccAuthTokensAPIPreCheck(t *testing.T) {
	c, err := dash0.NewClient(
		dash0.WithApiUrl(os.Getenv("DASH0_URL")),
		dash0.WithAuthToken(os.Getenv("DASH0_AUTH_TOKEN")),
	)
	if err != nil {
		t.Fatalf("Error creating client: %s", err)
	}
	if _, err := c.ListAuthTokens(context.Background()); dash0.IsNotFound(err) {
		t.Skip("The Dash0 API does not serve /api/auth-tokens yet")
	}
}
