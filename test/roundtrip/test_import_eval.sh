#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_eval.
#
# An eval is created out-of-band through `dash0 -X evals`, adopted with
# `terraform import` using the `dataset,identifier` ID, and must then plan
# clean, keep its origin, accept updates, and be destroyed.
#
# Steps:
#   1. Create the eval via the CLI (non-Terraform origin)
#   2. Discover the identifier from the CLI list output
#   3. Export its JSON via the CLI and write the Terraform resource shell
#   4. `terraform import` with the `dataset,identifier` ID
#   5. `terraform plan` reports no changes, and the origin is preserved
#   6. Modify + apply, twice — the stale dash0.com/version in the exported
#      document must not make the second apply fail
#   7. Destroy + verify server-side deletion
#   8. Importing the deleted eval is refused

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_eval) ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

write_provider_tf "$WORK_DIR"

SUFFIX="$(date +%s)-${RANDOM}"
OOB_ORIGIN="roundtrip-import-eval-${SUFFIX}"
EVAL_NAME="roundtrip-import-eval-${SUFFIX}"

# ---------------------------------------------------------------------------
# Step 1: Create the eval via the CLI. It is disabled and samples nothing.
# ---------------------------------------------------------------------------
info "Step 1: Creating eval via the CLI (origin ${OOB_ORIGIN})..."

cat > "${WORK_DIR}/create.json" <<JSONEOF
{
  "kind": "Dash0GenerativeAiEvaluation",
  "metadata": {"name": "${EVAL_NAME}", "labels": {"dash0.com/origin": "${OOB_ORIGIN}"}},
  "spec": {
    "enabled": false,
    "display": {"name": "Roundtrip Import Test Eval"},
    "match": {"filters": [{"key": "gen_ai.operation.name", "operator": "is_set"}]},
    "sample": {"rate": 0},
    "judge": {
      "model": "light",
      "systemPrompt": "You judge whether an answer is relevant to the question.",
      "userPrompt": "Question: {{ gen_ai.input.messages }}",
      "output": {"kind": "categorical", "spec": {"options": [{"label": "relevant", "intent": "positive"}, {"label": "off-topic", "intent": "negative"}]}}
    }
  }
}
JSONEOF

dash0 -X evals create --dataset "$DATASET" -f "${WORK_DIR}/create.json" >/dev/null \
  || fail "Failed to create eval via the CLI"

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the CLI list output.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via the CLI..."

IDENTIFIER="$(dash0 -X evals list --dataset "$DATASET" --all -o json \
  | python3 -c '
import json, sys
for it in json.load(sys.stdin):
    if it.get("metadata", {}).get("name") == sys.argv[1]:
        labels = it["metadata"].get("labels") or {}
        print(labels.get("dash0.com/origin") or labels.get("dash0.com/id") or "")
        break
' "$EVAL_NAME")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for '${EVAL_NAME}'"
[[ "$IDENTIFIER" != tf_* ]] || fail "Expected a non-Terraform identifier, got: ${IDENTIFIER}"
info "Identifier: ${IDENTIFIER}"

# ---------------------------------------------------------------------------
# Step 3: Export the document (JSON is valid YAML) and write the resource.
# ---------------------------------------------------------------------------
info "Step 3: Exporting the eval + writing Terraform config..."

eval_get "$IDENTIFIER" "$DATASET" > "${WORK_DIR}/eval.yaml" \
  || fail "Failed to export the eval"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_eval" "imported" {
  dataset   = var.dataset
  eval_yaml = file("${path.module}/eval.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_eval.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4 + 5: Import, then a clean plan with the origin preserved.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import..."
TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_eval.imported" "${DATASET},${IDENTIFIER}" \
  || fail "terraform import failed"

info "Step 5: Asserting a clean plan and a preserved origin..."
assert_idempotent "$WORK_DIR"
STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
[[ "$STATE_ORIGIN" == "$IDENTIFIER" ]] \
  || fail "Expected imported origin '${IDENTIFIER}' in state, got '${STATE_ORIGIN}'"

# ---------------------------------------------------------------------------
# Step 6: Modify + apply, twice.
# ---------------------------------------------------------------------------
for rate in 0.01 0.02; do
  info "Step 6: Setting the sample rate to ${rate} + applying..."
  python3 - "${WORK_DIR}/eval.yaml" "$rate" <<'PYEOF'
import sys, yaml
path, rate = sys.argv[1], float(sys.argv[2])
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["sample"]["rate"] = rate
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF
  TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
  eval_get "$IDENTIFIER" "$DATASET" | python3 -c '
import json, sys
sys.exit(0 if json.load(sys.stdin)["spec"]["sample"]["rate"] == float(sys.argv[1]) else 1)
' "$rate" || fail "API output does not reflect the sample rate ${rate}"
done
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 7: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 7: Destroying the imported eval..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
assert_eval_deleted "$IDENTIFIER" "$DATASET"

# ---------------------------------------------------------------------------
# Step 8: The deleted eval cannot be imported; a GET still finds its tombstone.
# ---------------------------------------------------------------------------
info "Step 8: Asserting the deleted eval cannot be imported..."
set +e
IMPORT_OUTPUT="$(TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_eval.imported" "${DATASET},${IDENTIFIER}" 2>&1)"
IMPORT_RC=$?
set -e
if [[ "$IMPORT_RC" -eq 0 ]]; then
  echo "$IMPORT_OUTPUT"
  fail "terraform import of a deleted eval succeeded; it must be refused"
fi
if ! echo "$IMPORT_OUTPUT" | grep -q "Error Importing Eval" || ! echo "$IMPORT_OUTPUT" | grep -q "deleted"; then
  echo "$IMPORT_OUTPUT"
  fail "terraform import of a deleted eval failed, but not with the tombstone error"
fi

info "=== dash0_eval import roundtrip test PASSED ==="
