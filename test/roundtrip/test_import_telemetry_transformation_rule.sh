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
    warn "SKIPPED: telemetry transformation import did not run: the credential lacks permissions (HTTP 403). Coverage gap, not a pass."
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

assert_imported_rule_matches() {
  local output
  output="$(rule_get "$IDENTIFIER")" || fail "Could not read imported rule ${IDENTIFIER}"
  assert_yaml_equivalent "${CASE_DIR}/rule.yaml" "$output" \
    || fail "Imported rule does not match the managed YAML"
  printf '%s' "$output" > "${CASE_DIR}/actual.json"
  # Whole-spec comparison preserves transformation and statement order, including example removal.
  python3 - "${CASE_DIR}/rule.yaml" "${CASE_DIR}/actual.json" "$OOB_ORIGIN" "$ID" "$DATASET" <<'PYEOF'
import json, sys, yaml
with open(sys.argv[1]) as f:
    expected = yaml.safe_load(f)
with open(sys.argv[2]) as f:
    actual = json.load(f)
labels = actual["metadata"]["labels"]
assert labels["dash0.com/origin"] == sys.argv[3], labels
assert labels["dash0.com/id"] == sys.argv[4], labels
assert labels["dash0.com/dataset"] == sys.argv[5], labels
for doc in (expected, actual):
    for key in ("conditions", "examples"):
        if not doc["spec"].get(key):
            doc["spec"].pop(key, None)
assert actual["spec"]["enabled"] is False, actual
assert expected["spec"] == actual["spec"], (expected["spec"], actual["spec"])
PYEOF
}

info "=== Roundtrip test: terraform import (dash0_telemetry_transformation_rule) ==="
rule_preflight

for mode in origin id; do
  CASE_DIR="${WORK_DIR}/${mode}"
  mkdir "$CASE_DIR"
  write_provider_tf "$CASE_DIR"
  OOB_ORIGIN="roundtrip-import-transformation-${mode}-$(date +%s)-${RANDOM}"

  cat > "${CASE_DIR}/create.yaml" <<'YAMLEOF'
kind: Dash0TelemetryTransformationRule
metadata:
  name: roundtrip-import-telemetry-transformation-rule
spec:
  enabled: false
  display:
    name: Roundtrip import telemetry transformation rule
  signal: logs
  context: log
  priority: 200
  transformations:
    - name: z-copy
      statements:
        - 'set(attributes["normalized"], attributes["legacy"])'
        - 'set(attributes["copied"], true)'
    - name: a-cleanup
      statements:
        - 'delete_key(attributes, "legacy")'
  examples:
    - name: Import example
      otlp:
        resourceLogs:
          - scopeLogs:
              - logRecords:
                  - body:
                      stringValue: Import example log
                    attributes:
                      - key: legacy
                        value:
                          stringValue: example-value
YAMLEOF
  python3 - "${CASE_DIR}/create.yaml" "$OOB_ORIGIN" > "${CASE_DIR}/create.json" <<'PYEOF'
import json, sys, yaml
with open(sys.argv[1]) as f:
    doc = yaml.safe_load(f)
doc["metadata"]["labels"] = {"dash0.com/origin": sys.argv[2]}
json.dump(doc, sys.stdout)
PYEOF

  info "Creating an out-of-band disabled rule for import by ${mode}..."
  dash0 -X api POST /api/telemetry-transformation-rules --dataset "$DATASET" \
    -f "${CASE_DIR}/create.json" > "${CASE_DIR}/created.json" \
    || fail "Could not create the out-of-band rule"

  info "Discovering the identifier from the list, which must omit stored examples..."
  dash0 -X api /api/telemetry-transformation-rules --dataset "$DATASET" > "${CASE_DIR}/list.json" \
    || fail "Could not list telemetry transformation rules"
  IDENTIFIER="$(python3 - "${CASE_DIR}/list.json" "$OOB_ORIGIN" "$mode" <<'PYEOF'
import json, sys
with open(sys.argv[1]) as f:
    items = json.load(f)["telemetryTransformationRules"]
matches = [r for r in items if r["metadata"]["labels"].get("dash0.com/origin") == sys.argv[2]]
assert len(matches) == 1, matches
rule = matches[0]
assert "examples" not in rule["spec"], "List must not be used as a full rule export"
print(rule["metadata"]["labels"]["dash0.com/" + sys.argv[3]])
PYEOF
)" || fail "Could not discover the rule's ${mode} in the list response"
  [[ -n "$IDENTIFIER" ]] || fail "The discovered identifier is empty"

  info "Exporting the full GET response, including examples, and removing the concurrency timestamp..."
  rule_get "$IDENTIFIER" > "${CASE_DIR}/export.json" || fail "Could not export the rule"
  ID="$(python3 - "${CASE_DIR}/export.json" "${CASE_DIR}/rule.yaml" <<'PYEOF'
import json, sys, yaml
with open(sys.argv[1]) as f:
    doc = json.load(f)
assert len(doc["spec"]["examples"]) == 1, doc
doc["metadata"].get("annotations", {}).pop("dash0.com/updated-at", None)
with open(sys.argv[2], "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
print(doc["metadata"]["labels"]["dash0.com/id"])
PYEOF
)" || fail "Full GET did not contain the stored example or server ID"

  cat > "${CASE_DIR}/main.tf" <<'EOF'
resource "dash0_telemetry_transformation_rule" "imported" {
  dataset                            = var.dataset
  telemetry_transformation_rule_yaml = file("${path.module}/rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_telemetry_transformation_rule.imported.origin
}

output "id" {
  value = dash0_telemetry_transformation_rule.imported.id
}
EOF

  tf_init "$CASE_DIR"
  TF_VAR_dataset="$DATASET" tf_import "$CASE_DIR" dash0_telemetry_transformation_rule.imported "${DATASET},${IDENTIFIER}"
  assert_idempotent "$CASE_DIR"
  STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$CASE_DIR" origin)"
  [[ "$STATE_ORIGIN" == "$IDENTIFIER" ]] \
    || fail "Import changed the supplied identifier: ${STATE_ORIGIN}"
  [[ "$(TF_VAR_dataset="$DATASET" tf_output "$CASE_DIR" id)" == "$ID" ]] || fail "Import changed the server ID"
  assert_imported_rule_matches

  for change in update_examples omit_examples; do
    info "Import by ${mode}: ${change}, apply, and verify idempotency..."
    python3 - "${CASE_DIR}/rule.yaml" "$change" <<'PYEOF'
import sys, yaml
path, change = sys.argv[1:]
with open(path) as f:
    doc = yaml.safe_load(f)
doc["metadata"].get("annotations", {}).pop("dash0.com/updated-at", None)
if change == "update_examples":
    doc["spec"]["examples"][0]["name"] = "Updated import example"
    doc["spec"]["examples"][0]["otlp"]["resourceLogs"][0]["scopeLogs"][0]["logRecords"][0]["body"]["stringValue"] = "Updated import example log"
    doc["spec"]["transformations"][0]["statements"].reverse()
else:
    doc["spec"].pop("examples")
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF
    TF_VAR_dataset="$DATASET" tf_apply "$CASE_DIR"
    [[ "$(TF_VAR_dataset="$DATASET" tf_output "$CASE_DIR" origin)" == "$STATE_ORIGIN" ]] || fail "Update changed the imported identifier"
    [[ "$(TF_VAR_dataset="$DATASET" tf_output "$CASE_DIR" id)" == "$ID" ]] || fail "Update changed the imported ID"
    assert_imported_rule_matches
    assert_idempotent "$CASE_DIR"
  done

  TF_VAR_dataset="$DATASET" tf_destroy "$CASE_DIR"
  assert_rule_deleted "$IDENTIFIER"
done

info "=== dash0_telemetry_transformation_rule import roundtrip test PASSED ==="
