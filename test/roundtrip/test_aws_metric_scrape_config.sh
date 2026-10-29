#!/usr/bin/env bash
# Roundtrip test for dash0_aws_metric_scrape_config (one config per AWS integration).
# The dash0 CLI has no command for this asset, so the API is called via `dash0 -X api`.
# Needs DASH0_AWS_INTEGRATION_ID; the integration's original config is restored at the end.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

aws_scrape_preflight "dash0_aws_metric_scrape_config roundtrip"

WORK_DIR="$(mktemp -d)"
DESTROYED=0

cleanup() {
  if [[ "$DESTROYED" -eq 0 && -f "${WORK_DIR}/terraform.tfstate" ]]; then
    tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
  fi
  local restored=1
  if [[ -f "${WORK_DIR}/original.json" ]]; then
    # One retry, then fail the test: a green run must leave the integration as it found it.
    aws_scrape_restore "${WORK_DIR}/original.json" || aws_scrape_restore "${WORK_DIR}/original.json" || restored=0
  fi
  rm -rf "$WORK_DIR"
  [[ "$restored" -eq 1 ]] || fail "Could not restore the original scrape config of ${AWS_INTEGRATION_ID}"
}
trap cleanup EXIT

info "=== Roundtrip test: dash0_aws_metric_scrape_config ==="
info "Working directory: ${WORK_DIR}"
info "AWS integration: ${AWS_INTEGRATION_ID}"

aws_scrape_save "${WORK_DIR}/original.json" || fail "Could not read the scrape config of ${AWS_INTEGRATION_ID}"
# The server keeps an existing dash0.com/origin on PUT and only takes a new one
# after the config was cleared, so start from a cleared config.
aws_scrape_clear || fail "Could not clear the scrape config of ${AWS_INTEGRATION_ID}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create via Terraform. Namespaces and metrics are listed in the order
# the server returns them, so assert_yaml_equivalent can compare positionally.
# ---------------------------------------------------------------------------
info "Step 1: Creating scrape config via Terraform..."

cat > "${WORK_DIR}/scrape-config.yaml" <<'YAMLEOF'
apiVersion: dash0.com/v1alpha1
kind: Dash0AwsMetricScrapeConfig
spec:
  namespaces:
    - namespace: AWS/Lambda
      metrics:
        - Errors
        - Invocations
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<EOF
resource "dash0_aws_metric_scrape_config" "test" {
  integration_id                = "${AWS_INTEGRATION_ID}"
  aws_metric_scrape_config_yaml = file("\${path.module}/scrape-config.yaml")
}

output "origin" {
  value = dash0_aws_metric_scrape_config.test.origin
}
EOF

tf_init "$WORK_DIR"
tf_apply "$WORK_DIR"

ORIGIN="$(tf_output "$WORK_DIR" origin)"
info "Created scrape config with origin: ${ORIGIN}"
[[ "$ORIGIN" == tf_* ]] || fail "Expected a tf_ origin, got '${ORIGIN}'"

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying scrape config via the API..."

API_OUTPUT="$(aws_scrape_get)" || fail "API could not read the scrape config"
echo "$API_OUTPUT"
assert_yaml_equivalent "${WORK_DIR}/scrape-config.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded scrape config YAMLs are not equivalent"

SERVER_LABELS="$(aws_scrape_field "doc['metadata']['labels'].get('dash0.com/origin', '') + '|' + doc['metadata']['labels'].get('dash0.com/source', '')")"
[[ "$SERVER_LABELS" == "${ORIGIN}|terraform" ]] \
  || fail "Expected origin|source '${ORIGIN}|terraform', got '${SERVER_LABELS}'"
info "Scrape config equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating scrape config (add a namespace)..."

cat >> "${WORK_DIR}/scrape-config.yaml" <<'YAMLEOF'
    - namespace: AWS/SQS
      metrics:
        - ApproximateAgeOfOldestMessage
        - NumberOfMessagesSent
YAMLEOF

tf_apply "$WORK_DIR"

API_OUTPUT="$(aws_scrape_get)" || fail "API could not read the scrape config after update"
assert_yaml_equivalent "${WORK_DIR}/scrape-config.yaml" "$API_OUTPUT" \
  || fail "Updated and downloaded scrape config YAMLs are not equivalent"
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
info "Step 5: Changing the config out-of-band..."

aws_scrape_get | python3 -c "
import json, sys
doc = json.load(sys.stdin)
doc['spec']['namespaces'] = [ns for ns in doc['spec']['namespaces'] if ns['namespace'] != 'AWS/SQS']
json.dump(doc, sys.stdout)
" > "${WORK_DIR}/drift.json"
aws_scrape_put "${WORK_DIR}/drift.json" || fail "Out-of-band PUT failed"

set +e
tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_RC=$?
set -e
[[ "$PLAN_RC" -eq 2 ]] || fail "Expected the plan to show drift (exit 2), got exit ${PLAN_RC}"
info "Drift detected."

tf_apply "$WORK_DIR"
assert_yaml_equivalent "${WORK_DIR}/scrape-config.yaml" "$(aws_scrape_get)" \
  || fail "Apply did not revert the out-of-band change"
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5b: An empty namespaces list stops scraping and stays in state. The
# server drops the origin for such a config, which Read must not take for a
# config cleared outside Terraform.
# ---------------------------------------------------------------------------
info "Step 5b: Applying an empty namespaces list..."

cat > "${WORK_DIR}/scrape-config.yaml" <<'YAMLEOF'
apiVersion: dash0.com/v1alpha1
kind: Dash0AwsMetricScrapeConfig
spec:
  namespaces: []
YAMLEOF

tf_apply "$WORK_DIR"
assert_aws_scrape_cleared
assert_idempotent "$WORK_DIR"
info "Empty namespaces list stays in state."

# ---------------------------------------------------------------------------
# Step 6: Destroy
# ---------------------------------------------------------------------------
info "Step 6: Destroying scrape config via Terraform..."
tf_destroy "$WORK_DIR"
DESTROYED=1

# ---------------------------------------------------------------------------
# Step 7: Verify the config is cleared
# ---------------------------------------------------------------------------
info "Step 7: Verifying scrape config is cleared..."
assert_aws_scrape_cleared
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_aws_metric_scrape_config roundtrip test PASSED ==="
