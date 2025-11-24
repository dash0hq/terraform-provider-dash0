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

```terraform
provider "dash0" {
  url        = "https://api.us-west-2.aws.dash0.com"
  auth_token = "auth_xxxx"
}
```

**Note:** Environment variables (`DASH0_URL` and `DASH0_AUTH_TOKEN`) will override provider configuration attributes if both are set.

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