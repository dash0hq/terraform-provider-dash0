#!/usr/bin/env bash
# Roundtrip test for dash0_recording_rule_group.
#
# Note: There is no dedicated CLI subcommand for recording rule groups,
# so we use `dash0 apply -f <file> --dataset <ds>` for verification and
# the Terraform state output for the origin.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_recording_rule_group ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create recording rule group
# ---------------------------------------------------------------------------
info "Step 1: Creating recording rule group via Terraform..."

cat > "${WORK_DIR}/recording_rule_group.yaml" <<'YAMLEOF'
kind: Dash0RecordingRuleGroup
metadata:
  name: roundtrip-test-rrg
  annotations:
    "dash0.com/folder-path": "/test/roundtrip"
spec:
  enabled: true
  display:
    name: Roundtrip Test Recording Rules
  interval: 1m
  rules:
    - record: test_metric:rate5m
      expression: rate(test_metric_total[5m])
      labels:
        env: test
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_recording_rule_group" "test" {
  dataset                   = var.dataset
  recording_rule_group_yaml = file("${path.module}/recording_rule_group.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_recording_rule_group.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created recording rule group with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI (dry-run apply to validate the resource exists)
# ---------------------------------------------------------------------------
info "Step 2: Verifying recording rule group exists..."

# We verify by re-reading the Terraform state — there is no dedicated CLI
# get command for recording rule groups. We do a plan to confirm state is
# consistent.
set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_EXIT=$?
set -e

if [[ "$PLAN_EXIT" -eq 0 ]]; then
  info "Recording rule group verified (state is consistent, resource exists)."
elif [[ "$PLAN_EXIT" -eq 2 ]]; then
  warn "Plan shows drift — the resource may have been modified externally."
else
  fail "Plan failed with exit code ${PLAN_EXIT}."
fi

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating recording rule group (adding a second rule)..."

cat > "${WORK_DIR}/recording_rule_group.yaml" <<'YAMLEOF'
kind: Dash0RecordingRuleGroup
metadata:
  name: roundtrip-test-rrg
  annotations:
    "dash0.com/folder-path": "/test/roundtrip"
spec:
  enabled: true
  display:
    name: Roundtrip Test Recording Rules - Updated
  interval: 1m
  rules:
    - record: test_metric:rate5m
      expression: rate(test_metric_total[5m])
      labels:
        env: test
    - record: test_errors:rate5m
      expression: rate(test_errors_total[5m])
      labels:
        env: test
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Recording rule group updated."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."

set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
EXIT_CODE=$?
set -e

if [[ "$EXIT_CODE" -eq 0 ]]; then
  info "Idempotency check PASSED: no changes detected."
elif [[ "$EXIT_CODE" -eq 2 ]]; then
  fail "Idempotency check FAILED: Terraform detected changes on a no-op re-apply."
else
  fail "Idempotency check ERROR: tofu plan exited with code ${EXIT_CODE}."
fi

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying recording rule group via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Recording rule group destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying recording rule group is deleted..."

# Re-import should fail if the resource was truly deleted.
# We attempt a plan with a fresh config referencing the same origin to
# confirm it no longer exists.

VERIFY_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR" "$VERIFY_DIR"' EXIT

write_provider_tf "$VERIFY_DIR"
cat > "${VERIFY_DIR}/main.tf" <<EOF
import {
  to = dash0_recording_rule_group.test
  id = "${DATASET},${ORIGIN}"
}

resource "dash0_recording_rule_group" "test" {
  dataset                   = "${DATASET}"
  recording_rule_group_yaml = "placeholder"
}
EOF

tf_init "$VERIFY_DIR"

set +e
(cd "$VERIFY_DIR" && TF_CLI_CONFIG_FILE="${VERIFY_DIR}/.terraformrc" $TF plan -input=false 2>&1)
IMPORT_EXIT=$?
set -e

if [[ "$IMPORT_EXIT" -ne 0 ]]; then
  info "Recording rule group confirmed deleted (import failed as expected)."
else
  fail "Recording rule group ${ORIGIN} still appears to exist after destroy!"
fi

info "=== dash0_recording_rule_group roundtrip test PASSED ==="
