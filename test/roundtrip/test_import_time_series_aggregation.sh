#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_time_series_aggregation.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# an aggregation is first created out-of-band via the dash0 CLI (representing a
# UI- or CLI-created asset), then adopted into Terraform state via
# `terraform import` using the `dataset,identifier` ID format. After import,
# `terraform plan` must report no changes.
#
# Two TSA-specific divergences from test_import_view.sh:
#   * The out-of-band document must carry `metadata.labels["dash0.com/origin"]`
#     explicitly — the CLI rejects a TSA document without it before making any
#     API call. Every other asset kind's import test can omit it. A non-`tf_`
#     origin is used so step 6's origin-preservation assertion is meaningful.
#   * Deletion is verified via `list`, not `get`: TSA deletes are soft, so a GET
#     by origin returns 200 with a `dash0.com/deleted-at` annotation. `list`
#     does exclude tombstones.
#
# Steps:
#   0. Preflight — every TSA endpoint requires the organization-admin role
#   1. Create the aggregation via the dash0 CLI (non-Terraform origin)
#   2. Discover the identifier from `dash0 tsa list -o json`
#   3. Export its YAML via the CLI and write the Terraform resource shell
#   4. `terraform import` with the `dataset,identifier` ID
#   5. `terraform plan` reports no changes
#   6. The imported origin is preserved in state (no fresh tf_ prefix)
#   7. Modify the YAML + apply — the imported resource is manageable
#   8. Destroy + verify server-side deletion via list

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_time_series_aggregation) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# Step 0a: Preflight — see tsa_preflight in common.sh for why this can skip.
info "Step 0a: Preflight — checking TSA API access..."
tsa_preflight "terraform import of a dash0_time_series_aggregation"

# ---------------------------------------------------------------------------
# Step 0b: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# Origins are unique per organization, and TSA deletes are soft, so a fixed
# origin would collide with tombstones left by earlier runs. Make it unique.
SUFFIX="$(date +%s)-${RANDOM}"
OOB_ORIGIN="roundtrip-import-tsa-${SUFFIX}"
DISPLAY_NAME="Roundtrip Import Test TSA ${SUFFIX}"

# ---------------------------------------------------------------------------
# Step 1: Create the aggregation via the dash0 CLI (out-of-band, no Terraform).
#
# `dash0.com/origin` is mandatory here: the CLI has no other source for the PUT
# URL path and rejects a TSA document without it before issuing any request.
# ---------------------------------------------------------------------------
info "Step 1: Creating time series aggregation via dash0 CLI (origin ${OOB_ORIGIN})..."

cat > "${WORK_DIR}/tsa.yaml" <<YAMLEOF
apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: roundtrip-import-tsa
  labels:
    "dash0.com/origin": "${OOB_ORIGIN}"
spec:
  enabled: true
  display:
    name: ${DISPLAY_NAME}
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
YAMLEOF

dash0 time-series-aggregations create -f "${WORK_DIR}/tsa.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create time series aggregation via dash0 CLI"
info "Aggregation created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the list response. TSA identifiers live
# at `.metadata.labels["dash0.com/origin"]`.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

# The display name is passed as argv, not interpolated into the program text:
# it round-trips through an API response, and any quote in it would surface as
# a confusing python SyntaxError instead of a clear assertion failure.
# assert_yaml_equivalent in common.sh uses the same quoted-heredoc + argv shape.
IDENTIFIER="$(dash0 time-series-aggregations list --dataset "$DATASET" -o json --limit 500 \
  | python3 -c '
import json, sys
data = json.load(sys.stdin)
items = data.get("items", data) if isinstance(data, dict) else data
target = sys.argv[1]
for it in items:
    if it.get("spec", {}).get("display", {}).get("name") == target:
        labels = it.get("metadata", {}).get("labels", {}) or {}
        print(labels.get("dash0.com/origin") or "")
        break
' "$DISPLAY_NAME")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for '${DISPLAY_NAME}'"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created aggregation, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export the current YAML from Dash0 (so terraform plan sees no diff
# after import) and write the resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 time-series-aggregations get "$IDENTIFIER" --dataset "$DATASET" -o yaml > "${WORK_DIR}/tsa.yaml" \
  || fail "Failed to export time series aggregation YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_time_series_aggregation" "imported" {
  dataset                      = var.dataset
  time_series_aggregation_yaml = file("${path.module}/tsa.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_time_series_aggregation.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_time_series_aggregation.imported" "${DATASET},${IDENTIFIER}" \
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
# Step 7: Modify + apply — mutate the sample interval, which the provider
# manages and the CLI surfaces verbatim, so it is easy to grep for.
#
# The API rejects anything outside 10s–10m ("The sample interval must be a
# valid duration between 10s and 10m", 400), so the updated value has to stay
# inside that window: 5m -> 10m is a real change and still legal.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove the imported resource is manageable..."

python3 - "${WORK_DIR}/tsa.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["sample"]["interval"] = "10m"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 time-series-aggregations get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT"
echo "$CLI_OUTPUT" | grep -q "10m" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying the imported aggregation via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
assert_tsa_deleted_via_list "$IDENTIFIER" "$DATASET"

info "=== dash0_time_series_aggregation import roundtrip test PASSED ==="
