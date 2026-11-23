#!/usr/bin/env bash
# Roundtrip test for `terraform import` of dash0_aws_log_group_policy (import ID = AWS integration id).
# The dash0 CLI in the test image has no command for this asset, so the API is called via `dash0 -X api`.
# Needs DASH0_AWS_INTEGRATION_ID; the integration's original policy is restored at the end.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
DESTROYED=0
MARKER="never-matches-$(date +%s)-$$"

aws_lgp_preflight "dash0_aws_log_group_policy import roundtrip" "${WORK_DIR}/original.json"

cleanup() {
  if [[ "$DESTROYED" -eq 0 && -f "${WORK_DIR}/terraform.tfstate" ]]; then
    tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
  fi
  local restored=1
  # One retry, then fail the test: a green run must leave the integration as it found it.
  aws_lgp_restore "${WORK_DIR}/original.json" || aws_lgp_restore "${WORK_DIR}/original.json" || restored=0
  rm -rf "$WORK_DIR"
  [[ "$restored" -eq 1 ]] || fail "Could not restore the original log-group policy of ${AWS_INTEGRATION_ID}"
}
trap cleanup EXIT

info "=== Roundtrip test: terraform import (dash0_aws_log_group_policy) ==="
info "Working directory: ${WORK_DIR}"
info "AWS integration: ${AWS_INTEGRATION_ID}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Write a policy via the API (out-of-band). The server stamps an
# api-<uuid> origin for a machine token, so this imports a policy owned by
# another API client.
# ---------------------------------------------------------------------------
info "Step 1: Writing policy via the API..."
aws_lgp_fixture_json "$MARKER" > "${WORK_DIR}/create.json"
aws_lgp_put "${WORK_DIR}/create.json" >/dev/null || fail "Failed to write policy via the API"
SERVER_ORIGIN="$(aws_lgp_field "(doc['metadata'].get('labels') or {}).get('dash0.com/origin', '')")"
info "Policy written via the API (origin: '${SERVER_ORIGIN}')."

# ---------------------------------------------------------------------------
# Step 2: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 2: Exporting YAML via the API + writing Terraform config..."

aws_lgp_get | python3 -c "
import sys, yaml
doc = yaml.safe_load(sys.stdin.read())
doc['metadata'] = {}
sys.stdout.write(yaml.safe_dump(doc, sort_keys=False))
" > "${WORK_DIR}/policy.yaml" || fail "Failed to export policy YAML"

cat > "${WORK_DIR}/main.tf" <<TFEOF
resource "dash0_aws_log_group_policy" "imported" {
  integration_id            = "${AWS_INTEGRATION_ID}"
  aws_log_group_policy_yaml = file("\${path.module}/policy.yaml")
}

output "origin" {
  value = dash0_aws_log_group_policy.imported.origin
}
TFEOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 3: terraform import with the integration id.
# ---------------------------------------------------------------------------
info "Step 3: Importing via terraform import (integration id)..."
tf_import "$WORK_DIR" "dash0_aws_log_group_policy.imported" "$AWS_INTEGRATION_ID" \
  || fail "terraform import failed"

# ---------------------------------------------------------------------------
# Step 4: Assert plan reports no changes. The imported deletion_disposition
# is the default keep, matching the configuration.
# ---------------------------------------------------------------------------
info "Step 4: Asserting terraform plan reports no changes after import..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: The imported origin is the server's, not a new tf_ one.
# ---------------------------------------------------------------------------
info "Step 5: Verifying origin preservation in state..."
STATE_ORIGIN="$(tf_output "$WORK_DIR" origin || true)"
[[ "$STATE_ORIGIN" == "$SERVER_ORIGIN" ]] \
  || fail "Expected imported origin '${SERVER_ORIGIN}', got '${STATE_ORIGIN}'"
info "Origin preservation check PASSED."

# ---------------------------------------------------------------------------
# Step 6: Modify + apply.
# ---------------------------------------------------------------------------
info "Step 6: Modifying + applying to prove imported resource is manageable..."

cat >> "${WORK_DIR}/policy.yaml" <<YAMLEOF
  - conditions:
    - key: dash0.cloud.tags.dash0-roundtrip-env
      operator: is_set
    - key: dash0.cloud.tags.dash0-roundtrip
      operator: starts_with
      value: ${MARKER}-
YAMLEOF

tf_apply "$WORK_DIR"
assert_yaml_equivalent "${WORK_DIR}/policy.yaml" "$(aws_lgp_get)" \
  || fail "API output does not reflect the post-import update"
info "Update-after-import verified via the API."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 7: Destroy + verify the policy is deleted, keeping forwarding.
# ---------------------------------------------------------------------------
info "Step 7: Destroying imported policy via Terraform..."
tf_destroy "$WORK_DIR"
DESTROYED=1
assert_aws_lgp_deleted_with keep

info "=== dash0_aws_log_group_policy import roundtrip test PASSED ==="
