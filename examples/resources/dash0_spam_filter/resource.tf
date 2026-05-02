resource "dash0_spam_filter" "drop_health_checks" {
  dataset = "default"

  spam_filter_yaml = <<-EOF
apiVersion: operator.dash0.com/v1alpha1
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
