#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_recording_rule.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a recording rule is first created out-of-band via the dash0 CLI
# (representing a UI- or CLI-created asset), then adopted into Terraform state
# via `terraform import` using the `dataset,identifier` ID format. After
# import, `terraform plan` must report no changes.
#
# Recording rules are the second outlier in the "What 'import' does here"
# table: their identifier lives at `.metadata.labels["dash0.com/id"]` — the
# origin label is not emitted for recording rules.
#
# Steps:
#   1. Create recording rule via dash0 CLI (non-Terraform origin)
#   2. Discover identifier from `dash0.com/id` label
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
# A per-run name keeps runs that share the dataset, concurrent or earlier, from
# finding, importing or deleting each other's recording rules. Lookups match a
# substring, so the random part is padded to a fixed width: one run's name
# cannot then be a substring of another's (`-12` would otherwise match `-123`).
RR_NAME="roundtrip-import-recording-$(date +%s)-$(printf '%05d' "$RANDOM")"
IDENTIFIER=""
gone=""

discover_identifier() {
  dash0 recording-rules list --dataset "$DATASET" -o json --all \
    | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    if sys.argv[1] in (it.get('metadata', {}).get('name', '') or ''):
        labels = it.get('metadata', {}).get('labels', {}) or {}
        print(labels.get('dash0.com/id') or '')
        break
" "$RR_NAME"
}

cleanup() {
  if [[ "$gone" != "yes" ]]; then
    [[ -n "$IDENTIFIER" ]] || IDENTIFIER="$(discover_identifier 2>/dev/null)" || true
    if [[ -n "$IDENTIFIER" ]]; then
      warn "Deleting recording rule ${IDENTIFIER} created by this run..."
      dash0 recording-rules delete "$IDENTIFIER" --dataset "$DATASET" --force \
        || warn "Could not delete recording rule ${IDENTIFIER}. Delete it by hand."
    fi
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: terraform import (dash0_recording_rule) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create recording rule via dash0 CLI (out-of-band, no Terraform).
# ---------------------------------------------------------------------------
info "Step 1: Creating recording rule via dash0 CLI (name ${RR_NAME})..."

cat > "${WORK_DIR}/recording_rule.yaml" <<YAMLEOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: ${RR_NAME}
spec:
  groups:
    - name: RoundtripImportRecording
      interval: 1m0s
      rules:
        - record: job:http_requests_total:rate5m
          expr: sum by (job) (rate(http_requests_total[5m]))
          labels:
            env: roundtrip-import
YAMLEOF

dash0 recording-rules create -f "${WORK_DIR}/recording_rule.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create recording rule via dash0 CLI"
info "Recording rule created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier — recording rules only expose
# `dash0.com/id` (no `dash0.com/origin` label).
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(discover_identifier)"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for ${RR_NAME}"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created recording rule, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 recording-rules get "$IDENTIFIER" --dataset "$DATASET" -o yaml > "${WORK_DIR}/recording_rule.yaml" \
  || fail "Failed to export recording rule YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_recording_rule" "imported" {
  dataset             = var.dataset
  recording_rule_yaml = file("${path.module}/recording_rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_recording_rule.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_recording_rule.imported" "${DATASET},${IDENTIFIER}" \
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
# Step 7: Modify + apply — bump the group interval, matching what
# test_import_check_rule.sh does. It's a first-class PrometheusRule field
# the CLI surfaces verbatim.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/recording_rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["groups"][0]["interval"] = "2m0s"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 recording-rules get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "2m0s" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported recording rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
# The get endpoint has a longer cache TTL than list; use list (which the test
# already relies on in step 2) for prompt post-destroy verification.
assert_gone_from_list "Recording rule" "$IDENTIFIER" labels \
  dash0 recording-rules list --dataset "$DATASET" -o json --all
gone="yes"

info "=== dash0_recording_rule import roundtrip test PASSED ==="
