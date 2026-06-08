resource "dash0_synthetic_check" "my_check" {
  dataset              = "default"
  synthetic_check_yaml = file("${path.module}/synthetic_check.yaml")
}

# Creating notification channels with `for_each`, and linking one of them
# from a synthetic check.
#
# `dash0_notification_channel` exposes a computed `id` attribute (the
# server-assigned UUID, resolved by the provider after creation). The
# synthetic check references that id in `spec.notifications.channels`,
# which requires raw UUIDs rather than the `tf_`-prefixed origin.
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

resource "dash0_synthetic_check" "checkout_api" {
  dataset = "default"

  synthetic_check_yaml = <<-YAML
kind: Dash0SyntheticCheck
metadata:
  name: checkout-api
spec:
  enabled: true
  notifications:
    channels:
      - ${dash0_notification_channel.team_oncall["sre"].id}
  plugin:
    display:
      name: checkout-api
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
      request:
        method: get
        url: https://api.example.com/health
        queryParameters: []
        headers: []
        redirects: follow
        tls:
          allowInsecure: false
        tracing:
          addTracingHeaders: true
  retries:
    kind: fixed
    spec:
      attempts: 3
      delay: 1s
  schedule:
    interval: 1m
    locations:
      - de-frankfurt
      - us-oregon
    strategy: all_locations
YAML
}
