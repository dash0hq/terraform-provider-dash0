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

# Fanning out check rules with `for_each` and routing each rule's alerts to one
# or more notification channels through the `dash0.com/notification-channel-ids`
# annotation. The annotation takes a comma-separated list of channel UUIDs —
# the `id` attribute on `dash0_notification_channel`, not the `origin`.
resource "dash0_notification_channel" "team_oncall" {
  for_each = {
    backend  = "backend-oncall@example.com"
    frontend = "frontend-oncall@example.com"
    sre      = "sre-oncall@example.com"
  }

  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: ${each.key} on-call
spec:
  type: email_v2
  config:
    recipients:
      - ${each.value}
    plaintext: false
  frequency: 10m
YAML
}

locals {
  service_check_rules = {
    backend-api = {
      service  = "backend-api"
      channels = ["backend", "sre"]
    }
    frontend-web = {
      service  = "frontend-web"
      channels = ["frontend"]
    }
    checkout = {
      service  = "checkout"
      channels = ["backend", "sre"]
    }
  }
}

resource "dash0_check_rule" "service_error_rate" {
  for_each = local.service_check_rules

  dataset = "production"

  check_rule_yaml = <<-EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: ${each.key}-error-rate
  annotations:
    dash0.com/notification-channel-ids: ${join(",", [for c in each.value.channels : dash0_notification_channel.team_oncall[c].id])}
spec:
  groups:
    - name: Alerting
      interval: 1m0s
      rules:
        - alert: ${each.value.service}-error-rate
          expr: (sum by (service_name) (increase({otel_metric_name = "dash0.spans", service_name = "${each.value.service}", otel_span_status_code = "ERROR"}[5m]))) / (sum by (service_name) (increase({otel_metric_name = "dash0.spans", service_name = "${each.value.service}"}[5m])) > 0)*100 > $__threshold
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: 'High error percentage for ${each.value.service}: {{$value|printf "%.2f"}}%'
            dash0-threshold-critical: "40"
            dash0-threshold-degraded: "35"
            dash0-enabled: true
          labels: {}
EOF
}
