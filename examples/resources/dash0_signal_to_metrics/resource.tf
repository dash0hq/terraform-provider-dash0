resource "dash0_signal_to_metrics" "checkout_errors" {
  dataset                = "default"
  signal_to_metrics_yaml = file("${path.module}/signal_to_metrics.yaml")
}
