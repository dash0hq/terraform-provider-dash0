terraform {
  required_providers {
    dash0 = {
      source  = "dash0/dash0"
      version = "0.0.1"
    }
  }
}

provider "dash0" {
  # Configuration can be provided via environment variables:
  # DASH0_URL and DASH0_AUTH_TOKEN
}

# Create a dashboard by loading its definition from a YAML file
resource "dash0_dashboard" "system_overview" {
  dataset     = "default"  # Optional, defaults to "default" if not specified

  # Load the dashboard definition from a local YAML file
  dashboard_definition_yaml = file("${path.module}/dashboards/system-overview.yaml")
}

