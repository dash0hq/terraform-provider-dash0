#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_view.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a view is first created out-of-band via the dash0 CLI (representing a UI-
# or CLI-created asset), then adopted into Terraform state via
# `terraform import` using the `dataset,identifier` ID format. After import,
# `terraform plan` must report no changes — the guide's central promise.
#
# The view identifier lives at `.metadata.labels["dash0.com/origin"]` for
# API-created assets (falling back to `.metadata.labels["dash0.com/id"]` for
# UI-created ones where the origin label is absent). This test exercises the
# API-created path.
#
# Steps:
#   1. Create view via dash0 CLI (non-Terraform origin, no `tf_` prefix)
#   2. Export its YAML via dash0 CLI (so the local file matches server state)
#   3. Write a matching resource shell in Terraform + `terraform init`
#   4. Run `terraform import` with the `dataset,identifier` ID
#   5. Assert `terraform plan -detailed-exitcode` returns 0 (no changes)
#   6. Verify the imported origin is preserved in state (no fresh tf_ prefix)
#   7. Modify the YAML + `terraform apply` — proves the imported resource is
#      manageable, not just visible in state
#   8. Destroy via Terraform + verify deletion server-side

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_view) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create view via dash0 CLI (out-of-band, no Terraform).
# ---------------------------------------------------------------------------
info "Step 1: Creating view via dash0 CLI..."

cat > "${WORK_DIR}/view.yaml" <<YAMLEOF
kind: Dash0View
metadata:
  name: roundtrip-import-view
  labels:
    "dash0.com/dataset": "${DATASET}"
spec:
  display:
    name: Roundtrip Import Test View
    folder: []
  type: spans
  permissions:
    - actions:
        - "views:read"
        - "views:delete"
      role: "admin"
    - actions:
        - "views:read"
      role: "basic_member"
  filter:
    - key: service.name
      operator: is
      value: probe
  table:
    columns:
      - colSize: minmax(auto, 2fr)
        key: dash0.span.name
        label: Name
      - colSize: min-content
        key: service.name
        label: Service
      - colSize: 8.5rem
        key: otel.span.duration
        label: Duration
    sort:
      - direction: ascending
        key: otel.span.duration
YAMLEOF

dash0 views create -f "${WORK_DIR}/view.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create view via dash0 CLI"
info "View created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier — API-created views expose their origin at
# `.metadata.labels["dash0.com/origin"]`. Fall back to `dash0.com/id` for UI-
# created assets where the origin label is absent (the import guide documents
# this fallback with the jq `//` operator).
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 views list --dataset "$DATASET" -o json --limit 500 \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    if it.get('spec', {}).get('display', {}).get('name') == 'Roundtrip Import Test View':
        labels = it.get('metadata', {}).get('labels', {}) or {}
        print(labels.get('dash0.com/origin') or labels.get('dash0.com/id') or '')
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for roundtrip-import-view"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created view, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export the current YAML from Dash0 (so terraform plan sees no diff
# after import) and write the resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 views get "$IDENTIFIER" --dataset "$DATASET" -o yaml > "${WORK_DIR}/view.yaml" \
  || fail "Failed to export view YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_view" "imported" {
  dataset   = var.dataset
  view_yaml = file("${path.module}/view.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_view.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_view.imported" "${DATASET},${IDENTIFIER}" \
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
# Step 7: Modify + apply — mutate the filter value. `spec.filter[]` is a
# first-class field the provider manages and the CLI surfaces verbatim, so
# the update is easy to grep for.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/view.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["filter"][0]["value"] = "probe-updated"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 views get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "probe-updated" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported view via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
# The get endpoint has a longer cache TTL than list; use list (which the test
# already relies on in step 2) for prompt post-destroy verification.
gone=""
for i in $(seq 1 10); do
  set +e
  dash0 views list --dataset "$DATASET" -o json --limit 500 \
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
    warn "View still in list (attempt ${i}/10), retrying in 3s..."
    sleep 3
  fi
done
[[ "$gone" == "yes" ]] || fail "View '${IDENTIFIER}' still returned by list after 10 attempts"

info "=== dash0_view import roundtrip test PASSED ==="
