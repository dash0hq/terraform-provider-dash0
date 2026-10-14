# Manage a Dash0 dataset declaratively. A dataset is an isolated slice of
# telemetry with its own retention, privacy, and ingestion settings.
# Datasets are organization-level resources (no dataset scope).
#
# metadata.labels."dash0.com/id" is the dataset slug. It is required and
# cannot be changed after creation; changing it destroys and re-creates the
# dataset.
#
# Destroying the resource soft-deletes the dataset. After 30 days, the dataset
# and all of its telemetry are purged. Re-creating a dataset with the same
# slug within 30 days restores it.
resource "dash0_dataset" "staging" {
  dataset_yaml = <<-YAML
kind: Dash0Dataset
metadata:
  name: Staging
  labels:
    dash0.com/id: staging
spec:
  tracing:
    retention: 30d
  logging:
    retention: 60d
  geoLocation:
    endUserGeoLocationStorageStrategy: up_to_country
  ipAddresses:
    endUserIpAddressStorageStrategy: anonymize
  cardholderDataRedaction:
    enabled: true
  genAiAttributeRedaction:
    conversation: redact
    toolCalls: disabled
    errors: disabled
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
