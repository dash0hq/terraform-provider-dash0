resource "dash0_spam_filter" "drop_health_checks" {
  dataset = "default"

  spam_filter_yaml = <<-EOF
apiVersion: v1alpha1
kind: Dash0SpamFilter
metadata:
  name: Drop noisy health checks
spec:
  contexts:
    - log
  filter:
    - key: "k8s.namespace.name"
      operator: "is"
      value: "kube-system"
EOF
}

# v1alpha2 uses a single `context` instead of a `contexts` list.
resource "dash0_spam_filter" "drop_debug_logs" {
  dataset = "default"

  spam_filter_yaml = <<-EOF
apiVersion: v1alpha2
kind: Dash0SpamFilter
metadata:
  name: Drop debug logs
spec:
  context: log
  filter:
    - key: "severity_text"
      operator: "is"
      value: "DEBUG"
EOF
}
