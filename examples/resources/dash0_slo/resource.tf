resource "dash0_slo" "checkout_availability" {
  dataset  = "default"
  slo_yaml = file("${path.module}/slo.yaml")
}

# Inline OpenSLO v1 document. SLOs are Private BETA and support a constrained
# subset of OpenSLO: a single objective, an inline `ratioMetric` indicator,
# `Occurrences` budgeting, and a rolling 28d (4w) window.
resource "dash0_slo" "checkout_latency" {
  dataset = "default"

  slo_yaml = <<-YAML
apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: checkout-latency
  annotations:
    dash0.com/display-name: Checkout latency
    dash0.com/enabled: "true"
spec:
  description: 99 percent of checkout HTTP requests complete under 500ms over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-latency-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_bucket{service_name="checkout",le="0.5"}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99% under 500ms
      target: 0.99
YAML
}
