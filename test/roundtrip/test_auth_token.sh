#!/usr/bin/env bash
# Roundtrip test for dash0_auth_token (identified by UUID id or origin).
# No auth-token CLI command exists, so the API is called via `dash0 -X api`; needs an admin or `*` token.
# The test token sits beside the caller's token and only it is deleted, so the last-token refusal cannot hit.
# The secret (spec.token) is never printed. The API returns it only on create.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

auth_token_preflight "dash0_auth_token roundtrip"

WORK_DIR="$(mktemp -d)"
NAME="Roundtrip Test Token ${RANDOM}"
DESTROYED=0

# Destroy on early failure so aborted runs do not leave a live token behind.
# Only the state's own resource (the test token) is destroyed.
cleanup() {
  if [[ "$DESTROYED" -eq 0 && -f "${WORK_DIR}/terraform.tfstate" ]]; then
    TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: dash0_auth_token ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create auth token via Terraform
# ---------------------------------------------------------------------------
info "Step 1: Creating auth token via Terraform..."

cat > "${WORK_DIR}/auth_token.yaml" <<YAMLEOF
apiVersion: dash0.com/v1alpha1
kind: Dash0AuthToken
metadata:
  name: ${NAME}
spec:
  actions:
    - ingest:logs
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_auth_token" "test" {
  auth_token_yaml = file("${path.module}/auth_token.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_auth_token.test.origin
}

output "id" {
  value = dash0_auth_token.test.id
}

output "token" {
  value     = dash0_auth_token.test.token
  sensitive = true
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
info "Created auth token with origin: ${ORIGIN}, id: ${ID}"

[[ "$ORIGIN" == tf_* ]] || fail "Expected a tf_-prefixed origin, got '${ORIGIN}'"
[[ "$ID" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] \
  || fail "Expected a UUID id, got '${ID}'"

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying auth token exists via the API..."

API_OUTPUT="$(auth_token_get "$ID" 2>&1)" \
  || fail "API could not find auth token ${ID}"
echo "$API_OUTPUT"

echo "$API_OUTPUT" | grep -q "$NAME" \
  || fail "API output does not contain expected auth token name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/auth_token.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded auth token YAMLs are not equivalent"
info "Auth token equivalence check PASSED."

info "Step 2c: Checking the secret from the create response is in state..."
STATE_SECRET="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" token)"
[[ -n "$STATE_SECRET" ]] || fail "Sensitive token attribute is empty in state"
# The API returns the secret only on create, so a GET must not carry it.
dash0 -X api "/api/auth-tokens/${ID}" --dataset "" \
  | python3 -c "import json, sys; sys.exit('spec.token' if json.load(sys.stdin).get('spec', {}).get('token') else 0)" \
  || fail "GET returned the secret; expected it on create only"
info "Secret check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update (name and actions in place; id, origin and secret unchanged)
# ---------------------------------------------------------------------------
info "Step 3: Updating auth token (name, actions)..."

cat > "${WORK_DIR}/auth_token.yaml" <<YAMLEOF
apiVersion: dash0.com/v1alpha1
kind: Dash0AuthToken
metadata:
  name: ${NAME} (Updated)
spec:
  actions:
    - ingest:logs
    - ingest:metrics
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Auth token updated."

[[ "$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)" == "$ID" ]] \
  || fail "Auth token id changed on update"
[[ "$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" token)" == "$STATE_SECRET" ]] \
  || fail "Auth token secret changed on update"

# ---------------------------------------------------------------------------
# Step 4: Verify update via the API
# ---------------------------------------------------------------------------
info "Step 4: Verifying update via the API..."

API_OUTPUT="$(auth_token_get "$ID" 2>&1)" \
  || fail "API could not find auth token ${ID} after update"
echo "$API_OUTPUT" | grep -q "${NAME} (Updated)" \
  || fail "API output does not reflect the name update"
assert_yaml_equivalent "${WORK_DIR}/auth_token.yaml" "$API_OUTPUT" \
  || fail "Updated and downloaded auth token YAMLs are not equivalent"
info "Update verified via the API."

# ---------------------------------------------------------------------------
# Step 5: Idempotency
# ---------------------------------------------------------------------------
info "Step 5: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Destroy (only the test token; the caller's token remains)
# ---------------------------------------------------------------------------
info "Step 6: Destroying auth token via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
DESTROYED=1
info "Auth token destroyed."

# ---------------------------------------------------------------------------
# Step 7: Verify deletion
# ---------------------------------------------------------------------------
info "Step 7: Verifying auth token is gone..."
assert_auth_token_deleted "$ID"
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_auth_token roundtrip test PASSED ==="
