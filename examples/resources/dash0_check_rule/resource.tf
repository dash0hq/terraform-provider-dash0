resource "dash0_check_rule" "adservice_error_rate" {
  dataset = "production"

  # Currently only one group incl. one rule is supported
  check_rule_yaml = <<-EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: adservice
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: adservice
          expr: (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != "", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_namespace, service_name) (increase({otel_metric_name = "dash0.spans", service_name = "adservice", service_namespace = "opentelemetry-demo", dash0_operation_name != ""}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            description: 'High error percentage for adservice: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "40"
            dash0-threshold-degraded: "35"
            dash0-enabled: true
          labels: {}
EOF
}