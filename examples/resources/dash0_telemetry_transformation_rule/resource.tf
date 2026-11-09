resource "dash0_telemetry_transformation_rule" "normalize_user_id" {
  dataset                            = "default"
  telemetry_transformation_rule_yaml = file("${path.module}/telemetry_transformation_rule.yaml")
}
