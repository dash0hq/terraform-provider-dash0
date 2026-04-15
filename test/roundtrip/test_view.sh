#!/usr/bin/env bash
# Roundtrip test for dash0_view.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_view ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create view
# ---------------------------------------------------------------------------
info "Step 1: Creating view via Terraform..."

cat > "${WORK_DIR}/view.yaml" <<YAMLEOF
kind: Dash0View
metadata:
  name: roundtrip-test-view
  labels:
    "dash0.com/dataset": "${DATASET}"
spec:
  display:
    name: Roundtrip Test View
    folder: []
  type: spans
  permissions:
    - actions:
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
  filter:
    - key: service.name
      operator: is
      value: roundtrip-test-service
  table:
    columns:
      - colSize: minmax(auto, 2fr)
        key: dash0.span.name
        label: Name
      - colSize: min-content
        key: service.name
        label: Service
      - colSize: 8.5rem
        key: otel.span.duration
        label: Duration
    sort:
      - direction: ascending
        key: otel.span.duration
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_view" "test" {
  dataset   = var.dataset
  view_yaml = file("${path.module}/view.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_view.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created view with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying view exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 views get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find view ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test-view\|Roundtrip Test View" \
  || fail "CLI output does not contain expected view name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/view.yaml" "$CLI_OUTPUT" \
  || fail "Uploaded and downloaded view YAMLs are not equivalent"
info "View equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating view (changing display name and adding a column)..."

cat > "${WORK_DIR}/view.yaml" <<YAMLEOF
kind: Dash0View
metadata:
  name: roundtrip-test-view
  labels:
    "dash0.com/dataset": "${DATASET}"
spec:
  display:
    name: Roundtrip Test View - Updated
    folder: []
  type: spans
  permissions:
    - actions:
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
  filter:
    - key: service.name
      operator: is
      value: roundtrip-test-service
  table:
    columns:
      - colSize: minmax(auto, 2fr)
        key: dash0.span.name
        label: Name
      - colSize: min-content
        key: service.name
        label: Service
      - colSize: 8.5rem
        key: otel.span.duration
        label: Duration
      - colSize: min-content
        key: otel.span.start_time
        label: Start Time
    sort:
      - direction: descending
        key: otel.span.duration
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "View updated."

CLI_OUTPUT="$(dash0 views get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "Updated" \
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
info "Step 5: Destroying view via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "View destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying view is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_view roundtrip test PASSED ==="
