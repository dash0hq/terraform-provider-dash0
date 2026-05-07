---
layout: ""
page_title: "Provider: Dash0"
description: |-
  The Dash0 provider provides dashboard, check rule and more resources for Dash0.
---

# Dash0 Provider

The Dash0 provider provides dashboard, check rule and more resources for [Dash0](https://dash0.com/).

The changelog for this provider can be found [on GitHub](https://github.com/dash0hq/terraform-provider-dash0/releases).

## Authentication

The Dash0 provider supports two authentication methods. You can get the authentication credentials through [Dash0's settings screens](https://app.dash0.com/settings/auth-tokens).

### Option 1: Environment Variables (Recommended)

Environment variables take precedence over provider configuration attributes.

```sh
export DASH0_URL="https://api.us-west-2.aws.dash0.com"
export DASH0_AUTH_TOKEN="auth_xxxx"
```

### Option 2: Provider Configuration

Alternatively, you can configure authentication directly in the provider block:

```hcl
provider "dash0" {
  url        = "https://api.us-west-2.aws.dash0.com"
  auth_token = "auth_xxxx"
}
```
**Note:** Environment variables (`DASH0_URL` and `DASH0_AUTH_TOKEN`) will override provider configuration attributes if both are set.

### Option 3: Using [Dash0 CLI](https://github.com/dash0hq/dash0-cli) configured profiles

If Dash0 CLI is installed and configured already with the credentials for it 
to be able to connect to Dash0 APIs, the provider will automatically pick up 
current `activeProfile` credentials from it. You can also specify a specific
profile from which you want the provider to pick up credentials from by 
specifying `profile` attribute like below - 

```hcl
provider "dash0" {
  profile = "test"
}

```

**Note:** Environment variables (`DASH0_URL` and `DASH0_AUTH_TOKEN`) will 
override provider configuration which overrides CLI configurations.

## Examples

### Creating a Dash0 provider

#### Using Environment Variables

```terraform
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

provider "dash0" {
  # Configuration will be read from environment variables:
  # DASH0_URL and DASH0_AUTH_TOKEN
}
```

#### Using Provider Configuration

```terraform
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

provider "dash0" {
  url        = "https://api.us-west-2.aws.dash0.com"
  auth_token = "auth_xxxx"
}
```

### Managing a Dashboard

```terraform
resource "dash0_dashboard" "my_dashboard" {
  dataset        = "default"
  dashboard_yaml = file("${path.module}/dashboard.yaml")
}
```

### Managing a Synthetic Check

```terraform
resource "dash0_synthetic_check" "my_check" {
  dataset              = "default"
  synthetic_check_yaml = file("${path.module}/synthetic_check.yaml")
}
```

### Managing a View

```terraform
resource "dash0_view" "my_check" {
  dataset   = "default"
  view_yaml = file("${path.module}/view.yaml")
}
```

### Managing a Check Rule

```terraform
resource "dash0_check_rule" "adservice_error_rate" {
  dataset = "production"

  # Currently only one group incl. one rule is supported
  check_rule_yaml = <<-EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: adservice
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: adservice
          expr: (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != "", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != ""}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            description: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "40"
            dash0-threshold-degraded: "35"
            dash0-enabled: true
          labels: {}
EOF
}
```

### Managing Notification Channels

```terraform
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
```
