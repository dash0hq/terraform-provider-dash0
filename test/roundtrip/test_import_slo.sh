#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_slo.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# an SLO is first created out-of-band via the dash0 CLI (representing a UI- or
# CLI-created asset), then adopted into Terraform state via `terraform import`
# using the `dataset,identifier` ID format. After import, `terraform plan`
# must report no changes.
#
# SLO identifiers live at `.metadata.labels["dash0.com/origin"]` (falling
# back to `dash0.com/id` on UI-created assets, matching spam filters, views,
# and synthetic checks).
#
# Steps:
#   1. Create SLO via dash0 CLI (non-Terraform origin)
#   2. Discover identifier from labels
#   3. Export YAML via dash0 CLI + write Terraform resource shell
#   4. terraform import
#   5. Assert plan reports no changes
#   6. Verify identifier preservation in state
#   7. Modify + apply — prove the imported resource is manageable
#   8. Destroy + verify server-side deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_slo) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create SLO via dash0 CLI (out-of-band, no Terraform).
# ---------------------------------------------------------------------------
info "Step 1: Creating SLO via dash0 CLI..."

cat > "${WORK_DIR}/slo.yaml" <<'YAMLEOF'
apiVersion: openslo.com/v1
kind: SLO
metadata:
  name: roundtrip-import-slo
  annotations:
    dash0.com/enabled: "true"
spec:
  description: 99 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99% availability
      target: 0.99
YAMLEOF

dash0 slos create -f "${WORK_DIR}/slo.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create SLO via dash0 CLI"
info "SLO created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 slos list --dataset "$DATASET" -o json --limit 500 \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    if it.get('metadata', {}).get('name') == 'roundtrip-import-slo':
        labels = it.get('metadata', {}).get('labels', {}) or {}
        print(labels.get('dash0.com/origin') or labels.get('dash0.com/id') or '')
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for roundtrip-import-slo"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created SLO, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 slos get "$IDENTIFIER" --dataset "$DATASET" -o yaml > "${WORK_DIR}/slo.yaml" \
  || fail "Failed to export SLO YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_slo" "imported" {
  dataset  = var.dataset
  slo_yaml = file("${path.module}/slo.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_slo.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_slo.imported" "${DATASET},${IDENTIFIER}" \
  || fail "terraform import failed"
info "Import completed."

# ---------------------------------------------------------------------------
# Step 5: Assert plan reports no changes.
# ---------------------------------------------------------------------------
info "Step 5: Asserting terraform plan reports no changes after import..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Identifier preservation.
# ---------------------------------------------------------------------------
info "Step 6: Verifying identifier preservation in state..."
STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
if [[ "$STATE_ORIGIN" != "$IDENTIFIER" ]]; then
  fail "Expected imported origin '${IDENTIFIER}' in state, got '${STATE_ORIGIN}'"
fi
if [[ "$STATE_ORIGIN" == tf_* ]]; then
  fail "Imported origin '${STATE_ORIGIN}' unexpectedly carries the tf_ prefix (would indicate re-anchoring)"
fi
info "Identifier preservation check PASSED."

# ---------------------------------------------------------------------------
# Step 7: Modify + apply — mutate the objective target. `spec.objectives[]` is
# a first-class field the provider manages and the CLI surfaces verbatim.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/slo.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["objectives"][0]["target"] = 0.995
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 slos get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "0.995" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported SLO via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
# The get endpoint has a longer cache TTL than list; use list (which the test
# already relies on in step 2) for prompt post-destroy verification.
gone=""
for i in $(seq 1 10); do
  set +e
  dash0 slos list --dataset "$DATASET" -o json --limit 500 \
    | python3 -c "
import json, sys
items = json.load(sys.stdin)
target = '$IDENTIFIER'
sys.exit(0 if any((it.get('metadata', {}).get('labels', {}) or {}).get('dash0.com/origin') == target or (it.get('metadata', {}).get('labels', {}) or {}).get('dash0.com/id') == target for it in items) else 1)
"
  found=$?
  set -e
  if [[ $found -ne 0 ]]; then
    info "Server-side deletion confirmed via list (attempt ${i})."
    gone="yes"
    break
  fi
  if [[ $i -lt 10 ]]; then
    warn "SLO still in list (attempt ${i}/10), retrying in 3s..."
    sleep 3
  fi
done
[[ "$gone" == "yes" ]] || fail "SLO '${IDENTIFIER}' still returned by list after 10 attempts"

info "=== dash0_slo import roundtrip test PASSED ==="
