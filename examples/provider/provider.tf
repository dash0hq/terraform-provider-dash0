provider "dash0" {
  # Configuration can be provided via environment variables:
  # DASH0_URL and DASH0_AUTH_TOKEN
}

resource "dash0_dashboard" "my_dashboard" {
  dataset        = "default"
  dashboard_yaml = file("${path.module}/dashboard.yaml")
}
