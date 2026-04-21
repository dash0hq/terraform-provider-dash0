# Slack webhook notification channel
resource "dash0_notification_channel" "slack_webhook" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Slack Alerts
spec:
  type: slack
  config:
    webhookURL: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
    channel: "#alerts"
  frequency: 10m
YAML
}

# Slack Bot notification channel
#
# Prerequisites:
#   1. Install the Dash0 Slack App via the Dash0 UI (Settings > Notification
#      Channels > Add Notification Channel > Slack Bot > Authorize). This is a
#      one-time operation per Slack workspace.
#   2. Invite the bot to the target channel: /invite @Dash0
resource "dash0_notification_channel" "slack_bot" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Slack Bot Alerts
spec:
  type: slack_bot
  config:
    teamId: "T012345"
    channel: "#alerts"
  frequency: 10m
YAML
}

# Email notification channel
resource "dash0_notification_channel" "email" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Email Alerts
spec:
  type: email_v2
  config:
    recipients:
      - oncall@example.com
      - sre-team@example.com
    plaintext: false
  frequency: 10m
YAML
}

# PagerDuty notification channel
resource "dash0_notification_channel" "pagerduty" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: PagerDuty Incidents
spec:
  type: pagerduty
  config:
    key: "my-pagerduty-integration-key"
    url: "https://events.pagerduty.com/v2/enqueue"
  frequency: 10m
YAML
}

# OpsGenie notification channel
resource "dash0_notification_channel" "opsgenie" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Opsgenie Alerts
spec:
  type: opsgenie
  config:
    apiKey: "my-opsgenie-api-key"
    instance: us
  frequency: 10m
YAML
}

# Generic webhook notification channel
resource "dash0_notification_channel" "webhook" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts
spec:
  type: webhook
  config:
    url: "https://example.com/webhook/alerts"
  frequency: 10m
YAML
}

# Microsoft Teams notification channel
resource "dash0_notification_channel" "teams" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Microsoft Teams Alerts
spec:
  type: teams_webhook
  config:
    url: "https://example.webhook.office.com/webhookb2/..."
  frequency: 10m
YAML
}

# Discord notification channel
resource "dash0_notification_channel" "discord" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Discord Alerts
spec:
  type: discord_webhook
  config:
    url: "https://discord.com/api/webhooks/..."
  frequency: 10m
YAML
}

# Google Chat notification channel
resource "dash0_notification_channel" "google_chat" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Google Chat Alerts
spec:
  type: google_chat_webhook
  config:
    url: "https://chat.googleapis.com/v1/spaces/.../messages?key=..."
  frequency: 10m
YAML
}

# Webhook notification channel with routing rules
#
# Routing rules control which alerts are delivered to this channel.
# Each top-level list item is an OR group; conditions within a group are ANDed.
# In this example, notifications are sent when:
#   (team.name = "sre" AND deployment.environment.name = "production")
#   OR (service.severity = "critical")
resource "dash0_notification_channel" "webhook_with_routing" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Production Alerts (Webhook with Routing)
spec:
  type: webhook
  config:
    url: "https://example.com/webhook/production-alerts"
  frequency: 5m
  routing:
    filters:
      - - key: team.name
          operator: is
          value: sre
        - key: deployment.environment.name
          operator: is
          value: production
      - - key: service.severity
          operator: is
          value: critical
YAML
}

# You can also load the YAML definition from a file:
#
# resource "dash0_notification_channel" "from_file" {
#   notification_channel_yaml = file("${path.module}/notification_channel.yaml")
# }
