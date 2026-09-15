resource "dash0_time_series_aggregation" "http_server_request_duration" {
  dataset                      = "default"
  time_series_aggregation_yaml = file("${path.module}/time_series_aggregation.yaml")
}
