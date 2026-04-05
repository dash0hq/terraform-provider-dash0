#!/usr/bin/env bash
# Roundtrip test for dash0_dashboard.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_dashboard ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create dashboard
# ---------------------------------------------------------------------------
info "Step 1: Creating dashboard via Terraform..."

# Write the initial dashboard YAML
cat > "${WORK_DIR}/dashboard.yaml" <<'DASHEOF'
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: roundtrip-test-dashboard
  labels: {}
spec:
  duration: "30m"
  display:
    description: "Roundtrip test dashboard"
    name: "Roundtrip Test Dashboard"
  layouts:
    - kind: Grid
      spec:
        items:
          - content:
              $ref: "#/spec/panels/panel-1"
            height: 8
            width: 24
            x: 0
            "y": 0
  panels:
    panel-1:
      kind: Panel
      spec:
        display:
          description: ""
          name: "Test Panel"
        plugin:
          kind: TimeSeriesChart
          spec:
            legend:
              position: bottom
        queries:
          - kind: TimeSeriesQuery
            spec:
              plugin:
                kind: PrometheusTimeSeriesQuery
                spec:
                  query: "up"
DASHEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_dashboard" "test" {
  dataset        = var.dataset
  dashboard_yaml = file("${path.module}/dashboard.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_dashboard.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created dashboard with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying dashboard exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 dashboards get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find dashboard ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test-dashboard\|Roundtrip Test Dashboard" \
  || fail "CLI output does not contain expected dashboard name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/dashboard.yaml" "$CLI_OUTPUT" \
  || fail "Uploaded and downloaded dashboard YAMLs are not equivalent"
info "Dashboard equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update dashboard
# ---------------------------------------------------------------------------
info "Step 3: Updating dashboard (changing display name and description)..."

cat > "${WORK_DIR}/dashboard.yaml" <<'DASHEOF'
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: roundtrip-test-dashboard
  labels: {}
spec:
  duration: "1h"
  display:
    description: "UPDATED roundtrip test dashboard"
    name: "Roundtrip Test Dashboard - Updated"
  layouts:
    - kind: Grid
      spec:
        items:
          - content:
              $ref: "#/spec/panels/panel-1"
            height: 8
            width: 24
            x: 0
            "y": 0
  panels:
    panel-1:
      kind: Panel
      spec:
        display:
          description: ""
          name: "Updated Test Panel"
        plugin:
          kind: TimeSeriesChart
          spec:
            legend:
              position: bottom
        queries:
          - kind: TimeSeriesQuery
            spec:
              plugin:
                kind: PrometheusTimeSeriesQuery
                spec:
                  query: "up"
DASHEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Dashboard updated."

CLI_OUTPUT="$(dash0 dashboards get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "Updated\|UPDATED" \
  || fail "CLI output does not reflect the update"
info "Update verified via CLI."

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
info "Step 5: Destroying dashboard via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Dashboard destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying dashboard is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_dashboard roundtrip test PASSED ==="
