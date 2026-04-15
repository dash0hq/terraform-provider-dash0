#!/usr/bin/env bash
# Targeted test: does the server add keep_firing_for when only `for` is set?
#
# Creates a check rule with `for: 5m0s` and NO `keep_firing_for`, then checks
# if the plan detects drift (i.e., the server enriched the response).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Test: does the server add keep_firing_for? ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Write provider configuration
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create check rule with for: 5m0s but NO keep_firing_for
# ---------------------------------------------------------------------------
info "Step 1: Creating check rule with for: 5m0s (no keep_firing_for)..."

cat > "${WORK_DIR}/check_rule.yaml" <<'YAMLEOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: keep-firing-for-test
spec:
  groups:
    - name: KeepFiringForTest
      interval: 1m0s
      rules:
        - alert: keep-firing-for-test-alert
          expr: vector(1) > 0
          for: 5m0s
          annotations:
            summary: "Test alert for keep_firing_for idempotency"
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
# Step 2: Idempotency — does re-plan show changes?
# ---------------------------------------------------------------------------
info "Step 2: Re-planning to check if server added keep_firing_for..."

set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
EXIT_CODE=$?
set -e

if [[ "$EXIT_CODE" -eq 0 ]]; then
  info "Idempotency check PASSED: server did NOT add keep_firing_for."
elif [[ "$EXIT_CODE" -eq 2 ]]; then
  warn "Idempotency check FAILED: server likely added keep_firing_for or other fields."
  # Show the plan diff for debugging
  TF_VAR_dataset="$DATASET" \
    bash -c "cd '$WORK_DIR' && TF_CLI_CONFIG_FILE='${WORK_DIR}/.terraformrc' tofu plan -input=false" || true
else
  fail "tofu plan exited with unexpected code ${EXIT_CODE}."
fi

# ---------------------------------------------------------------------------
# Step 3: Cleanup
# ---------------------------------------------------------------------------
info "Step 3: Destroying check rule..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

if [[ "$EXIT_CODE" -eq 2 ]]; then
  fail "Server adds keep_firing_for — this is an idempotency bug."
fi

info "=== keep_firing_for test PASSED ==="
