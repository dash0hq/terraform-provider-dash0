#!/usr/bin/env bash
# Roundtrip test for `terraform import` of dash0_dataset (org-scoped; import ID = slug).
# The dash0 CLI has no `datasets` command, so the API is called via `dash0 -X api`.
# Destroy is a soft delete (purged after 30 days), so each run uses a unique slug.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
SLUG="rt-ds-$(date +%s)-${RANDOM}"
DESTROYED=0

# Destroy on early failure so aborted runs do not leave a live dataset behind.
# Before the import (no state yet) the dataset is deleted through the API.
cleanup() {
  if [[ "$DESTROYED" -eq 0 ]]; then
    if [[ -f "${WORK_DIR}/terraform.tfstate" ]]; then
      TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR" >/dev/null 2>&1 || true
    else
      dash0 -X api DELETE "/api/datasets/${SLUG}" --dataset "" >/dev/null 2>&1 || true
    fi
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: terraform import (dash0_dataset) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset slug: ${SLUG}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create dataset via the API (out-of-band).
# ---------------------------------------------------------------------------
info "Step 1: Creating dataset via the API..."

cat > "${WORK_DIR}/create.json" <<JSONEOF
{
  "kind": "Dash0Dataset",
  "metadata": {
    "name": "Roundtrip Import Dataset",
    "labels": { "dash0.com/id": "${SLUG}" }
  },
  "spec": {
    "tracing": { "retention": "30d" },
    "logging": { "retention": "30d" },
    "geoLocation": { "endUserGeoLocationStorageStrategy": "up_to_country" },
    "ipAddresses": { "endUserIpAddressStorageStrategy": "anonymize" },
    "cardholderDataRedaction": { "enabled": true }
  }
}
JSONEOF

dash0 -X api POST /api/datasets --dataset "" -f "${WORK_DIR}/create.json" >/dev/null \
  || fail "Failed to create dataset via the API"
info "Dataset created via the API: ${SLUG}"

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the dataset list.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via the dataset list..."

IDENTIFIER="$(dash0 -X api /api/datasets --dataset "" \
  | python3 -c "
import json, sys
doc = json.load(sys.stdin)
for it in doc.get('datasets', []):
    labels = it.get('metadata', {}).get('labels', {})
    if labels.get('dash0.com/id') == '$SLUG':
        print(labels['dash0.com/id'])
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for dataset ${SLUG}"
info "Identifier: ${IDENTIFIER}"

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via the API + writing Terraform config..."

# Drop fields the resource does not manage (permissions, telemetry filters)
# and server-generated ones (annotations, permitted actions, origin, version,
# source), keeping the slug label.
dataset_get "$IDENTIFIER" \
  | python3 -c "
import sys, yaml
doc = yaml.safe_load(sys.stdin.read())
meta = doc['metadata']
meta.pop('annotations', None)
meta['labels'] = {'dash0.com/id': meta['labels']['dash0.com/id']}
for key in ('permissions', 'permittedActions', 'telemetryFilters'):
    doc['spec'].pop(key, None)
sys.stdout.write(yaml.safe_dump(doc, sort_keys=False))
" > "${WORK_DIR}/dataset.yaml" \
  || fail "Failed to export dataset YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_dataset" "imported" {
  dataset_yaml = file("${path.module}/dataset.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_dataset.imported.origin
}

output "id" {
  value = dash0_dataset.imported.id
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import with the slug only — no dataset prefix.
# Datasets are organization-scoped, matching the team import path.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import (slug only, no dataset)..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_dataset.imported" "$IDENTIFIER" \
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
STATE_ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
if [[ "$STATE_ID" != "$IDENTIFIER" ]]; then
  fail "Expected imported id '${IDENTIFIER}' in state, got '${STATE_ID}'"
fi
STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
if [[ "$STATE_ORIGIN" == tf_* ]]; then
  fail "Imported origin '${STATE_ORIGIN}' unexpectedly carries the tf_ prefix (would indicate re-anchoring)"
fi
info "Identifier preservation check PASSED."

# ---------------------------------------------------------------------------
# Step 7: Modify + apply.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/dataset.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["metadata"]["name"] = "updated-after-import"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

API_OUTPUT="$(dataset_get "$IDENTIFIER" 2>&1)"
echo "$API_OUTPUT" | grep -q "updated-after-import" \
  || fail "API output does not reflect the post-import update"
info "Update-after-import verified via the API."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported dataset via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
DESTROYED=1

info "Step 8b: Verifying server-side deletion..."
assert_dataset_deleted "$IDENTIFIER"

info "=== dash0_dataset import roundtrip test PASSED ==="
