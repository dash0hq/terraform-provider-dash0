#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_check_rule.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a check rule is first created out-of-band via the dash0 CLI (representing a
# UI- or CLI-created asset), then adopted into Terraform state via
# `terraform import` using the `dataset,identifier` ID format. After import,
# `terraform plan` must report no changes — the guide's central promise.
#
# Check rules are the outlier in the "What 'import' does here" table: their
# list-response is a flat structure (top-level `.id` / `.name`, no `metadata`
# wrapper), and the CLI's `get -o yaml` returns that same flat form rather
# than the PrometheusRule YAML the provider ingests. Because of that, this
# test keeps a locally-authored PrometheusRule YAML file instead of exporting
# via the CLI at step 3 — the equivalent-uploaded/downloaded assertion that
# `test_check_rule.sh` already proves at every apply covers the round-trip.
#
# Steps:
#   1. Create check rule via dash0 CLI (non-Terraform origin, no `tf_` prefix)
#   2. Discover its identifier from the list endpoint (flat `.id` field)
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

info "=== Roundtrip test: terraform import (dash0_check_rule) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create check rule via dash0 CLI (out-of-band, no Terraform).
# The fixture matches the PrometheusRule form the provider ingests — the same
# one exercised end-to-end by test_check_rule.sh.
# ---------------------------------------------------------------------------
info "Step 1: Creating check rule via dash0 CLI..."

cat > "${WORK_DIR}/check_rule.yaml" <<'YAMLEOF'
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: roundtrip-import-check
spec:
  groups:
    - name: RoundtripImportCheck
      interval: 1m0s
      rules:
        - alert: roundtrip-import-alert
          expr: vector(1) > 0
          for: 0s
          keep_firing_for: 0s
          annotations:
            summary: "Roundtrip import test alert"
            description: "Alert created by the import roundtrip test"
            dash0-threshold-critical: "90"
            dash0-threshold-degraded: "80"
            dash0-enabled: true
          labels: {}
YAMLEOF

dash0 check-rules create -f "${WORK_DIR}/check_rule.yaml" --dataset "$DATASET" >/dev/null \
  || fail "Failed to create check rule via dash0 CLI"
info "Check rule created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier — check rules have a flat list shape with
# top-level `.id` and `.name` (no `metadata` wrapper). The name the API stores
# is `<group_name> - <alert_name>`.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 check-rules list --dataset "$DATASET" -o json --limit 500 \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    name = it.get('name', '') or ''
    if 'roundtrip-import' in name:
        print(it['id'])
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for roundtrip-import check rule"
info "Identifier: ${IDENTIFIER}"

# Sanity check: the CLI-created identifier must NOT carry the tf_ prefix — that
# prefix belongs to origins the provider generates on Create.
if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created check rule, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Write the Terraform resource shell. We keep the locally-authored
# PrometheusRule fixture — the CLI's `get -o yaml` returns the flat form,
# not the PrometheusRule form the provider expects, so exporting via the CLI
# here would break the round-trip.
# ---------------------------------------------------------------------------
info "Step 3: Writing Terraform config..."

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_check_rule" "imported" {
  dataset         = var.dataset
  check_rule_yaml = file("${path.module}/check_rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_check_rule.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import + sync the local file to state.
#
# The API returns a normalized PrometheusRule shape (no `metadata.name`, no
# zero-value `for` / `keep_firing_for`, different key ordering) which the
# provider stores verbatim in state on import. Because we cannot export the
# server's PrometheusRule view via the CLI (`dash0 check-rules get -o yaml`
# returns the flat form), we extract the imported `check_rule_yaml` from
# Terraform state and write it back to the local file. `terraform plan` can
# then compare file-vs-state as pure strings and see no diff.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_check_rule.imported" "${DATASET},${IDENTIFIER}" \
  || fail "terraform import failed"
info "Import completed."

info "Step 4b: Syncing local YAML file to imported state..."
(cd "$WORK_DIR" && TF_CLI_CONFIG_FILE="${WORK_DIR}/.terraformrc" tofu show -json) \
  | python3 -c "
import json, sys
data = json.load(sys.stdin)
for r in data['values']['root_module']['resources']:
    if r['address'] == 'dash0_check_rule.imported':
        sys.stdout.write(r['values']['check_rule_yaml'])
        break
" > "${WORK_DIR}/check_rule.yaml"
[[ -s "${WORK_DIR}/check_rule.yaml" ]] || fail "Failed to sync check_rule.yaml from state"

# ---------------------------------------------------------------------------
# Step 5: `terraform plan` must report no changes.
# ---------------------------------------------------------------------------
info "Step 5: Asserting terraform plan reports no changes after import..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Origin preservation — state must carry the CLI-assigned identifier.
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
# Step 7: Modify + apply — bump the group interval and prove the imported
# resource is fully manageable. Interval is a first-class PrometheusRule
# field, safe to mutate, and shows up verbatim in the CLI output.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/check_rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["groups"][0]["interval"] = "2m0s"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 check-rules get "$IDENTIFIER" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "2m0s" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

# Re-run idempotency to catch a plan modifier that only settles after a real
# apply (regression insurance for any future normalizer change).
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify deletion server-side
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported check rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion via list..."
# The get endpoint has a longer cache TTL than list; use list (which the test
# already relies on in step 2) for prompt post-destroy verification.
gone=""
for i in $(seq 1 10); do
  set +e
  dash0 check-rules list --dataset "$DATASET" -o json --limit 500 \
    | python3 -c "
import json, sys
items = json.load(sys.stdin)
target = '$IDENTIFIER'
sys.exit(0 if any(it.get('id') == target for it in items) else 1)
"
  found=$?
  set -e
  if [[ $found -ne 0 ]]; then
    info "Server-side deletion confirmed via list (attempt ${i})."
    gone="yes"
    break
  fi
  if [[ $i -lt 10 ]]; then
    warn "Check rule still in list (attempt ${i}/10), retrying in 3s..."
    sleep 3
  fi
done
[[ "$gone" == "yes" ]] || fail "Check rule '${IDENTIFIER}' still returned by list after 10 attempts"

info "=== dash0_check_rule import roundtrip test PASSED ==="
