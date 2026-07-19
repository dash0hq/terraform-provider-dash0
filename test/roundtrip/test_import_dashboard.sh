#!/usr/bin/env bash
# Roundtrip test for `terraform import` on a dataset-scoped resource (dash0_dashboard).
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a dashboard is first created out-of-band via the dash0 CLI (representing a UI-
# or CLI-created asset), then adopted into Terraform state via `terraform import`
# using the `dataset,identifier` ID format. After import, `terraform plan` must
# report no changes — the guide's central promise.
#
# Steps:
#   1. Create dashboard via dash0 CLI (non-Terraform origin, no `tf_` prefix)
#   2. Export its YAML via dash0 CLI (so the local file matches server state)
#   3. Write a matching resource shell in Terraform + `terraform init`
#   4. Run `terraform import` with the `dataset,identifier` ID
#   5. Assert `terraform plan -detailed-exitcode` returns 0 (no changes)
#   6. Verify the imported origin is preserved in state (no fresh tf_ prefix)
#   7. Modify the YAML + `terraform apply` — proves the imported resource is
#      manageable, not just visible in state
#   8. Destroy via Terraform + verify deletion server-side

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_dashboard) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create dashboard via dash0 CLI (out-of-band, no Terraform)
# ---------------------------------------------------------------------------
info "Step 1: Creating dashboard via dash0 CLI..."

cat > "${WORK_DIR}/dashboard.yaml" <<'DASHEOF'
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: roundtrip-import-dashboard
  labels: {}
spec:
  duration: "30m"
  display:
    description: "Roundtrip import test dashboard"
    name: "Roundtrip Import Test Dashboard"
  layouts:
    - kind: Grid
      spec:
        items:
          - content:
              $ref: "#/spec/panels/panel-1"
            height: 8
            width: 24
            x: 0
            "y": 0
  panels:
    panel-1:
      kind: Panel
      spec:
        display:
          description: ""
          name: "Import Test Panel"
        plugin:
          kind: TimeSeriesChart
          spec:
            legend:
              position: bottom
        queries:
          - kind: TimeSeriesQuery
            spec:
              plugin:
                kind: PrometheusTimeSeriesQuery
                spec:
                  query: "up"
DASHEOF

dash0 dashboards create -f "${WORK_DIR}/dashboard.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create dashboard via dash0 CLI"
info "Dashboard created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier (metadata.dash0Extensions.id) — the same
# path the import guide documents.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 dashboards list --dataset "$DATASET" -o json --limit 500 \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    if it.get('spec', {}).get('display', {}).get('name') == 'Roundtrip Import Test Dashboard':
        print(it['metadata']['dash0Extensions']['id'])
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for roundtrip-import-dashboard"
info "Identifier: ${IDENTIFIER}"

# Sanity check: the CLI-created identifier must NOT carry the tf_ prefix — that
# prefix belongs to origins the provider generates on Create.
if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created dashboard, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export the current YAML from Dash0 (so terraform plan sees no diff
# after import) and write the resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 dashboards get "$IDENTIFIER" --dataset "$DATASET" -o yaml > "${WORK_DIR}/dashboard.yaml" \
  || fail "Failed to export dashboard YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_dashboard" "imported" {
  dataset        = var.dataset
  dashboard_yaml = file("${path.module}/dashboard.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_dashboard.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_dashboard.imported" "${DATASET},${IDENTIFIER}" \
  || fail "terraform import failed"
info "Import completed."

# ---------------------------------------------------------------------------
# Step 5: `terraform plan` must report no changes — the guide's central claim
# ---------------------------------------------------------------------------
info "Step 5: Asserting terraform plan reports no changes after import..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Origin preservation — state must carry the CLI-assigned identifier,
# not a fresh tf_-prefixed one.
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
# Step 7: Modify + apply — proves the imported resource is fully manageable.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/dashboard.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc.setdefault("spec", {}).setdefault("display", {})["description"] = "UPDATED via terraform after import"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 dashboards get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "UPDATED via terraform after import" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

# Re-run idempotency to catch a plan modifier that only settles after a real
# apply (regression insurance for any future normalizer change).
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify deletion server-side
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported dashboard via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
# The get endpoint has a longer cache TTL than list; use list (which the test
# already relies on in step 2) for prompt post-destroy verification.
gone=""
for i in $(seq 1 10); do
  set +e
  dash0 dashboards list --dataset "$DATASET" -o json --limit 500 \
    | python3 -c "
import json, sys
items = json.load(sys.stdin)
sys.exit(0 if any(it.get('spec', {}).get('display', {}).get('name') == 'Roundtrip Import Test Dashboard' for it in items) else 1)
"
  found=$?
  set -e
  if [[ $found -ne 0 ]]; then
    info "Server-side deletion confirmed via list (attempt ${i})."
    gone="yes"
    break
  fi
  if [[ $i -lt 10 ]]; then
    warn "Dashboard still in list (attempt ${i}/10), retrying in 3s..."
    sleep 3
  fi
done
[[ "$gone" == "yes" ]] || fail "Dashboard 'Roundtrip Import Test Dashboard' still returned by list after 10 attempts"

info "=== dash0_dashboard import roundtrip test PASSED ==="
