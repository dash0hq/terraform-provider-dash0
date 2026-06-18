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

The Dash0 provider supports three authentication sources. You can get the authentication credentials through [Dash0's settings screens](https://app.dash0.com/settings/auth-tokens).

Credentials are resolved in this order:

1. The `DASH0_API_URL` and `DASH0_AUTH_TOKEN` environment variables (`DASH0_URL` is accepted as a deprecated fallback for the URL).
2. The `url` and `auth_token` provider attributes.
3. A [dash0 CLI](https://github.com/dash0hq/dash0-cli) profile — the one named by the `profile` provider attribute, or the active profile in the CLI configuration directory if `profile` is unset.

### Option 1: Environment Variables (Recommended)

```sh
export DASH0_API_URL="https://api.us-west-2.aws.dash0.com"
export DASH0_AUTH_TOKEN="auth_xxxx"
export DASH0_MAX_RETRIES=3  # optional, default: 3, max: 5
```

The following environment variables are supported:

| Variable | Required | Description | Default |
|----------|----------|-------------|---------|
| `DASH0_API_URL` | Yes | The base URL of the Dash0 API (e.g. `https://api.us-west-2.aws.dash0.com`). Overrides the `url` provider attribute. | — |
| `DASH0_URL` | No | Deprecated alias for `DASH0_API_URL`. Used only when `DASH0_API_URL` is not set. | — |
| `DASH0_AUTH_TOKEN` | Yes | The API auth token for Dash0. Must start with `auth_` or `dash0_at_`. Overrides the `auth_token` provider attribute. | — |
| `DASH0_CONFIG_DIR` | No | Directory containing the dash0 CLI configuration files (`activeProfile`, `profiles.json`). Used when loading credentials from a CLI profile. | `~/.dash0` |
| `DASH0_MAX_RETRIES` | No | Maximum number of retries for failed API requests (0–5). Overrides the `max_retries` provider attribute. | `3` |

### Option 2: Provider Configuration

Alternatively, you can configure the provider directly in the provider block:

```terraform
provider "dash0" {
  url         = "https://api.us-west-2.aws.dash0.com"
  auth_token  = "auth_xxxx"
  max_retries = 3  # optional, default: 3, max: 5
}
```

Environment variables take precedence over provider configuration attributes when both are set.

### Option 3: dash0 CLI profile

If the [dash0 CLI](https://github.com/dash0hq/dash0-cli) is installed and configured, the provider can load credentials from one of its profiles when neither environment variables nor provider attributes supply them. By default the active profile is used; the `profile` attribute selects a specific profile by name.

```terraform
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "~> 1.6.0"
    }
  }
}

# The `profile` attribute loads credentials from a named dash0 CLI profile
# (configured via `dash0 config profiles create`). Omit it to fall back to the
# CLI's active profile.
provider "dash0" {
  profile = "test1"
}
```

The CLI configuration directory defaults to `~/.dash0`; set `DASH0_CONFIG_DIR` to point at a different location (useful for tests or for sandboxed environments).

#### OAuth-enabled profiles

Profiles authenticated via `dash0 auth login` (OAuth) are fully supported. The provider transparently refreshes the access token when it is close to expiry. If the refresh token itself has expired or been revoked, the provider emits a clear error asking you to re-authenticate:

```
Error: OAuth re-authentication required

The OAuth session for your dash0 CLI profile has expired.
Run `dash0 auth login` to re-authenticate, then re-run your Terraform command.
```

**Note:** Provider versions before this feature was added reject OAuth-enabled profiles with an `Invalid Dash0 Auth Token` error because they require auth tokens to start with the `auth_` prefix. OAuth access tokens use the `dash0_at_` prefix instead. If you see this error, upgrade the provider to the latest version.

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
  # DASH0_API_URL (or DASH0_URL as fallback) and DASH0_AUTH_TOKEN
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

# Creating notification channels with `for_each`, and linking one of them
# from a synthetic check.
#
# `dash0_notification_channel` exposes a computed `id` attribute (the
# server-assigned UUID, resolved by the provider after creation). The
# synthetic check references that id in `spec.notifications.channels`,
# which requires raw UUIDs rather than the `tf_`-prefixed origin.
resource "dash0_notification_channel" "team_oncall" {
  for_each = {
    backend  = "backend-oncall@example.com"
    frontend = "frontend-oncall@example.com"
    sre      = "sre-oncall@example.com"
  }

  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: ${each.key} on-call
spec:
  type: email_v2
  config:
    recipients:
      - ${each.value}
    plaintext: false
  frequency: 10m
YAML
}

resource "dash0_synthetic_check" "checkout_api" {
  dataset = "default"

  synthetic_check_yaml = <<-YAML
kind: Dash0SyntheticCheck
metadata:
  name: checkout-api
spec:
  enabled: true
  notifications:
    channels:
      - ${dash0_notification_channel.team_oncall["sre"].id}
  plugin:
    display:
      name: checkout-api
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
      request:
        method: get
        url: https://api.example.com/health
        queryParameters: []
        headers: []
        redirects: follow
        tls:
          allowInsecure: false
        tracing:
          addTracingHeaders: true
  retries:
    kind: fixed
    spec:
      attempts: 3
      delay: 1s
  schedule:
    interval: 1m
    locations:
      - de-frankfurt
      - us-oregon
    strategy: all_locations
YAML
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

# Fanning out check rules with `for_each` and routing each rule's alerts to one
# or more notification channels through the `dash0.com/notification-channel-ids`
# annotation. The annotation takes a comma-separated list of channel UUIDs —
# the `id` attribute on `dash0_notification_channel`, not the `origin`.
resource "dash0_notification_channel" "team_oncall" {
  for_each = {
    backend  = "backend-oncall@example.com"
    frontend = "frontend-oncall@example.com"
    sre      = "sre-oncall@example.com"
  }

  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: ${each.key} on-call
spec:
  type: email_v2
  config:
    recipients:
      - ${each.value}
    plaintext: false
  frequency: 10m
YAML
}

locals {
  service_check_rules = {
    backend-api = {
      service  = "backend-api"
      channels = ["backend", "sre"]
    }
    frontend-web = {
      service  = "frontend-web"
      channels = ["frontend"]
    }
    checkout = {
      service  = "checkout"
      channels = ["backend", "sre"]
    }
  }
}

resource "dash0_check_rule" "service_error_rate" {
  for_each = local.service_check_rules

  dataset = "production"

  check_rule_yaml = <<-EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: ${each.key}-error-rate
  annotations:
    dash0.com/notification-channel-ids: ${join(",", [for c in each.value.channels : dash0_notification_channel.team_oncall[c].id])}
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: ${each.value.service}-error-rate
          expr: (sum by (service_name) (increase({otel_metric_name = "dash0.spans", service_name = "${each.value.service}", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_name) (increase({otel_metric_name = "dash0.spans", service_name = "${each.value.service}"}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for ${each.value.service}: {{$value|printf "%.2f"}}%'
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
#   3. The Dash0 bot must be explicitly added to each Slack channel it will post to.
#      In Slack, open the target channel and run `/invite @Dash0`. Repeat this for every channel
#      you want to receive notifications in.
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