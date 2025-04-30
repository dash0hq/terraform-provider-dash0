# Dash0 Terraform Provider Usage Guide

This document describes how to use the Dash0 Terraform Provider to manage dashboards in the Dash0 observability platform.

## Installation

You can install this provider using the standard Terraform provider installation methods:

1. Add the provider to your Terraform configuration:

```hcl
terraform {
  required_providers {
    dash0 = {
      source  = "dash0/dash0"
      version = "~> 0.1"
    }
  }
}
```

2. Run `terraform init` to download the provider.

## Provider Configuration

The provider is configured using environment variables only:

```shell
export DASH0_AUTH_TOKEN="auth_xxxx"
export DASH0_URL="https://api.us-west-2.aws.dash0.com"  # Optional
```

```hcl
provider "dash0" {
  # No configuration needed in the provider block
  # Authentication is handled via environment variables
}
```

## Managing Dashboards

Dashboards in Dash0 are defined using the Perses dashboard YAML format. You can define them in two ways:

### Option 1: Inline YAML

```hcl
resource "dash0_dashboard" "example" {
  name        = "example-dashboard"
  description = "Example dashboard created via Terraform"
  dataset     = "default"  # Optional, defaults to "default" if not specified
  
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

### Option 2: Load from YAML File (Recommended)

```hcl
resource "dash0_dashboard" "example" {
  name        = "example-dashboard"
  description = "Example dashboard created via Terraform"
  dataset     = "production"  # Optional, defaults to "default" if not specified
  
  # Load the dashboard definition from a local YAML file
  dashboard_definition_yaml = file("${path.module}/dashboards/example-dashboard.yaml")
}
```

Loading from separate YAML files provides several benefits:
- Better separation of concerns
- Dashboard definitions can be edited by users who are not familiar with Terraform
- Better version control for dashboard configurations
- IDE support for syntax highlighting and validation of YAML files


## Complete Example

Here's a complete example that sets up a dashboard using separate YAML files.

Project structure:
```
.
├── main.tf
└── dashboards/
    └── system-overview.yaml
```

### main.tf
```hcl
terraform {
  required_providers {
    dash0 = {
      source  = "dash0/dash0"
      version = "~> 0.1"
    }
  }
}

provider "dash0" {
  # No configuration needed in the provider block
  # Authentication is handled via environment variables
}

# Create a dashboard by loading its definition from a YAML file
resource "dash0_dashboard" "system_overview" {
  name        = "system-overview"
  description = "System overview dashboard with key metrics"
  dataset     = "default"  # Optional, defaults to "default" if not specified
  
  # Load the dashboard definition from a local YAML file
  dashboard_definition_yaml = file("${path.module}/dashboards/system-overview.yaml")
}

```

### dashboards/system-overview.yaml
```yaml
kind: Dashboard
metadata:
  name: system-overview
spec:
  panels:
    - kind: Panel
      spec:
        display:
          name: CPU Usage
        plugin:
          kind: TimeSeriesChart
          spec:
            queries:
              - kind: PrometheusTimeSeriesQuery
                spec:
                  query: "avg by (instance) (irate(node_cpu_seconds_total{mode!='idle'}[5m]) * 100)"
    - kind: Panel
      spec:
        display:
          name: Memory Usage
        plugin:
          kind: TimeSeriesChart
          spec:
            queries:
              - kind: PrometheusTimeSeriesQuery
                spec:
                  query: "node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes * 100"
```


## Importing Existing Resources

### Importing Dashboards

```shell
terraform import dash0_dashboard.example example-dashboard
```


## Development

For development instructions and guidelines, please see the [README.md](README.md) file.