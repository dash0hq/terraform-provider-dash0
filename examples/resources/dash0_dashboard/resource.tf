resource "dash0_dashboard" "my_dashboard" {
  dataset        = "default"
  dashboard_yaml = file("${path.module}/dashboard.yaml")
}

