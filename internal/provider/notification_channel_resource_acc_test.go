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

const notificationChannelResourceName = "dash0_notification_channel.test"

const basicNotificationChannelYaml = `kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts
spec:
  type: webhook
  config:
    url: https://example.com/webhook/alerts`

const updatedNotificationChannelYaml = `kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts (Updated)
spec:
  type: webhook
  config:
    url: https://example.com/webhook/alerts-updated`

func TestAccNotificationChannelResource(t *testing.T) {
	// Skip if TF_ACC is not set to "1"
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Acceptance tests skipped unless TF_ACC=1")
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccNotificationChannelResourceConfig(basicNotificationChannelYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify the notification channel exists
					testAccCheckNotificationChannelExists(notificationChannelResourceName),
					// Verify attributes
					resource.TestCheckResourceAttr(notificationChannelResourceName, "notification_channel_yaml", basicNotificationChannelYaml),
					resource.TestCheckResourceAttrSet(notificationChannelResourceName, "origin"),
				),
			},
			// ImportState testing
			{
				ResourceName:      notificationChannelResourceName,
				ImportState:       true,
				ImportStateVerify: false,
				// Import uses only the origin (no dataset prefix)
				ImportStateIdFunc: testAccNotificationChannelImportStateIdFunc(notificationChannelResourceName),
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					// Verify we have exactly one state
					if len(states) != 1 {
						return fmt.Errorf("expected 1 state, got %d", len(states))
					}

					// Verify the origin attribute
					if origin := states[0].Attributes["origin"]; origin == "" {
						return fmt.Errorf("origin attribute is missing or empty")
					}

					// Verify the notification_channel_yaml attribute
					if yaml := states[0].Attributes["notification_channel_yaml"]; yaml == "" {
						return fmt.Errorf("notification_channel_yaml attribute is missing or empty")
					}

					return nil
				},
			},
			// Update testing
			{
				Config: testAccNotificationChannelResourceConfig(updatedNotificationChannelYaml),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckNotificationChannelExists(notificationChannelResourceName),
					resource.TestCheckResourceAttr(notificationChannelResourceName, "notification_channel_yaml", updatedNotificationChannelYaml),
				),
			},
			// Test deleting
			{
				Config: `provider "dash0" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckNotificationChannelDoesNotExists(notificationChannelResourceName),
				),
			},
		},
	})
}

// Test configuration for notification channel resource
func testAccNotificationChannelResourceConfig(notificationChannelYaml string) string {
	return fmt.Sprintf(`
provider "dash0" {}

resource "dash0_notification_channel" "test" {
  notification_channel_yaml = %q
}
`, notificationChannelYaml)
}

// Check that the notification channel exists in the API
func testAccCheckNotificationChannelExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("no notification channel ID is set")
		}

		// Extract origin from state
		origin := rs.Primary.Attributes["origin"]

		// Create a new client to verify the notification channel exists
		c, err := client.NewDash0Client(
			os.Getenv("DASH0_URL"),
			os.Getenv("DASH0_AUTH_TOKEN"),
			"test",
		)
		if err != nil {
			return fmt.Errorf("Error creating client: %s", err)
		}

		// Attempt to retrieve the notification channel
		_, err = c.GetNotificationChannel(context.Background(), origin)
		if err != nil {
			return fmt.Errorf("Error retrieving notification channel: %s", err)
		}

		return nil
	}
}

// Check that the notification channel does not exist
func testAccCheckNotificationChannelDoesNotExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		_, ok := s.RootModule().Resources[resourceName]
		if ok {
			return fmt.Errorf("expected notification channel state not to exist: %s", resourceName)
		}
		return nil
	}
}

// Function to generate import ID for notification channel resource
func testAccNotificationChannelImportStateIdFunc(resourceName string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("not found: %s", resourceName)
		}

		// Import uses only the origin (no dataset prefix)
		return rs.Primary.Attributes["origin"], nil
	}
}
