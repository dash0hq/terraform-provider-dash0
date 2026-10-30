#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_sampling_rule.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a sampling rule is first created out-of-band through the API (representing a
# UI- or API-created asset), then adopted into Terraform state via
# `terraform import` using the `dataset,identifier` ID format. After import,
# `terraform plan` must report no changes.
#
# The dash0 CLI has no sampling-rules command, so every read and the
# out-of-band create go through the raw API command (`dash0 -X api`). The
# create uses POST, the documented way to create a rule outside Terraform.
#
# Steps:
#   0. Preflight — creating a sampling rule requires the admin role
#   1. Create the rule via the API (non-Terraform origin)
#   2. Discover the identifier from the list response
#   3. Export its YAML via the API and write the Terraform resource shell
#   4. `terraform import` with the `dataset,identifier` ID
#   5. `terraform plan` reports no changes
#   6. The imported origin is preserved in state (no fresh tf_ prefix)
#   7. Modify the YAML + apply — the imported resource is manageable
#   8. Destroy + verify server-side deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_sampling_rule) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# Step 0a: Preflight — see sampling_rule_preflight in common.sh for why this can skip.
info "Step 0a: Preflight — checking sampling rules API access..."
sampling_rule_preflight "terraform import of a dash0_sampling_rule"

# ---------------------------------------------------------------------------
# Step 0b: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

SUFFIX="$(date +%s)-${RANDOM}"
OOB_ORIGIN="roundtrip-import-sampling-rule-${SUFFIX}"
DISPLAY_NAME="Roundtrip Import Test Sampling Rule ${SUFFIX}"

# ---------------------------------------------------------------------------
# Step 1: Create the rule via the API (out-of-band, no Terraform). A non-`tf_`
# origin is set so step 6's origin-preservation assertion is meaningful. The
# rule is disabled and its condition matches no span, so it never changes
# which traces the dataset keeps. The body carries the `dash0.com/dataset`
# label with the same value as `?dataset=`, as the provider sends it: older
# servers read only the label, and the API will reject a mismatch.
# ---------------------------------------------------------------------------
info "Step 1: Creating sampling rule via the API (origin ${OOB_ORIGIN})..."

cat > "${WORK_DIR}/create.json" <<JSONEOF
{
  "kind": "Dash0Sampling",
  "metadata": {
    "name": "roundtrip-import-sampling-rule",
    "labels": {"dash0.com/origin": "${OOB_ORIGIN}", "dash0.com/dataset": "${DATASET}"}
  },
  "spec": {
    "enabled": false,
    "display": {"name": "${DISPLAY_NAME}"},
    "conditions": {"kind": "ottl", "spec": {"ottl": "span.name == \"dash0-terraform-roundtrip-matches-nothing\""}},
    "rateLimit": {"rate": 100}
  }
}
JSONEOF

dash0 -X api POST "/api/sampling-rules" --dataset "$DATASET" -f "${WORK_DIR}/create.json" >/dev/null \
  || fail "Failed to create sampling rule via the API"
info "Rule created via the API."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the list response. Sampling rule
# identifiers live at `.metadata.labels["dash0.com/origin"]`, falling back to
# `.metadata.labels["dash0.com/id"]` when the origin label is absent.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via the API..."

# The display name is passed as argv, not interpolated into the program text,
# so a quote in it surfaces as a clear assertion failure.
IDENTIFIER="$(dash0 -X api "/api/sampling-rules" --dataset "$DATASET" \
  | python3 -c '
import json, sys
data = json.load(sys.stdin)
items = data.get("samplingRules", []) if isinstance(data, dict) else data
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
# API response is used as the sampling_rule_yaml verbatim.
# ---------------------------------------------------------------------------
info "Step 3: Exporting the rule via the API + writing Terraform config..."

sampling_rule_get "$IDENTIFIER" "$DATASET" > "${WORK_DIR}/sampling_rule.yaml" \
  || fail "Failed to export the sampling rule"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_sampling_rule" "imported" {
  dataset            = var.dataset
  sampling_rule_yaml = file("${path.module}/sampling_rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_sampling_rule.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_sampling_rule.imported" "${DATASET},${IDENTIFIER}" \
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
# Step 7: Modify + apply — change the rate limit, which the provider manages
# and the API returns verbatim.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove the imported resource is manageable..."

python3 - "${WORK_DIR}/sampling_rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["rateLimit"] = {"rate": 50}
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

API_OUTPUT="$(sampling_rule_get "$IDENTIFIER" "$DATASET" 2>&1)"
echo "$API_OUTPUT"
printf '%s' "$API_OUTPUT" | python3 -c '
import json, sys
rate = (json.load(sys.stdin).get("spec", {}).get("rateLimit") or {}).get("rate")
sys.exit(0 if rate == 50 else 1)
' || fail "API output does not reflect the post-import update"
info "Update-after-import verified via the API."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying the imported rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion..."
assert_sampling_rule_deleted "$IDENTIFIER" "$DATASET"

info "=== dash0_sampling_rule import roundtrip test PASSED ==="
