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
trap 'rm -rf "$WORK_DIR"' EXIT

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
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created notification channel with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying notification channel exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 -X notification-channels get "$ORIGIN" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find notification channel ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test" \
  || fail "CLI output does not contain expected notification channel name"

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

info "=== dash0_notification_channel roundtrip test PASSED ==="
