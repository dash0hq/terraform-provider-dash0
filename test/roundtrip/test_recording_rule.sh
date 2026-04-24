#!/usr/bin/env bash
# Roundtrip test for dash0_recording_rule.
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

info "=== Roundtrip test: dash0_recording_rule ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Write provider configuration
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create recording rule
# ---------------------------------------------------------------------------
info "Step 1: Creating recording rule via Terraform..."

cat > "${WORK_DIR}/recording_rule.yaml" <<'YAMLEOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: roundtrip-test-recording
spec:
  groups:
    - name: RoundtripRecordingTest
      interval: 1m0s
      rules:
        - record: job:http_requests_total:rate5m
          expr: sum by (job) (rate(http_requests_total[5m]))
          labels:
            env: roundtrip-test
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_recording_rule" "test" {
  dataset            = var.dataset
  recording_rule_yaml = file("${path.module}/recording_rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_recording_rule.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created recording rule with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying recording rule exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 recording-rules get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find recording rule ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -q "job:http_requests_total:rate5m" \
  || fail "CLI output does not contain expected recording rule metric name"

info "Step 2b: Verifying YAML equivalence..."
assert_yaml_equivalent "${WORK_DIR}/recording_rule.yaml" "$CLI_OUTPUT"
info "YAML equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update and re-apply
# ---------------------------------------------------------------------------
info "Step 3: Updating recording rule (changing metric name and expression)..."

cat > "${WORK_DIR}/recording_rule.yaml" <<'YAMLEOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: roundtrip-test-recording
spec:
  groups:
    - name: RoundtripRecordingTest
      interval: 1m0s
      rules:
        - record: job:http_requests_total:rate10m
          expr: sum by (job) (rate(http_requests_total[10m]))
          labels:
            env: roundtrip-test-updated
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Recording rule updated."

# Verify updated values via CLI
CLI_OUTPUT="$(dash0 recording-rules get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "rate10m" \
  || fail "CLI output does not reflect the update"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 4: Idempotency — re-apply without changes
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying recording rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Recording rule destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion via CLI
# ---------------------------------------------------------------------------
info "Step 6: Verifying recording rule is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_recording_rule roundtrip test PASSED ==="
