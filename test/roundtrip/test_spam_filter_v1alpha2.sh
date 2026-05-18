#!/usr/bin/env bash
# Roundtrip test for dash0_spam_filter (v1alpha2 shape).
#
# v1alpha2 uses a single `spec.context` scalar (one signal type) instead of
# the v1alpha1 `spec.contexts` list.
#
# Steps:
#   1. Create the resource via Terraform
#   2. Verify it exists via dash0 CLI
#   3. Update a field and re-apply via Terraform
#   4. Re-apply without changes (idempotency)
#   5. Destroy the resource via Terraform
#   6. Verify deletion via Terraform

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_spam_filter (v1alpha2) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Write provider configuration
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create spam filter
# ---------------------------------------------------------------------------
info "Step 1: Creating v1alpha2 spam filter via Terraform..."

cat > "${WORK_DIR}/spam_filter.yaml" <<'YAMLEOF'
apiVersion: v1alpha2
kind: Dash0SpamFilter
metadata:
  name: Drop debug logs
  annotations:
    dash0.com/enabled: "true"
spec:
  context: log
  filter:
    - key: "severity_text"
      operator: "is"
      value: "DEBUG"
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_spam_filter" "test" {
  dataset          = var.dataset
  spam_filter_yaml = file("${path.module}/spam_filter.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_spam_filter.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created v1alpha2 spam filter with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying spam filter exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 -X spam-filters get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find spam filter ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -q "DEBUG" \
  || fail "CLI output does not contain expected spam filter content"

info "Step 2b: Verifying YAML equivalence..."
assert_yaml_equivalent "${WORK_DIR}/spam_filter.yaml" "$CLI_OUTPUT"
info "YAML equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update and re-apply
# ---------------------------------------------------------------------------
info "Step 3: Updating v1alpha2 spam filter (adding a second filter condition)..."

cat > "${WORK_DIR}/spam_filter.yaml" <<'YAMLEOF'
apiVersion: v1alpha2
kind: Dash0SpamFilter
metadata:
  name: Drop debug logs (updated)
  annotations:
    dash0.com/enabled: "true"
spec:
  context: log
  filter:
    - key: "severity_text"
      operator: "is"
      value: "DEBUG"
    - key: "service.name"
      operator: "starts_with"
      value: "noisy-"
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "v1alpha2 spam filter updated."

# Verify updated values via CLI
CLI_OUTPUT="$(dash0 -X spam-filters get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "noisy-" \
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
info "Step 5: Destroying v1alpha2 spam filter via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "v1alpha2 spam filter destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion via Terraform
# ---------------------------------------------------------------------------
info "Step 6: Verifying v1alpha2 spam filter is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_spam_filter (v1alpha2) roundtrip test PASSED ==="
