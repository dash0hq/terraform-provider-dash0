#!/usr/bin/env bash
# Roundtrip test for dash0_check_rule.
#
# Steps:
#   1. Create the resource via Terraform
#   2. Verify it exists via dash0 CLI
#   3. Update a field and re-apply via Terraform
#   4. Re-apply without changes (idempotency)
#   5. Destroy the resource via Terraform
#   6. Verify deletion via dash0 CLI

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_check_rule ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Write provider configuration
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create check rule
# ---------------------------------------------------------------------------
info "Step 1: Creating check rule via Terraform..."

cat > "${WORK_DIR}/check_rule.yaml" <<'YAMLEOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: roundtrip-test-check
spec:
  groups:
    - name: RoundtripTest
      interval: 1m0s
      rules:
        - alert: roundtrip-test-alert
          expr: vector(1) > 0
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: "Roundtrip test alert"
            description: "This is a test alert created by roundtrip tests"
            dash0-threshold-critical: "90"
            dash0-threshold-degraded: "80"
            dash0-enabled: true
          labels: {}
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_check_rule" "test" {
  dataset         = var.dataset
  check_rule_yaml = file("${path.module}/check_rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_check_rule.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created check rule with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying check rule exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 check-rules get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find check rule ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -q "roundtrip-test-alert" \
  || fail "CLI output does not contain expected check rule alert name"

# The CLI returns check rules in a flat format (not PrometheusRule YAML), so a
# structural YAML equivalence check is not possible. Verify key fields instead.
info "Step 2b: Verifying key fields in downloaded check rule..."
echo "$CLI_OUTPUT" | grep -q "vector(1) > 0" \
  || fail "CLI output does not contain expected expression"
echo "$CLI_OUTPUT" | grep -q "Roundtrip test alert" \
  || fail "CLI output does not contain expected summary"
echo "$CLI_OUTPUT" | grep -q "90" \
  || fail "CLI output does not contain expected threshold"
info "Check rule field verification PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update and re-apply
# ---------------------------------------------------------------------------
info "Step 3: Updating check rule (changing threshold and description)..."

cat > "${WORK_DIR}/check_rule.yaml" <<'YAMLEOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: roundtrip-test-check
spec:
  groups:
    - name: RoundtripTest
      interval: 1m0s
      rules:
        - alert: roundtrip-test-alert
          expr: vector(1) > 0
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: "Roundtrip test alert - UPDATED"
            description: "This is an UPDATED test alert"
            dash0-threshold-critical: "95"
            dash0-threshold-degraded: "85"
            dash0-enabled: true
          labels: {}
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Check rule updated."

# Verify updated values via CLI
CLI_OUTPUT="$(dash0 check-rules get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "UPDATED" \
  || fail "CLI output does not reflect the update"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 4: Idempotency — re-apply without changes
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
info "Step 5: Destroying check rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Check rule destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion via CLI
# ---------------------------------------------------------------------------
info "Step 6: Verifying check rule is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_check_rule roundtrip test PASSED ==="
