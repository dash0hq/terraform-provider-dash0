terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "0.0.4"
    }
  }
}

provider "dash0" {
  # Configuration can be provided via environment variables:
  # DASH0_URL and DASH0_AUTH_TOKEN
}

resource "dash0_dashboard" "system_overview" {
  dataset        = "default"
  dashboard_yaml = file("${path.module}/dashboards/system-overview.yaml")
}

