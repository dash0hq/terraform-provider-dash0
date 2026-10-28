#!/usr/bin/env bash
# Roundtrip test for `terraform import` of dash0_aws_metric_scrape_config (import ID = AWS integration id).
# The dash0 CLI has no command for this asset, so the API is called via `dash0 -X api`.
# Needs DASH0_AWS_INTEGRATION_ID; the integration's original config is restored at the end.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

aws_scrape_preflight "dash0_aws_metric_scrape_config import roundtrip"

WORK_DIR="$(mktemp -d)"
DESTROYED=0

cleanup() {
  if [[ "$DESTROYED" -eq 0 && -f "${WORK_DIR}/terraform.tfstate" ]]; then
    tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
  fi
  if [[ -f "${WORK_DIR}/original.json" ]]; then
    aws_scrape_restore "${WORK_DIR}/original.json" || warn "Could not restore the original scrape config of ${AWS_INTEGRATION_ID}"
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: terraform import (dash0_aws_metric_scrape_config) ==="
info "Working directory: ${WORK_DIR}"
info "AWS integration: ${AWS_INTEGRATION_ID}"

aws_scrape_save "${WORK_DIR}/original.json" || fail "Could not read the scrape config of ${AWS_INTEGRATION_ID}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Write a config via the API (out-of-band), without an origin, like
# the Dash0 UI does.
# ---------------------------------------------------------------------------
info "Step 1: Writing scrape config via the API..."

cat > "${WORK_DIR}/create.json" <<'JSONEOF'
{
  "apiVersion": "dash0.com/v1alpha1",
  "kind": "Dash0AwsMetricScrapeConfig",
  "metadata": {},
  "spec": {
    "namespaces": [
      { "namespace": "AWS/Lambda", "metrics": ["Invocations", "Errors"] }
    ]
  }
}
JSONEOF
aws_scrape_put "${WORK_DIR}/create.json" || fail "Failed to write scrape config via the API"
SERVER_ORIGIN="$(aws_scrape_field "(doc['metadata'].get('labels') or {}).get('dash0.com/origin', '')")"
info "Scrape config written via the API (origin: '${SERVER_ORIGIN}')."

# ---------------------------------------------------------------------------
# Step 2: Export current YAML + write resource shell. The identifier is the
# integration id itself, so there is nothing to discover.
# ---------------------------------------------------------------------------
info "Step 2: Exporting YAML via the API + writing Terraform config..."

aws_scrape_get | python3 -c "
import sys, yaml
doc = yaml.safe_load(sys.stdin.read())
doc['metadata'] = {}
sys.stdout.write(yaml.safe_dump(doc, sort_keys=False))
" > "${WORK_DIR}/scrape-config.yaml" || fail "Failed to export scrape config YAML"

cat > "${WORK_DIR}/main.tf" <<EOF
resource "dash0_aws_metric_scrape_config" "imported" {
  integration_id                = "${AWS_INTEGRATION_ID}"
  aws_metric_scrape_config_yaml = file("\${path.module}/scrape-config.yaml")
}

output "origin" {
  value = dash0_aws_metric_scrape_config.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 3: terraform import with the integration id.
# ---------------------------------------------------------------------------
info "Step 3: Importing via terraform import (integration id)..."
tf_import "$WORK_DIR" "dash0_aws_metric_scrape_config.imported" "$AWS_INTEGRATION_ID" \
  || fail "terraform import failed"

# ---------------------------------------------------------------------------
# Step 4: Assert plan reports no changes.
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

cat >> "${WORK_DIR}/scrape-config.yaml" <<'YAMLEOF'
  - namespace: AWS/SQS
    metrics:
    - NumberOfMessagesSent
YAMLEOF

tf_apply "$WORK_DIR"
API_OUTPUT="$(aws_scrape_get)"
assert_yaml_equivalent "${WORK_DIR}/scrape-config.yaml" "$API_OUTPUT" \
  || fail "API output does not reflect the post-import update"
info "Update-after-import verified via the API."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 7: Destroy + verify the config is cleared.
# ---------------------------------------------------------------------------
info "Step 7: Destroying imported scrape config via Terraform..."
tf_destroy "$WORK_DIR"
DESTROYED=1
assert_aws_scrape_cleared

info "=== dash0_aws_metric_scrape_config import roundtrip test PASSED ==="
