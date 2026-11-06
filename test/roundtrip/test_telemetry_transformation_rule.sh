#!/usr/bin/env bash

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

rule_get() {
  dash0 -X api "/api/telemetry-transformation-rules/$1" --dataset "$DATASET"
}

rule_preflight() {
  local output rc
  if output="$(dash0 -X api /api/telemetry-transformation-rules --dataset "$DATASET" 2>&1)"; then
    return
  else
    rc=$?
  fi
  if grep -qE 'HTTP 403([^0-9]|$)' <<<"$output"; then
    warn "SKIPPED: telemetry transformation lifecycle did not run: the credential lacks permissions (HTTP 403). Coverage gap, not a pass."
    exit 77
  fi
  fail "Telemetry transformation preflight failed (exit ${rc}): ${output}"
}

assert_rule_deleted() {
  local identifier="$1" output status attempt
  for attempt in $(seq 1 10); do
    if output="$(rule_get "$identifier" 2>&1)"; then
      status="$(printf '%s' "$output" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
annotations = doc["metadata"].get("annotations") or {}
print("deleted" if annotations.get("dash0.com/deleted-at") else "live")
')" || fail "Unreadable deletion response for ${identifier}"
      [[ "$status" != deleted ]] || return 0
    elif grep -qE 'HTTP 404([^0-9]|$)' <<<"$output"; then
      return 0
    else
      fail "Could not verify deletion of ${identifier}: ${output}"
    fi
    [[ "$attempt" -eq 10 ]] || sleep 3
  done
  fail "Telemetry transformation rule ${identifier} is still live"
}

assert_rule_matches() {
  local output
  output="$(rule_get "$ORIGIN")" || fail "Could not read rule ${ORIGIN}"
  assert_yaml_equivalent "${WORK_DIR}/rule.yaml" "$output" \
    || fail "Uploaded and downloaded telemetry transformation rules differ"
  printf '%s' "$output" > "${WORK_DIR}/actual.json"
  # The common comparator sorts lists of objects; transformations must stay in execution order.
  python3 - "${WORK_DIR}/rule.yaml" "${WORK_DIR}/actual.json" "$DATASET" "$ORIGIN" "$ID" <<'PYEOF'
import json, sys, yaml
with open(sys.argv[1]) as f:
    expected = yaml.safe_load(f)
with open(sys.argv[2]) as f:
    actual = json.load(f)
assert actual["kind"] == "Dash0TelemetryTransformationRule", actual
labels = actual["metadata"]["labels"]
assert labels["dash0.com/dataset"] == sys.argv[3], labels
assert labels["dash0.com/origin"] == sys.argv[4], labels
assert labels["dash0.com/id"] == sys.argv[5], labels
for doc in (expected, actual):
    spec = doc["spec"]
    spec.setdefault("priority", 200)
    for key in ("conditions", "examples"):
        if not spec.get(key):
            spec.pop(key, None)
assert expected["spec"] == actual["spec"], (expected["spec"], actual["spec"])
PYEOF
}

info "=== Roundtrip test: dash0_telemetry_transformation_rule ==="
rule_preflight
write_provider_tf "$WORK_DIR"

cat > "${WORK_DIR}/rule.yaml" <<'YAMLEOF'
kind: Dash0TelemetryTransformationRule
metadata:
  name: roundtrip-telemetry-transformation-rule
spec:
  enabled: false
  display:
    name: Roundtrip telemetry transformation rule
  signal: logs
  context: log
  conditions:
    - 'attributes["legacy"] != nil'
  transformations:
    - name: z-copy
      statements:
        - 'set(attributes["normalized"], attributes["legacy"])'
        - 'set(attributes["copied"], true)'
    - name: a-cleanup
      statements:
        - 'delete_key(attributes, "legacy")'
        - 'delete_key(attributes, "copied")'
  examples:
    - name: Initial example
      otlp:
        resourceLogs:
          - scopeLogs:
              - logRecords:
                  - body:
                      stringValue: Initial example log
                    attributes:
                      - key: legacy
                        value:
                          stringValue: example-value
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_telemetry_transformation_rule" "test" {
  dataset                            = var.dataset
  telemetry_transformation_rule_yaml = file("${path.module}/rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_telemetry_transformation_rule.test.origin
}

output "id" {
  value = dash0_telemetry_transformation_rule.test.id
}
EOF

info "Creating a disabled rule with ordered transformations and a stored example..."
tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
[[ "$ORIGIN" =~ ^tf_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] \
  || fail "Expected a fresh tf_UUID origin, got ${ORIGIN}"
[[ -n "$ID" ]] || fail "The server-assigned ID is empty"
assert_rule_matches
assert_idempotent "$WORK_DIR"

info "Changing only transformation and statement order must produce a plan..."
python3 - "${WORK_DIR}/rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"]["transformations"].reverse()
for transformation in doc["spec"]["transformations"]:
    transformation["statements"].reverse()
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF
if TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"; then
  fail "Changing transformation and statement order did not produce a diff"
else
  PLAN_RC=$?
  [[ "$PLAN_RC" -eq 2 ]] || fail "Order-only plan failed (exit ${PLAN_RC}, expected 2)"
fi
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
assert_rule_matches
assert_idempotent "$WORK_DIR"

info "Updating only the stored example..."
python3 - "${WORK_DIR}/rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
example = doc["spec"]["examples"][0]
example["name"] = "Updated example"
example["otlp"]["resourceLogs"][0]["scopeLogs"][0]["logRecords"][0]["body"]["stringValue"] = "Updated example log"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
[[ "$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)" == "$ORIGIN" ]] || fail "Update changed the origin"
[[ "$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)" == "$ID" ]] || fail "Update changed the ID"
assert_rule_matches
assert_idempotent "$WORK_DIR"

info "Omitting examples must remove them on PUT..."
python3 - "${WORK_DIR}/rule.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["spec"].pop("examples")
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
assert_rule_matches
assert_idempotent "$WORK_DIR"

info "An explicit default priority must not cause drift..."
sed -i '/^  enabled: false$/a\  priority: 200' "${WORK_DIR}/rule.yaml"
assert_idempotent "$WORK_DIR"

info "A dataset change must plan a replacement, without creating another dataset..."
(
  cd "$WORK_DIR" || exit 1
  TF_VAR_dataset="$DATASET" TF_CLI_CONFIG_FILE="${WORK_DIR}/.terraformrc" \
    "$TF" plan -input=false -var="dataset=${DATASET}-replacement-plan-only" -out=replacement.tfplan
  "$TF" show -json replacement.tfplan > replacement.json
)
python3 - "${WORK_DIR}/replacement.json" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    plan = json.load(f)
change = next(r["change"] for r in plan["resource_changes"] if r["address"] == "dash0_telemetry_transformation_rule.test")
assert change["actions"] == ["delete", "create"], change
PYEOF
rm -f "${WORK_DIR}/replacement.tfplan" "${WORK_DIR}/replacement.json"

info "Replacing in the same dataset must generate a fresh origin and ID..."
OLD_ORIGIN="$ORIGIN"
OLD_ID="$ID"
(
  cd "$WORK_DIR" || exit 1
  TF_VAR_dataset="$DATASET" TF_CLI_CONFIG_FILE="${WORK_DIR}/.terraformrc" \
    "$TF" apply -auto-approve -input=false -replace=dash0_telemetry_transformation_rule.test
)
ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
[[ "$ORIGIN" == tf_* && "$ORIGIN" != "$OLD_ORIGIN" ]] || fail "Replacement reused the origin"
[[ -n "$ID" && "$ID" != "$OLD_ID" ]] || fail "Replacement reused the ID"
assert_rule_deleted "$OLD_ORIGIN"
assert_rule_matches
assert_idempotent "$WORK_DIR"

info "Destroying the rule and verifying deletion server-side..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
assert_rule_deleted "$ORIGIN"
info "=== dash0_telemetry_transformation_rule roundtrip test PASSED ==="
