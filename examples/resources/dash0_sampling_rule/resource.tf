resource "dash0_sampling_rule" "keep_production_errors" {
  dataset            = "default"
  sampling_rule_yaml = file("${path.module}/sampling_rule.yaml")
}
