resource "dash0_recording_rule" "span_duration_p95" {
  dataset = "production"

  recording_rule_yaml = <<-EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: span-duration-aggregations
spec:
  groups:
    - name: SpanDurationAggregations
      interval: 1m0s
      rules:
        - record: service_name:dash0_spans_duration:p95_5m
          expr: histogram_quantile(0.95, sum by (le, service_name) (rate({otel_metric_name="dash0.spans.duration"}[5m])))
          labels:
            quantile: "0.95"
EOF
}
