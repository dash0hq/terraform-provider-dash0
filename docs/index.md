---
page_title: "Provider: Dash0"
subcategory: ""
description: |-
  The Dash0 provider allows Terraform to manage resources on Dash0 observability platform.
---

# Dash0 Provider

The Dash0 provider allows Terraform to manage resources on [Dash0](https://dash0.com) observability platform. This provider can be used to define dashboards in Perses dashboard format.

## Example Usage

```terraform
# Configure the Dash0 provider
provider "dash0" {
  # No configuration needed in the provider block
  # Authentication is handled via environment variables
  # DASH0_URL and DASH0_AUTH_TOKEN
}

# Create a dashboard
resource "dash0_dashboard" "example" {
  name        = "example-dashboard"
  description = "Example dashboard created via Terraform"
  
  dashboard_definition_yaml = <<-EOT
    kind: Dashboard
    metadata:
      name: example-dashboard
    spec:
      panels:
        - kind: Panel
          spec:
            display:
              name: Sample Panel
            plugin:
              kind: TimeSeriesChart
              spec:
                queries:
                  - kind: PrometheusTimeSeriesQuery
                    spec:
                      query: "up"
  EOT
}
```

## Authentication

The Dash0 provider authenticates using environment variables only:

```shell
export DASH0_AUTH_TOKEN="auth_xxxx"
export DASH0_URL="https://api.us-west-2.aws.dash0.com"  # Optional, defaults to https://api.us-west-2.aws.dash0.com
```

```terraform
provider "dash0" {
  # No configuration needed in the provider block
  # Authentication is handled via environment variables
}
```

## Environment Variables

- `DASH0_URL` - Dash0 API URL (e.g., https://api.us-west-2.aws.dash0.com). Defaults to `https://api.us-west-2.aws.dash0.com` if not provided.
- `DASH0_AUTH_TOKEN` - Dash0 authentication token (e.g., auth_xxxx). **Required**
