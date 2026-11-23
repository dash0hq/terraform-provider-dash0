#!/usr/bin/env bash
# Roundtrip test for dash0_aws_log_group_policy (one policy per AWS integration).
# The dash0 CLI in the test image has no command for this asset, so the API is called via `dash0 -X api`.
# Needs DASH0_AWS_INTEGRATION_ID; the integration's original policy is restored at the end.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
DESTROYED=0
MARKER="never-matches-$(date +%s)-$$"

aws_lgp_preflight "dash0_aws_log_group_policy roundtrip" "${WORK_DIR}/original.json"

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

info "=== Roundtrip test: dash0_aws_log_group_policy ==="
info "Working directory: ${WORK_DIR}"
info "AWS integration: ${AWS_INTEGRATION_ID}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create via Terraform. Every value matches no real log group.
# ---------------------------------------------------------------------------
info "Step 1: Creating log-group policy via Terraform..."

cat > "${WORK_DIR}/policy.yaml" <<YAMLEOF
apiVersion: dash0.com/v1alpha1
kind: Dash0AwsLogGroupInstrumentationPolicy
spec:
  onNoLongerMatching: keep
  rules:
    - conditions:
        - key: dash0.cloud.tags.dash0-roundtrip
          operator: is
          value: ${MARKER}
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<TFEOF
variable "disposition" {
  type    = string
  default = "keep"
}

resource "dash0_aws_log_group_policy" "test" {
  integration_id            = "${AWS_INTEGRATION_ID}"
  deletion_disposition      = var.disposition
  aws_log_group_policy_yaml = file("\${path.module}/policy.yaml")
}

output "origin" {
  value = dash0_aws_log_group_policy.test.origin
}
TFEOF

tf_init "$WORK_DIR"
tf_apply "$WORK_DIR"

ORIGIN="$(tf_output "$WORK_DIR" origin)"
info "Created policy with origin: ${ORIGIN}"
[[ "$ORIGIN" == tf_* ]] || fail "Expected a tf_ origin, got '${ORIGIN}'"

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying policy via the API..."

API_OUTPUT="$(aws_lgp_get)" || fail "API could not read the policy"
echo "$API_OUTPUT"
assert_yaml_equivalent "${WORK_DIR}/policy.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded policy YAMLs are not equivalent"
SERVER_ORIGIN="$(aws_lgp_field "(doc['metadata'].get('labels') or {}).get('dash0.com/origin', '')")"
[[ "$SERVER_ORIGIN" == "$ORIGIN" ]] || fail "Expected server origin '${ORIGIN}', got '${SERVER_ORIGIN}'"
info "Policy equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update (add a second, ORed rule)
# ---------------------------------------------------------------------------
info "Step 3: Updating policy (add a rule)..."

cat >> "${WORK_DIR}/policy.yaml" <<YAMLEOF
    - conditions:
        - key: dash0.cloud.tags.dash0-roundtrip-env
          operator: is_one_of
          values: [${MARKER}-a, ${MARKER}-b]
YAMLEOF

tf_apply "$WORK_DIR"
assert_yaml_equivalent "${WORK_DIR}/policy.yaml" "$(aws_lgp_get)" \
  || fail "Updated and downloaded policy YAMLs are not equivalent"
[[ "$(tf_output "$WORK_DIR" origin)" == "$ORIGIN" ]] || fail "Origin changed on update"
info "Update verified via the API."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Drift made outside Terraform shows in the plan and apply reverts it.
# ---------------------------------------------------------------------------
info "Step 5: Changing the policy out-of-band..."

aws_lgp_get | python3 -c "
import json, sys
doc = json.load(sys.stdin)
doc['spec']['rules'] = doc['spec']['rules'][:1]
json.dump(doc, sys.stdout)
" > "${WORK_DIR}/drift.json"
aws_lgp_put "${WORK_DIR}/drift.json" >/dev/null || fail "Out-of-band PUT failed"

set +e
tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_RC=$?
set -e
[[ "$PLAN_RC" -eq 2 ]] || fail "Expected the plan to show drift (exit 2), got exit ${PLAN_RC}"
info "Drift detected."

tf_apply "$WORK_DIR"
assert_yaml_equivalent "${WORK_DIR}/policy.yaml" "$(aws_lgp_get)" \
  || fail "Apply did not revert the out-of-band change"
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Changing only deletion_disposition stays in state; the policy's
# revision must not move.
# ---------------------------------------------------------------------------
info "Step 6: Switching deletion_disposition..."
REVISION_BEFORE="$(aws_lgp_field "(doc['metadata'].get('labels') or {}).get('dash0.com/version', '')")"
TF_VAR_disposition=remove tf_apply "$WORK_DIR"
TF_VAR_disposition=keep tf_apply "$WORK_DIR"
REVISION_AFTER="$(aws_lgp_field "(doc['metadata'].get('labels') or {}).get('dash0.com/version', '')")"
[[ "$REVISION_BEFORE" == "$REVISION_AFTER" ]] \
  || fail "Changing only deletion_disposition wrote the policy (revision ${REVISION_BEFORE} -> ${REVISION_AFTER})"
info "Disposition-only change made no API write."

# ---------------------------------------------------------------------------
# Step 7: Destroy with the default disposition keeps forwarding.
# ---------------------------------------------------------------------------
info "Step 7: Destroying policy via Terraform..."
tf_destroy "$WORK_DIR"
DESTROYED=1
assert_aws_lgp_deleted_with keep
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_aws_log_group_policy roundtrip test PASSED ==="
