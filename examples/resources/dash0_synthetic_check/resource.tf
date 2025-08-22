resource "dash0_synthetic_check" "my_check" {
  dataset              = "default"
  synthetic_check_yaml = file("${path.module}/synthetic_check.yaml")
}