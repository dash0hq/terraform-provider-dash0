#!/usr/bin/env bash
# Roundtrip test for dash0_eval.
#
# The eval is read back and changed out of band through `dash0 -X evals`.
# Eval deletes are soft, so deletion is proven by a tombstone (or a 404) on GET.
#
# The eval is disabled and samples nothing, so it never calls the judge.
#
# Steps:
#   1. Create via Terraform, assert a tf_-prefixed origin
#   2. Verify via the API + YAML equivalence
#   3. Update (display name + output threshold) and verify via the API
#  3b. Change the display name out of band and assert the plan shows it;
#      apply restores it
#  3c. Rename metadata.name and assert the plan replaces the eval, the new
#      eval gets a new origin, and the old one is deleted
#  3d. Change the judge output kind and assert the plan replaces the eval
#   4. Idempotency
#   5. Destroy
#   6. Verify deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_eval ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

write_provider_tf "$WORK_DIR"

# Eval names are unique per dataset, so concurrent runs must not share one.
SUFFIX="$(date +%s)-${RANDOM}"
EVAL_NAME="roundtrip-eval-${SUFFIX}"

# plan_exit_code — prints the exit code of `terraform plan -detailed-exitcode`
# and saves the plan output, without colour codes, to plan.txt. The colour
# codes split phrases such as "must be replaced", so a grep would miss them.
plan_exit_code() {
  set +e
  TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR" > "${WORK_DIR}/plan.raw" 2>&1
  local rc=$?
  set -e
  sed 's/\x1b\[[0-9;]*m//g' "${WORK_DIR}/plan.raw" > "${WORK_DIR}/plan.txt"
  echo "$rc"
}

# eval_value <origin> <python-expression> — prints the expression, evaluated
# against the eval JSON bound to `d`.
eval_value() {
  eval_get "$1" "$DATASET" | python3 -c '
import json, sys
d = json.load(sys.stdin)
print(eval(sys.argv[1]))
' "$2"
}

# assert_plan_replaces — the next plan must destroy and re-create the eval.
assert_plan_replaces() {
  local rc
  rc="$(plan_exit_code)"
  if [[ "$rc" -ne 2 ]] || ! grep -q "must be replaced" "${WORK_DIR}/plan.txt"; then
    cat "${WORK_DIR}/plan.txt"
    fail "$1: the plan does not replace the eval (exit ${rc})."
  fi
}

# ---------------------------------------------------------------------------
# Step 1: Create the eval
# ---------------------------------------------------------------------------
info "Step 1: Creating eval via Terraform..."

cat > "${WORK_DIR}/eval.yaml" <<YAMLEOF
kind: Dash0GenerativeAiEvaluation
metadata:
  name: ${EVAL_NAME}
  annotations:
    dash0.com/folder-path: /terraform-roundtrip
spec:
  enabled: false
  display:
    name: Roundtrip Test Eval
  match:
    filters:
      - key: gen_ai.operation.name
        operator: is_set
  sample:
    rate: 0
  judge:
    model: light
    systemPrompt: You judge whether an answer is relevant to the question.
    userPrompt: "Question: {{ gen_ai.input.messages }}"
    output:
      kind: numeric
      spec:
        threshold: 0.5
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_eval" "test" {
  dataset   = var.dataset
  eval_yaml = file("${path.module}/eval.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_eval.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created eval with origin: ${ORIGIN}"
[[ "$ORIGIN" == tf_* ]] \
  || fail "Expected a Terraform-generated origin with the tf_ prefix, got: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying the eval via the API..."

API_OUTPUT="$(eval_get "$ORIGIN" "$DATASET" 2>&1)" \
  || fail "API could not find eval ${ORIGIN}: ${API_OUTPUT}"
echo "$API_OUTPUT"
assert_yaml_equivalent "${WORK_DIR}/eval.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded evals are not equivalent."
[[ "$(eval_value "$ORIGIN" 'd["metadata"]["labels"]["dash0.com/source"]')" == "terraform" ]] \
  || fail "The eval does not report source terraform"
info "YAML equivalence and source checks PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update in place
# ---------------------------------------------------------------------------
info "Step 3: Updating the display name and threshold..."

sed -i \
  -e 's/^    name: Roundtrip Test Eval$/    name: Roundtrip Test Eval - Updated/' \
  -e 's/^        threshold: 0.5$/        threshold: 0.7/' \
  "${WORK_DIR}/eval.yaml"
grep -q '^        threshold: 0.7$' "${WORK_DIR}/eval.yaml" \
  || fail "Test setup: could not change the threshold"

RC="$(plan_exit_code)"
if [[ "$RC" -ne 2 ]] || grep -q "must be replaced" "${WORK_DIR}/plan.txt"; then
  cat "${WORK_DIR}/plan.txt"
  fail "An in-place change did not plan an in-place update (exit ${RC})."
fi

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
API_OUTPUT="$(eval_get "$ORIGIN" "$DATASET" 2>&1)"
assert_yaml_equivalent "${WORK_DIR}/eval.yaml" "$API_OUTPUT" \
  || fail "API output does not reflect the update"
[[ "$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)" == "$ORIGIN" ]] \
  || fail "An in-place update changed the origin"
info "Update verified via the API."

# ---------------------------------------------------------------------------
# Step 3b: An out-of-band change is drift, and apply restores it.
# ---------------------------------------------------------------------------
info "Step 3b: Changing the display name out of band..."

eval_get "$ORIGIN" "$DATASET" > "${WORK_DIR}/oob.json"
python3 - "${WORK_DIR}/oob.json" <<'PYEOF'
import json, sys
path = sys.argv[1]
with open(path) as f:
    doc = json.load(f)
doc["spec"]["display"]["name"] = "Changed out of band"
with open(path, "w") as f:
    json.dump(doc, f)
PYEOF
dash0 -X evals update "$ORIGIN" --dataset "$DATASET" -f "${WORK_DIR}/oob.json" >/dev/null \
  || fail "Out-of-band update of eval ${ORIGIN} failed"

RC="$(plan_exit_code)"
[[ "$RC" -eq 2 ]] || { cat "${WORK_DIR}/plan.txt"; fail "The out-of-band change is not reported as drift (exit ${RC}, want 2)."; }

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
[[ "$(eval_value "$ORIGIN" 'd["spec"]["display"]["name"]')" == "Roundtrip Test Eval - Updated" ]] \
  || fail "Apply did not restore the display name"
info "Drift check PASSED."

# ---------------------------------------------------------------------------
# Step 3c: Renaming replaces the eval. The API refuses a rename, so an
# in-place update would fail at apply.
# ---------------------------------------------------------------------------
info "Step 3c: Renaming metadata.name..."

sed -i "s/^  name: ${EVAL_NAME}\$/  name: ${EVAL_NAME}-renamed/" "${WORK_DIR}/eval.yaml"
grep -q "^  name: ${EVAL_NAME}-renamed\$" "${WORK_DIR}/eval.yaml" \
  || fail "Test setup: could not rename the eval"

assert_plan_replaces "Renaming the eval"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

OLD_ORIGIN="$ORIGIN"
ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
[[ "$ORIGIN" != "$OLD_ORIGIN" ]] || fail "The replacement kept the old origin ${OLD_ORIGIN}"
[[ "$(eval_value "$ORIGIN" 'd["metadata"]["name"]')" == "${EVAL_NAME}-renamed" ]] \
  || fail "The replacement eval does not carry the new name"
assert_eval_deleted "$OLD_ORIGIN" "$DATASET"
info "Rename replaced the eval."

# ---------------------------------------------------------------------------
# Step 3d: Changing the output kind replaces the eval.
# ---------------------------------------------------------------------------
info "Step 3d: Changing the judge output kind to boolean..."

python3 - "${WORK_DIR}/eval.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["judge"]["output"] = {"kind": "boolean"}
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

assert_plan_replaces "Changing the output kind"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
OLD_ORIGIN="$ORIGIN"
ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
[[ "$ORIGIN" != "$OLD_ORIGIN" ]] || fail "The replacement kept the old origin ${OLD_ORIGIN}"
assert_eval_deleted "$OLD_ORIGIN" "$DATASET"
info "Output kind change replaced the eval."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5 + 6: Destroy and verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 5: Destroying the eval via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 6: Verifying the eval is gone server-side..."
assert_eval_deleted "$ORIGIN" "$DATASET"

info "=== dash0_eval roundtrip test PASSED ==="
