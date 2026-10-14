#!/usr/bin/env bash
# Roundtrip test for dash0_dataset (org-scoped, identified by slug).
# The dash0 CLI has no `datasets` command, so the API is called via `dash0 -X api`.
# Destroy is a soft delete (restorable for 30 days), so each run uses a unique slug.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
SLUG="rt-ds-$(date +%s)-${RANDOM}"
DESTROYED=0

# Destroy on early failure so aborted runs do not leave a live dataset behind.
cleanup() {
  if [[ "$DESTROYED" -eq 0 && -f "${WORK_DIR}/terraform.tfstate" ]]; then
    TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: dash0_dataset ==="
info "Working directory: ${WORK_DIR}"
info "Dataset slug: ${SLUG}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create dataset via Terraform
# ---------------------------------------------------------------------------
info "Step 1: Creating dataset via Terraform..."

cat > "${WORK_DIR}/dataset.yaml" <<YAMLEOF
kind: Dash0Dataset
metadata:
  name: Roundtrip Test Dataset
  labels:
    dash0.com/id: ${SLUG}
spec:
  tracing:
    retention: 30d
  logging:
    retention: 30d
  geoLocation:
    endUserGeoLocationStorageStrategy: up_to_country
  ipAddresses:
    endUserIpAddressStorageStrategy: anonymize
  cardholderDataRedaction:
    enabled: true
  genAiAttributeRedaction:
    conversation: redact
    toolCalls: disabled
    errors: disabled
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_dataset" "test" {
  dataset_yaml = file("${path.module}/dataset.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_dataset.test.origin
}

output "id" {
  value = dash0_dataset.test.id
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
info "Created dataset with origin: ${ORIGIN}, id: ${ID}"

[[ "$ID" == "$SLUG" ]] || fail "Expected id '${SLUG}' (the slug), got '${ID}'"

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying dataset exists via the API..."

API_OUTPUT="$(dataset_get "$SLUG" 2>&1)" \
  || fail "API could not find dataset ${SLUG}"
echo "$API_OUTPUT"

echo "$API_OUTPUT" | grep -q "Roundtrip Test Dataset" \
  || fail "API output does not contain expected dataset name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/dataset.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded dataset YAMLs are not equivalent"
info "Dataset equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating dataset (name, geolocation, IP storage, redaction)..."

cat > "${WORK_DIR}/dataset.yaml" <<YAMLEOF
kind: Dash0Dataset
metadata:
  name: Roundtrip Test Dataset (Updated)
  labels:
    dash0.com/id: ${SLUG}
spec:
  tracing:
    retention: 30d
  logging:
    retention: 30d
  geoLocation:
    endUserGeoLocationStorageStrategy: do_not_store
  ipAddresses:
    endUserIpAddressStorageStrategy: do_not_store
  cardholderDataRedaction:
    enabled: false
  genAiAttributeRedaction:
    conversation: disabled
    toolCalls: redact
    errors: disabled
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Dataset updated."

# ---------------------------------------------------------------------------
# Step 4: Verify update via the API
# ---------------------------------------------------------------------------
info "Step 4: Verifying update via the API..."

API_OUTPUT="$(dataset_get "$SLUG" 2>&1)" \
  || fail "API could not find dataset ${SLUG} after update"
echo "$API_OUTPUT" | grep -q "Roundtrip Test Dataset (Updated)" \
  || fail "API output does not reflect the name update"
assert_yaml_equivalent "${WORK_DIR}/dataset.yaml" "$API_OUTPUT" \
  || fail "Updated and downloaded dataset YAMLs are not equivalent"
info "Update verified via the API."

# ---------------------------------------------------------------------------
# Step 5: Idempotency
# ---------------------------------------------------------------------------
info "Step 5: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Destroy
# ---------------------------------------------------------------------------
info "Step 6: Destroying dataset via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
DESTROYED=1
info "Dataset destroyed (soft-deleted)."

# ---------------------------------------------------------------------------
# Step 7: Verify deletion
# ---------------------------------------------------------------------------
info "Step 7: Verifying dataset is gone..."
assert_dataset_deleted "$SLUG"
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_dataset roundtrip test PASSED ==="
