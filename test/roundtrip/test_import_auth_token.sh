#!/usr/bin/env bash
# Roundtrip test for `terraform import` of dash0_auth_token (by id and by origin).
# No auth-token CLI command exists, so the API is called via `dash0 -X api`; needs an admin or `*` token.
# The test token sits beside the caller's token and only it is deleted, so the last-token refusal cannot hit.
# The secret (spec.token) is never printed. The API returns it only on create.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

auth_token_preflight "dash0_auth_token import roundtrip"

WORK_DIR="$(mktemp -d)"
ORIGIN="rt-import-$(date +%s)-${RANDOM}"
NAME="Roundtrip Import Token ${RANDOM}"
ID=""
DESTROYED=0

# Destroy on early failure so aborted runs do not leave a live token behind.
# Before the import (no state yet) the test token is deleted through the API.
cleanup() {
  if [[ "$DESTROYED" -eq 0 ]]; then
    if [[ -f "${WORK_DIR}/terraform.tfstate" ]]; then
      TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
    elif [[ -n "$ID" ]]; then
      dash0 -X api DELETE "/api/auth-tokens/${ID}" --dataset "" >/dev/null 2>&1 || true
    fi
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: terraform import (dash0_auth_token) ==="
info "Working directory: ${WORK_DIR}"
info "Origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create auth token via the API (out-of-band).
# A PUT to an unknown origin creates the token.
# ---------------------------------------------------------------------------
info "Step 1: Creating auth token via the API..."

cat > "${WORK_DIR}/create.json" <<JSONEOF
{
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0AuthToken",
  "metadata": {
    "name": "${NAME}",
    "labels": { "dash0.com/origin": "${ORIGIN}" }
  },
  "spec": {
    "actions": ["views:read"]
  }
}
JSONEOF

CREATE_OUTPUT="$(dash0 -X api PUT "/api/auth-tokens/${ORIGIN}" --dataset "" -f "${WORK_DIR}/create.json")" \
  || fail "Failed to create auth token via the API"

# ---------------------------------------------------------------------------
# Step 2: Discover the id from the create response (the secret is not read).
# ---------------------------------------------------------------------------
info "Step 2: Discovering the id..."

ID="$(printf '%s' "$CREATE_OUTPUT" | python3 -c "
import json, sys
print(json.load(sys.stdin)['metadata']['labels']['dash0.com/id'])
")"
[[ -n "$ID" ]] || fail "Could not discover id for auth token ${ORIGIN}"
info "Auth token created via the API: ${ID}"

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via the API + writing Terraform config..."

# auth_token_get already drops the secret; drop server-managed labels and annotations too.
auth_token_get "$ID" \
  | python3 -c "
import sys, yaml
doc = yaml.safe_load(sys.stdin.read())
doc['metadata'].pop('annotations', None)
doc['metadata'].pop('labels', None)
sys.stdout.write(yaml.safe_dump(doc, sort_keys=False))
" > "${WORK_DIR}/auth_token.yaml" \
  || fail "Failed to export auth token YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_auth_token" "imported" {
  auth_token_yaml = file("${path.module}/auth_token.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_auth_token.imported.origin
}

output "id" {
  value = dash0_auth_token.imported.id
}

output "token" {
  value     = dash0_auth_token.imported.token
  sensitive = true
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import by origin, then by id into a fresh state.
# Auth tokens are organization-scoped: no dataset prefix.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import (origin, no dataset)..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_auth_token.imported" "$ORIGIN" \
  || fail "terraform import by origin failed"
info "Step 4b: Asserting terraform plan reports no changes after import by origin..."
assert_idempotent "$WORK_DIR"

info "Step 4c: Re-importing by id into a fresh state..."
rm -f "${WORK_DIR}"/terraform.tfstate*
TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_auth_token.imported" "$ID" \
  || fail "terraform import by id failed"
info "Import completed."

# ---------------------------------------------------------------------------
# Step 5: Assert plan reports no changes.
# ---------------------------------------------------------------------------
info "Step 5: Asserting terraform plan reports no changes after import by id..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Identifier preservation and null secret.
# ---------------------------------------------------------------------------
info "Step 6: Verifying id and origin in state, and a null secret..."
STATE_ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
[[ "$STATE_ID" == "$ID" ]] || fail "Expected imported id '${ID}' in state, got '${STATE_ID}'"
STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
[[ "$STATE_ORIGIN" == "$ORIGIN" ]] \
  || fail "Expected imported origin '${ORIGIN}' in state, got '${STATE_ORIGIN}'"
# The API returns the secret only on create, so an imported token has a null secret.
STATE_SECRET="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" token || true)"
[[ -z "$STATE_SECRET" ]] || fail "Expected a null token after import, got a value"
info "Identifier check PASSED, imported token is null."

# ---------------------------------------------------------------------------
# Step 7: Modify + apply.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/auth_token.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["metadata"]["name"] = "updated-after-import"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

API_OUTPUT="$(auth_token_get "$ID" 2>&1)"
echo "$API_OUTPUT" | grep -q "updated-after-import" \
  || fail "API output does not reflect the post-import update"
info "Update-after-import verified via the API."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy (only the test token) + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported auth token via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
DESTROYED=1

info "Step 8b: Verifying server-side deletion..."
assert_auth_token_deleted "$ID"

info "=== dash0_auth_token import roundtrip test PASSED ==="
