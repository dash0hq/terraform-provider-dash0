# Manage a Dash0 auth token declaratively. Auth tokens are organization-level
# credentials for telemetry ingestion and the Dash0 API.
#
# Managing auth tokens requires an admin user or a token with the `*` action
# and no dataset or signal restriction.
#
# Do not set spec.token. Dash0 generates the secret; read it from the
# sensitive `token` attribute. The secret is stored in the Terraform state in
# plain text.
resource "dash0_auth_token" "ingest" {
  auth_token_yaml = <<-YAML
apiVersion: dash0.com/v1alpha1
kind: Dash0AuthToken
metadata:
  name: Production ingestion
spec:
  # ingest:* is the "Ingesting" option in the Dash0 UI.
  actions:
    - ingest:*
  # Limit the signals with scope.signals (the "Signal types" field in the UI).
  scope:
    signals:
      restriction: restricted
      types:
        - logs
        - metricDataPoints
        - spans
YAML
}

# The secret, for example to configure an OpenTelemetry Collector.
output "ingest_token" {
  value     = dash0_auth_token.ingest.token
  sensitive = true
}

# To rotate the secret without downtime, add a second dash0_auth_token
# resource, move the consumers to its secret, and remove the old resource in a
# later apply.

# You can also load the YAML definition from a file:
#
# resource "dash0_auth_token" "ingest" {
#   auth_token_yaml = file("${path.module}/auth-tokens/ingest.yaml")
# }
