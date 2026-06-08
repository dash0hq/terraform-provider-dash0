#!/usr/bin/env bash
# Roundtrip test for dash0_notification_channel.
#
# Steps:
#   1. Create the resource via Terraform
#   2. Verify it exists via dash0 CLI
#   3. Update a field and re-apply via Terraform
#   4. Re-apply without changes (idempotency)
#   5. Destroy the resource via Terraform
#   6. Verify deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
WORK_DIR2="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR" "$WORK_DIR2"' EXIT

info "=== Roundtrip test: dash0_notification_channel ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create notification channel
# ---------------------------------------------------------------------------
info "Step 1: Creating notification channel via Terraform..."

cat > "${WORK_DIR}/notification_channel.yaml" <<'YAMLEOF'
kind: Dash0NotificationChannel
metadata:
  name: roundtrip-test-channel
spec:
  type: webhook
  config:
    url: https://example.com/webhook/roundtrip-test
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_notification_channel" "test" {
  notification_channel_yaml = file("${path.module}/notification_channel.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_notification_channel.test.origin
}

output "id" {
  value = dash0_notification_channel.test.id
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created notification channel with origin: ${ORIGIN}"

# Verify the computed id attribute is a bare UUID, distinct from the origin's
# UUID (origin is tf_<uuid>). Synthetic checks and check rules reference the
# channel by this id, not by the origin.
ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
info "Notification channel id: ${ID}"
echo "$ID" | grep -Eq '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' \
  || fail "notification channel id output '${ID}' is not a bare UUID"
[ "tf_${ID}" != "$ORIGIN" ] \
  || fail "notification channel id unexpectedly equals the origin UUID"
info "Notification channel id check PASSED."

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying notification channel exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 -X notification-channels get "$ORIGIN" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find notification channel ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test" \
  || fail "CLI output does not contain expected notification channel name"

# The resolved id must match the server-assigned dash0.com/id in the download.
echo "$CLI_OUTPUT" | grep -q "$ID" \
  || fail "CLI output does not contain the resolved channel id ${ID}"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/notification_channel.yaml" "$CLI_OUTPUT" \
  || fail "Uploaded and downloaded notification channel YAMLs are not equivalent"
info "Notification channel equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating notification channel (changing URL and name)..."

cat > "${WORK_DIR}/notification_channel.yaml" <<'YAMLEOF'
kind: Dash0NotificationChannel
metadata:
  name: roundtrip-test-channel-UPDATED
spec:
  type: webhook
  config:
    url: https://example.com/webhook/roundtrip-test-updated
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Notification channel updated."

CLI_OUTPUT="$(dash0 -X notification-channels get "$ORIGIN" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "UPDATED" \
  || fail "CLI output does not reflect the update"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying notification channel via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Notification channel destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying notification channel is gone..."
assert_deleted_via_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 7: Channel referenced by a synthetic check (SUP-1178)
#
# Regression test: a synthetic check referencing the channel by its `id`
# attribute must have that id stored under spec.notifications.channels (the
# value the backend resolves channels by) and must not drift.
# ---------------------------------------------------------------------------
info "Step 7: Verifying a synthetic check can reference the channel by id..."

write_provider_tf "$WORK_DIR2"

cat > "${WORK_DIR2}/main.tf" <<'EOF'
resource "dash0_notification_channel" "test" {
  notification_channel_yaml = <<-YAML
kind: Dash0NotificationChannel
metadata:
  name: roundtrip-test-channel-ref
spec:
  type: webhook
  config:
    url: https://example.com/webhook/roundtrip-ref
YAML
}

resource "dash0_synthetic_check" "test" {
  dataset = var.dataset
  synthetic_check_yaml = <<-YAML
kind: Dash0SyntheticCheck
metadata:
  name: roundtrip-test-check-ref
  labels: {}
spec:
  enabled: true
  notifications:
    channels:
      - ${dash0_notification_channel.test.id}
  plugin:
    display:
      name: roundtrip-ref.example.com
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
        degradedAssertions: []
      request:
        method: get
        url: https://www.example.com
        queryParameters: []
        headers: []
        redirects: follow
        tls:
          allowInsecure: false
        tracing:
          addTracingHeaders: false
  retries:
    kind: fixed
    spec:
      attempts: 2
      delay: 1s
  schedule:
    interval: 5m
    locations:
      - de-frankfurt
    strategy: all_locations
YAML
}

variable "dataset" {
  type = string
}

output "channel_id" {
  value = dash0_notification_channel.test.id
}

output "check_origin" {
  value = dash0_synthetic_check.test.origin
}
EOF

tf_init "$WORK_DIR2"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR2"

CHANNEL_ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR2" channel_id)"
CHECK_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR2" check_origin)"
info "Referenced channel id ${CHANNEL_ID} on synthetic check ${CHECK_ORIGIN}"

CHECK_OUTPUT="$(dash0 synthetic-checks get "$CHECK_ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find synthetic check ${CHECK_ORIGIN}"
echo "$CHECK_OUTPUT"
echo "$CHECK_OUTPUT" | grep -q "$CHANNEL_ID" \
  || fail "synthetic check notifications.channels does not contain the channel id ${CHANNEL_ID}"
info "Channel reference check PASSED."

# Re-apply without changes to confirm the reference does not drift.
assert_idempotent "$WORK_DIR2"

TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR2"
info "Cross-resource reference resources destroyed."

info "=== dash0_notification_channel roundtrip test PASSED ==="
