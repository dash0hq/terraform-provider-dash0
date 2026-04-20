# Webhook notification channel
resource "dash0_notification_channel" "webhook" {
  notification_channel_yaml = file("${path.module}/notification_channel_webhook.yaml")
}

# Basic Webhook notification channel with inline YAML
resource "dash0_notification_channel" "webhook_inline" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Webhook Alerts
spec:
  type: webhook
  config:
    url: https://example.com/webhook/alerts
  frequency: 10m
YAML
}

# Slack Webhook notification channel
resource "dash0_notification_channel" "slack_webhook" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Slack Alerts
spec:
  type: slack
  config:
    channel: "#alerts"
    webhookURL: *slack_webhook_url # TODO CHANGE THIS
  frequency: 10m
YAML
}

# Slack Bot notification channel
resource "dash0_notification_channel" "slack_bot" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Slack Bot Alerts
spec:
  type: slack_bot
  config:
    channel: "#alerts"
    teamId: *slack_teamId # TODO CHANGE THIS
  frequency: 6h
YAML
}

# Advanced Webhook notification channel with routing
resource "dash0_notification_channel" "webhook_with_routing" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: Production Alerts (Webhook with Routing)
spec:
  type: webhook
  config:
    url: https://example.com/webhook/production-alerts
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
