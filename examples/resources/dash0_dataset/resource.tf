# Manage a Dash0 dataset declaratively. A dataset is an isolated slice of
# telemetry with its own retention, privacy, and ingestion settings.
# Datasets are organization-level resources (no dataset scope).
#
# metadata.labels."dash0.com/id" is the dataset slug. It is required and
# cannot be changed after creation; changing it destroys and re-creates the
# dataset.
#
# Destroying the resource soft-deletes the dataset. Dashboards, views, check
# rules, synthetic checks, recording rules, SLOs and time series aggregations
# are kept for 30 days; applying the same slug within that time restores them.
# Spam filters are not restored. After 30 days the dataset, its permissions and
# those assets are removed. Telemetry is removed as its retention expires.
#
# spec.permissions replaces the user and team grants on the dataset. Omit it to
# leave the grants as they are. A grant cannot include dataset:write, and an
# empty list does not remove grants.
resource "dash0_dataset" "staging" {
  dataset_yaml = <<-YAML
apiVersion: dash0.com/v1alpha1
kind: Dash0Dataset
metadata:
  name: Staging
  labels:
    dash0.com/id: staging
spec:
  retention:
    traces: 30d
    logs: 60d
  privacy:
    ipAddresses:
      endUser:
        storage: anonymize
    geoLocation:
      endUser:
        storage: up_to_country
    redaction:
      cardholderData:
        enabled: true
      genAi:
        conversation: redact
        toolCalls: disabled
        errors: disabled
  permissions:
    # teamId is the id of a team, for example dash0_team.backend.id.
    - teamId: 00000000-0000-0000-0000-000000000000
      actions: [dataset:read, dataset:delete]
    - userId: user_01ABCDEF
      actions: [dataset:read]
YAML
}

# The dataset slug, for use as the `dataset` of other resources.
output "staging_dataset_slug" {
  value = dash0_dataset.staging.id
}

# You can also load the YAML definition from a file:
#
# resource "dash0_dataset" "staging" {
#   dataset_yaml = file("${path.module}/datasets/staging.yaml")
# }
