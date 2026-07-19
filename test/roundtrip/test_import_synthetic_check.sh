#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_synthetic_check.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a synthetic check is first created out-of-band via the dash0 CLI
# (representing a UI- or CLI-created asset), then adopted into Terraform state
# via `terraform import` using the `dataset,identifier` ID format. After
# import, `terraform plan` must report no changes.
#
# Synthetic checks store the identifier at
# `.metadata.labels["dash0.com/origin"]` (falling back to `dash0.com/id` when
# the origin label is absent on UI-created assets).
#
# Steps:
#   1. Create synthetic check via dash0 CLI (non-Terraform origin)
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

info "=== Roundtrip test: terraform import (dash0_synthetic_check) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create synthetic check via dash0 CLI (out-of-band, no Terraform).
# Fixture mirrors test_synthetic_check.sh — a shape known to round-trip.
# ---------------------------------------------------------------------------
info "Step 1: Creating synthetic check via dash0 CLI..."

cat > "${WORK_DIR}/synthetic_check.yaml" <<'YAMLEOF'
kind: Dash0SyntheticCheck
metadata:
  name: roundtrip-import-sc
  labels: {}
spec:
  enabled: true
  notifications:
    channels: []
  plugin:
    display:
      name: roundtrip-import.example.com
    kind: http
    spec:
      assertions:
        criticalAssertions:
          - kind: status_code
            spec:
              value: "200"
              operator: is
        degradedAssertions: []
      request:
        method: get
        url: https://www.example.com
        queryParameters: []
        headers: []
        redirects: follow
        tls:
          allowInsecure: false
        tracing:
          addTracingHeaders: false
  retries:
    kind: fixed
    spec:
      attempts: 2
      delay: 1s
  schedule:
    interval: 5m
    locations:
      - de-frankfurt
    strategy: all_locations
YAMLEOF

dash0 synthetic-checks create -f "${WORK_DIR}/synthetic_check.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create synthetic check via dash0 CLI"
info "Synthetic check created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 synthetic-checks list --dataset "$DATASET" -o json --limit 500 \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    if it.get('metadata', {}).get('name') == 'roundtrip-import-sc':
        labels = it.get('metadata', {}).get('labels', {}) or {}
        print(labels.get('dash0.com/origin') or labels.get('dash0.com/id') or '')
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for roundtrip-import-sc"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created synthetic check, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 synthetic-checks get "$IDENTIFIER" --dataset "$DATASET" -o yaml > "${WORK_DIR}/synthetic_check.yaml" \
  || fail "Failed to export synthetic check YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_synthetic_check" "imported" {
  dataset              = var.dataset
  synthetic_check_yaml = file("${path.module}/synthetic_check.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_synthetic_check.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_synthetic_check.imported" "${DATASET},${IDENTIFIER}" \
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
# Step 7: Modify + apply — bump the schedule interval. It's a first-class
# field the CLI surfaces verbatim, safe to mutate without invalidating the
# rest of the check.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/synthetic_check.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["schedule"]["interval"] = "10m"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 synthetic-checks get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "interval: 10m" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported synthetic check via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
# The get endpoint has a longer cache TTL than list; use list (which the test
# already relies on in step 2) for prompt post-destroy verification.
gone=""
for i in $(seq 1 10); do
  set +e
  dash0 synthetic-checks list --dataset "$DATASET" -o json --limit 500 \
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
    warn "Synthetic check still in list (attempt ${i}/10), retrying in 3s..."
    sleep 3
  fi
done
[[ "$gone" == "yes" ]] || fail "Synthetic check '${IDENTIFIER}' still returned by list after 10 attempts"

info "=== dash0_synthetic_check import roundtrip test PASSED ==="
