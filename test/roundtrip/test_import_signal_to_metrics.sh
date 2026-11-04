#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_signal_to_metrics.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a signal-to-metrics rule is first created out-of-band through the API
# (representing a UI- or API-created asset), then adopted into Terraform state
# via `terraform import` using the `dataset,identifier` ID format. After
# import, `terraform plan` must report no changes.
#
# The dash0 CLI has no signal-to-metrics command, so every read and the
# out-of-band create go through the raw API command (`dash0 -X api`).
#
# Steps:
#   0. Preflight — the credential must be allowed to manage signal-to-metrics rules
#   1. Create the rule via the API (non-Terraform origin)
#   2. Discover the identifier from the list response
#   3. Export its YAML via the API and write the Terraform resource shell
#   4. `terraform import` with the `dataset,identifier` ID
#   5. `terraform plan` reports no changes
#   6. The imported origin is preserved in state (no fresh tf_ prefix)
#   7. Modify the YAML + apply, twice — the imported resource is manageable,
#      and the stale dash0.com/version label the exported YAML carries does
#      not make the second apply fail
#   8. Destroy + verify server-side deletion
#   9. Importing the deleted rule is refused (deletes are soft, so a GET still
#      finds the tombstone)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_signal_to_metrics) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# Step 0a: Preflight — see signal_to_metrics_preflight in common.sh for why this can skip.
info "Step 0a: Preflight — checking signal-to-metrics API access..."
signal_to_metrics_preflight "terraform import of a dash0_signal_to_metrics"

# ---------------------------------------------------------------------------
# Step 0b: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

SUFFIX="$(date +%s)-${RANDOM}"
OOB_ORIGIN="roundtrip-import-signal-to-metrics-${SUFFIX}"
DISPLAY_NAME="Roundtrip Import Test Signal To Metrics ${SUFFIX}"

# ---------------------------------------------------------------------------
# Step 1: Create the rule via the API (out-of-band, no Terraform). A non-`tf_`
# origin is used so step 6's origin-preservation assertion is meaningful. The
# rule is disabled and its filter matches no span, so it never produces
# metric data points.
# ---------------------------------------------------------------------------
info "Step 1: Creating signal-to-metrics rule via the API (origin ${OOB_ORIGIN})..."

cat > "${WORK_DIR}/create.json" <<JSONEOF
{
  "kind": "Dash0SignalToMetrics",
  "metadata": {"name": "roundtrip-import-signal-to-metrics"},
  "spec": {
    "enabled": false,
    "display": {"name": "${DISPLAY_NAME}"},
    "match": {
      "signal": "spans",
      "filters": [{"key": "service.name", "operator": "is", "value": "dash0-terraform-roundtrip-matches-nothing"}]
    },
    "output": {"name": "dash0_terraform_roundtrip_import_matches_nothing", "interval": "60s"}
  }
}
JSONEOF

dash0 -X api PUT "/api/signal-to-metrics/${OOB_ORIGIN}" --dataset "$DATASET" -f "${WORK_DIR}/create.json" >/dev/null \
  || fail "Failed to create signal-to-metrics rule via the API"
info "Rule created via the API."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the list response. Signal-to-metrics
# identifiers live at `.metadata.labels["dash0.com/origin"]`, falling back to
# `.metadata.labels["dash0.com/id"]` when the origin label is absent.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via the API..."

# The display name is passed as argv, not interpolated into the program text,
# so a quote in it surfaces as a clear assertion failure.
IDENTIFIER="$(dash0 -X api "/api/signal-to-metrics" --dataset "$DATASET" \
  | python3 -c '
import json, sys
data = json.load(sys.stdin)
items = data.get("signalToMetrics", []) if isinstance(data, dict) else data
target = sys.argv[1]
for it in items:
    if (it.get("spec", {}).get("display") or {}).get("name") == target:
        labels = it.get("metadata", {}).get("labels", {}) or {}
        print(labels.get("dash0.com/origin") or labels.get("dash0.com/id") or "")
        break
' "$DISPLAY_NAME")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for '${DISPLAY_NAME}'"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from an API-created rule, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export the current document from Dash0 (so terraform plan sees no
# diff after import) and write the resource shell. JSON is valid YAML, so the
# API response is used as the signal_to_metrics_yaml verbatim — server labels,
# dash0.com/version included.
# ---------------------------------------------------------------------------
info "Step 3: Exporting the rule via the API + writing Terraform config..."

signal_to_metrics_get "$IDENTIFIER" "$DATASET" > "${WORK_DIR}/signal_to_metrics.yaml" \
  || fail "Failed to export the signal-to-metrics rule"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_signal_to_metrics" "imported" {
  dataset                = var.dataset
  signal_to_metrics_yaml = file("${path.module}/signal_to_metrics.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_signal_to_metrics.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_signal_to_metrics.imported" "${DATASET},${IDENTIFIER}" \
  || fail "terraform import failed"
info "Import completed."

# ---------------------------------------------------------------------------
# Step 5: `terraform plan` must report no changes.
# ---------------------------------------------------------------------------
info "Step 5: Asserting terraform plan reports no changes after import..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Origin preservation.
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
# Step 7: Modify + apply, twice. The exported YAML carries the
# dash0.com/version label of the moment it was exported. The API rejects a
# stale version with 400, so without the provider stripping that label the
# second apply would fail.
# ---------------------------------------------------------------------------
# set_interval <interval> — rewrites spec.output.interval in the exported YAML.
set_interval() {
  python3 - "${WORK_DIR}/signal_to_metrics.yaml" "$1" <<'PYEOF'
import sys, yaml
path, interval = sys.argv[1], sys.argv[2]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["output"]["interval"] = interval
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF
}

# assert_interval <interval> — the API must report the given interval.
assert_interval() {
  local output
  output="$(signal_to_metrics_get "$IDENTIFIER" "$DATASET" 2>&1)"
  echo "$output"
  printf '%s' "$output" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["spec"]["output"]["interval"] == sys.argv[1] else 1)
' "$1" || fail "API output does not reflect the post-import update to interval $1"
}

for interval in 5m 10m; do
  info "Step 7: Setting the interval to ${interval} + applying..."
  set_interval "$interval"
  TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
  assert_interval "$interval"
done
info "Update-after-import verified via the API, twice."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying the imported rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion..."
assert_signal_to_metrics_deleted "$IDENTIFIER" "$DATASET"

# ---------------------------------------------------------------------------
# Step 9: The deleted rule cannot be imported. The GET still answers with the
# tombstone, so without the provider's check the import would adopt a rule
# that no longer produces metrics.
# ---------------------------------------------------------------------------
info "Step 9: Asserting the deleted rule cannot be imported..."
set +e
IMPORT_OUTPUT="$(TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_signal_to_metrics.imported" "${DATASET},${IDENTIFIER}" 2>&1)"
IMPORT_RC=$?
set -e
if [[ "$IMPORT_RC" -eq 0 ]]; then
  echo "$IMPORT_OUTPUT"
  fail "terraform import of a deleted signal-to-metrics rule succeeded; it must be refused"
fi
# Terraform wraps diagnostic details across lines, so match the one-line
# summary and a single word of the detail rather than a phrase.
if ! echo "$IMPORT_OUTPUT" | grep -q "Error Importing Signal-to-Metrics Rule" \
  || ! echo "$IMPORT_OUTPUT" | grep -q "deleted"; then
  echo "$IMPORT_OUTPUT"
  fail "terraform import of a deleted rule failed, but not with the tombstone error"
fi
info "Tombstone import refused, as expected."

info "=== dash0_signal_to_metrics import roundtrip test PASSED ==="
