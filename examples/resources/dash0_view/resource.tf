resource "dash0_view" "my_check" {
  dataset   = "default"
  view_yaml = file("${path.module}/view.yaml")
}