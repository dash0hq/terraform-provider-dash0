#!/usr/bin/env bash
# Roundtrip test for dash0_synthetic_check.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_synthetic_check ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create synthetic check
# ---------------------------------------------------------------------------
info "Step 1: Creating synthetic check via Terraform..."

cat > "${WORK_DIR}/synthetic_check.yaml" <<'YAMLEOF'
kind: Dash0SyntheticCheck
metadata:
  name: roundtrip-test-check
  labels: {}
spec:
  enabled: true
  notifications:
    channels: []
  plugin:
    display:
      name: roundtrip-test.example.com
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
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_synthetic_check" "test" {
  dataset              = var.dataset
  synthetic_check_yaml = file("${path.module}/synthetic_check.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_synthetic_check.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created synthetic check with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying synthetic check exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 synthetic-checks get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find synthetic check ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test" \
  || fail "CLI output does not contain expected synthetic check name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/synthetic_check.yaml" "$CLI_OUTPUT" \
  || fail "Uploaded and downloaded synthetic check YAMLs are not equivalent"
info "Synthetic check equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating synthetic check (changing interval and display name)..."

cat > "${WORK_DIR}/synthetic_check.yaml" <<'YAMLEOF'
kind: Dash0SyntheticCheck
metadata:
  name: roundtrip-test-check
  labels: {}
spec:
  enabled: true
  notifications:
    channels: []
  plugin:
    display:
      name: roundtrip-test-UPDATED.example.com
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
    interval: 10m
    locations:
      - de-frankfurt
      - us-oregon
    strategy: all_locations
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Synthetic check updated."

CLI_OUTPUT="$(dash0 synthetic-checks get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "UPDATED" \
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
info "Step 5: Destroying synthetic check via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Synthetic check destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying synthetic check is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_synthetic_check roundtrip test PASSED ==="
