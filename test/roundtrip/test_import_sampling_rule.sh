#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_sampling_rule.
#
# Simulates the user story documented in docs/guides/import-existing-assets.md:
# a sampling rule is first created out-of-band through the API (representing a
# UI- or API-created asset), then adopted into Terraform state via
# `terraform import` using the `dataset,identifier` ID format. After import,
# `terraform plan` must report no changes.
#
# The identifier is the rule's `dash0.com/origin` label, or its `dash0.com/id`
# label when the rule has no origin (UI-created rules). Both cases run:
#   - origin: the rule is created with a non-Terraform origin label.
#   - id:     the rule is created without an origin label, so it is imported by
#             id and the provider addresses it by id on every later read, write
#             and delete. Step 7 proves the apply after import updates that rule
#             rather than creating a second one next to it.
#
# The dash0 CLI has no sampling-rules command, so every read and the
# out-of-band create go through the raw API command (`dash0 -X api`). The
# create uses POST, the documented way to create a rule outside Terraform.
#
# Steps (per case):
#   1. Create the rule via the API (non-Terraform origin, or no origin)
#   2. Discover the identifier from the list response
#   3. Export its YAML via the API and write the Terraform resource shell
#   4. `terraform import` with the `dataset,identifier` ID
#   5. `terraform plan` reports no changes
#   6. The identifier is preserved in state as the origin (no fresh tf_ prefix)
#   7. Modify the YAML + apply — the same rule is updated, no second one appears
#   8. Destroy + verify server-side deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_ROOT="$(mktemp -d)"
trap 'rm -rf "$WORK_ROOT"' EXIT

info "=== Roundtrip test: terraform import (dash0_sampling_rule) ==="
info "Working directory: ${WORK_ROOT}"
info "Dataset: ${DATASET}"

# Step 0: Preflight — see sampling_rule_preflight in common.sh for why this can skip.
info "Step 0: Preflight — checking sampling rules API access..."
sampling_rule_preflight "terraform import of a dash0_sampling_rule"

# rules_with_display_name <display-name>
#
# Prints one "<origin>,<id>" line per sampling rule in $DATASET with the given
# display name; a label that is absent prints as empty. The display name is
# passed as argv, not interpolated into the program text, so a quote in it
# surfaces as a clear assertion failure.
rules_with_display_name() {
  dash0 -X api "/api/sampling-rules" --dataset "$DATASET" \
    | python3 -c '
import json, sys
data = json.load(sys.stdin)
items = data.get("samplingRules", []) if isinstance(data, dict) else data
for it in items:
    if (it.get("spec", {}).get("display") or {}).get("name") == sys.argv[1]:
        labels = it.get("metadata", {}).get("labels", {}) or {}
        print("%s,%s" % (labels.get("dash0.com/origin") or "", labels.get("dash0.com/id") or ""))
' "$1"
}

# run_import_case <origin|id>
run_import_case() {
  local case_name="$1"
  local work_dir="${WORK_ROOT}/${case_name}"
  mkdir -p "$work_dir"
  write_provider_tf "$work_dir"

  local suffix display_name oob_origin labels
  suffix="$(date +%s)-${RANDOM}"
  display_name="Roundtrip Import Test Sampling Rule ${case_name} ${suffix}"
  # The sampling rule write endpoints ignore `?dataset=` and read the dataset
  # from the `dash0.com/dataset` label, so the body always carries it.
  labels="\"dash0.com/dataset\": \"${DATASET}\""
  if [[ "$case_name" == "origin" ]]; then
    oob_origin="roundtrip-import-sampling-rule-${suffix}"
    labels="\"dash0.com/origin\": \"${oob_origin}\", ${labels}"
  fi

  info "=== Case: import by ${case_name} ==="

  # -------------------------------------------------------------------------
  # Step 1: Create the rule via the API (out-of-band, no Terraform). The rule
  # is disabled and its condition matches no span, so it never changes which
  # traces the dataset keeps.
  # -------------------------------------------------------------------------
  info "Step 1: Creating sampling rule via the API..."

  cat > "${work_dir}/create.json" <<JSONEOF
{
  "kind": "Dash0Sampling",
  "metadata": {
    "name": "roundtrip-import-sampling-rule",
    "labels": {${labels}}
  },
  "spec": {
    "enabled": false,
    "display": {"name": "${display_name}"},
    "conditions": {"kind": "ottl", "spec": {"ottl": "span.name == \"dash0-terraform-roundtrip-matches-nothing\""}},
    "rateLimit": {"rate": 100}
  }
}
JSONEOF

  dash0 -X api POST "/api/sampling-rules" --dataset "$DATASET" -f "${work_dir}/create.json" >/dev/null \
    || fail "Failed to create sampling rule via the API"
  info "Rule created via the API."

  # -------------------------------------------------------------------------
  # Step 2: Discover the identifier from the list response:
  # `.metadata.labels["dash0.com/origin"]`, falling back to
  # `.metadata.labels["dash0.com/id"]` when the origin label is absent.
  # -------------------------------------------------------------------------
  info "Step 2: Discovering identifier via the API..."

  local matches listed_origin listed_id identifier
  matches="$(rules_with_display_name "$display_name")"
  [[ -n "$matches" && "$matches" != *$'\n'* ]] \
    || fail "Expected exactly one rule named '${display_name}', got: ${matches:-none}"
  listed_origin="${matches%%,*}"
  listed_id="${matches#*,}"
  [[ -n "$listed_id" ]] || fail "Rule '${display_name}' has no dash0.com/id label"

  if [[ "$case_name" == "origin" ]]; then
    [[ "$listed_origin" == "$oob_origin" ]] \
      || fail "Expected origin '${oob_origin}' on the created rule, got '${listed_origin}'"
    identifier="$listed_origin"
  else
    # Without this, the case would silently fall back to the origin path.
    [[ -z "$listed_origin" ]] \
      || fail "Expected no origin label on a rule created without one, got '${listed_origin}'; the import-by-id path cannot be exercised"
    identifier="$listed_id"
  fi
  info "Identifier: ${identifier}"

  if [[ "$identifier" == tf_* ]]; then
    fail "Expected a non-Terraform identifier from an API-created rule, got: ${identifier}"
  fi

  # -------------------------------------------------------------------------
  # Step 3: Export the current document from Dash0 (so terraform plan sees no
  # diff after import) and write the resource shell. JSON is valid YAML, so
  # the API response is used as the sampling_rule_yaml verbatim.
  # -------------------------------------------------------------------------
  info "Step 3: Exporting the rule via the API + writing Terraform config..."

  sampling_rule_get "$identifier" "$DATASET" > "${work_dir}/sampling_rule.yaml" \
    || fail "Failed to export the sampling rule"

  cat > "${work_dir}/main.tf" <<'EOF'
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

  tf_init "$work_dir"

  # -------------------------------------------------------------------------
  # Step 4: terraform import
  # -------------------------------------------------------------------------
  info "Step 4: Importing via terraform import..."

  TF_VAR_dataset="$DATASET" tf_import "$work_dir" "dash0_sampling_rule.imported" "${DATASET},${identifier}" \
    || fail "terraform import failed"
  info "Import completed."

  # -------------------------------------------------------------------------
  # Step 5: `terraform plan` must report no changes.
  # -------------------------------------------------------------------------
  info "Step 5: Asserting terraform plan reports no changes after import..."
  assert_idempotent "$work_dir"

  # -------------------------------------------------------------------------
  # Step 6: Identifier preservation.
  # -------------------------------------------------------------------------
  info "Step 6: Verifying identifier preservation in state..."
  local state_origin
  state_origin="$(TF_VAR_dataset="$DATASET" tf_output "$work_dir" origin)"
  if [[ "$state_origin" != "$identifier" ]]; then
    fail "Expected imported origin '${identifier}' in state, got '${state_origin}'"
  fi
  info "Identifier preservation check PASSED."

  # -------------------------------------------------------------------------
  # Step 7: Modify + apply — change the rate limit, which the provider manages
  # and the API returns verbatim. The apply must update the imported rule in
  # place: exactly one rule with the display name, with the same id as before.
  # -------------------------------------------------------------------------
  info "Step 7: Modifying + applying to prove the imported resource is manageable..."

  python3 - "${work_dir}/sampling_rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["rateLimit"] = {"rate": 50}
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

  TF_VAR_dataset="$DATASET" tf_apply "$work_dir"

  local api_output
  api_output="$(sampling_rule_get "$identifier" "$DATASET" 2>&1)"
  echo "$api_output"
  printf '%s' "$api_output" | python3 -c '
import json, sys
rate = (json.load(sys.stdin).get("spec", {}).get("rateLimit") or {}).get("rate")
sys.exit(0 if rate == 50 else 1)
' || fail "API output does not reflect the post-import update"

  matches="$(rules_with_display_name "$display_name")"
  [[ -n "$matches" && "$matches" != *$'\n'* ]] \
    || fail "After apply, expected exactly one rule named '${display_name}', got: ${matches:-none}"
  [[ "${matches#*,}" == "$listed_id" ]] \
    || fail "After apply, expected the rule with id '${listed_id}', got '${matches#*,}'; the apply created a new rule instead of updating the imported one"
  info "Update-after-import verified via the API."

  assert_idempotent "$work_dir"

  # -------------------------------------------------------------------------
  # Step 8: Destroy + verify server-side deletion.
  # -------------------------------------------------------------------------
  info "Step 8: Destroying the imported rule via Terraform..."
  TF_VAR_dataset="$DATASET" tf_destroy "$work_dir"

  info "Step 8b: Verifying server-side deletion..."
  assert_sampling_rule_deleted "$identifier" "$DATASET"
  matches="$(rules_with_display_name "$display_name")"
  [[ -z "$matches" ]] || fail "After destroy, rules named '${display_name}' remain: ${matches}"

  info "Case import by ${case_name} PASSED."
}

run_import_case origin
run_import_case id

info "=== dash0_sampling_rule import roundtrip test PASSED ==="
