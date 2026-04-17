# Webhook notification channel
resource "dash0_notification_channel" "webhook" {
  notification_channel_yaml = file("${path.module}/notification_channel_webhook.yaml")
}

# Slack notification channel
resource "dash0_notification_channel" "slack" {
  notification_channel_yaml = file("${path.module}/notification_channel_slack.yaml")
}

# Email notification channel
resource "dash0_notification_channel" "email" {
  notification_channel_yaml = file("${path.module}/notification_channel_email.yaml")
}

# PagerDuty notification channel
resource "dash0_notification_channel" "pagerduty" {
  notification_channel_yaml = file("${path.module}/notification_channel_pagerduty.yaml")
}

# Opsgenie notification channel
resource "dash0_notification_channel" "opsgenie" {
  notification_channel_yaml = file("${path.module}/notification_channel_opsgenie.yaml")
}

# Microsoft Teams notification channel
resource "dash0_notification_channel" "teams" {
  notification_channel_yaml = file("${path.module}/notification_channel_teams.yaml")
}

# Discord notification channel
resource "dash0_notification_channel" "discord" {
  notification_channel_yaml = file("${path.module}/notification_channel_discord.yaml")
}

# Google Chat notification channel
resource "dash0_notification_channel" "google_chat" {
  notification_channel_yaml = file("${path.module}/notification_channel_google_chat.yaml")
}

# Webhook notification channel with routing rules
resource "dash0_notification_channel" "webhook_with_routing" {
  notification_channel_yaml = file("${path.module}/notification_channel_with_routing.yaml")
}
