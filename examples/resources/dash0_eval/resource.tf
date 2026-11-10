resource "dash0_eval" "answer_relevance" {
  dataset   = "default"
  eval_yaml = file("${path.module}/eval.yaml")
}
