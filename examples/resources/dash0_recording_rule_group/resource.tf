resource "dash0_recording_rule_group" "http_metrics" {
  dataset                   = "default"
  recording_rule_group_yaml = file("${path.module}/recording_rule_group.yaml")
}
