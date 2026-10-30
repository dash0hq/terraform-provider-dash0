#!/usr/bin/env bash
# Roundtrip test for dash0_signal_to_metrics.
#
# The dash0 CLI has no signal-to-metrics command, so the rule is read back
# through the raw API command (`dash0 -X api`, see signal_to_metrics_get in
# common.sh). Signal-to-metrics deletes are soft, so deletion is proven by a
# tombstone (or a 404) on GET.
#
# The rule's filter matches no span, so enabling it in the shared test dataset
# never produces metric data points.
#
# Steps:
#   0. Preflight — the credential must be allowed to manage signal-to-metrics rules
#   1. Create via Terraform, assert a tf_-prefixed origin
#   2. Verify via the API + YAML equivalence
#   3. Update (display name + interval) and verify via the API
#  3b. Toggle `spec.enabled` to false and assert it round-trips and settles —
#      the end-to-end proof that spec.enabled participates in drift detection
#  3c. Set `spec.targetDatasetMode: original` out of band and assert the plan
#      stays clean — the server default must not read as drift
#  3d. Change the display name out of band and assert the plan shows it —
#      the counterpart of 3c: a real change is drift; apply restores it
#   4. Idempotency
#   5. Destroy
#   6. Verify deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_signal_to_metrics ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# Step 0a: Preflight — see signal_to_metrics_preflight in common.sh for why this can skip.
info "Step 0a: Preflight — checking signal-to-metrics API access..."
signal_to_metrics_preflight "dash0_signal_to_metrics roundtrip lifecycle test"

# ---------------------------------------------------------------------------
# Step 0b: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# out_of_band_edit <origin> <python expression on doc>
#
# Changes the rule outside Terraform: GET it, apply the expression to the
# parsed document, and PUT it back through the raw API command.
out_of_band_edit() {
  local origin="$1" edit="$2"
  signal_to_metrics_get "$origin" "$DATASET" > "${WORK_DIR}/oob.json" \
    || fail "Could not read signal-to-metrics rule ${origin} for an out-of-band edit"
  python3 - "${WORK_DIR}/oob.json" "$edit" <<'PYEOF'
import json, sys
path, edit = sys.argv[1], sys.argv[2]
with open(path) as f:
    doc = json.load(f)
exec(edit)
with open(path, "w") as f:
    json.dump(doc, f)
PYEOF
  dash0 -X api PUT "/api/signal-to-metrics/${origin}" --dataset "$DATASET" -f "${WORK_DIR}/oob.json" >/dev/null \
    || fail "Out-of-band PUT of signal-to-metrics rule ${origin} failed"
}

# ---------------------------------------------------------------------------
# Step 1: Create the rule
# ---------------------------------------------------------------------------
info "Step 1: Creating signal-to-metrics rule via Terraform..."

cat > "${WORK_DIR}/signal_to_metrics.yaml" <<'YAMLEOF'
kind: Dash0SignalToMetrics
metadata:
  name: roundtrip-test-signal-to-metrics
spec:
  enabled: true
  display:
    name: Roundtrip Test Signal To Metrics
  match:
    signal: spans
    filters:
      - key: service.name
        operator: is
        value: dash0-terraform-roundtrip-matches-nothing
  output:
    name: dash0_terraform_roundtrip_matches_nothing
    interval: 60s
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_signal_to_metrics" "test" {
  dataset                = var.dataset
  signal_to_metrics_yaml = file("${path.module}/signal_to_metrics.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_signal_to_metrics.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created signal-to-metrics rule with origin: ${ORIGIN}"

[[ "$ORIGIN" == tf_* ]] \
  || fail "Expected a Terraform-generated origin with the tf_ prefix, got: ${ORIGIN}"
info "Origin prefix check PASSED."

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying the rule exists via the API..."

API_OUTPUT="$(signal_to_metrics_get "$ORIGIN" "$DATASET" 2>&1)" \
  || fail "API could not find signal-to-metrics rule ${ORIGIN}: ${API_OUTPUT}"
echo "$API_OUTPUT"

echo "$API_OUTPUT" | grep -q "Roundtrip Test Signal To Metrics" \
  || fail "API output does not contain the expected display name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/signal_to_metrics.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded signal-to-metrics rules are not equivalent."
info "YAML equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating the rule (display name and interval)..."

sed -i \
  -e 's/^    name: Roundtrip Test Signal To Metrics$/    name: Roundtrip Test Signal To Metrics - Updated/' \
  -e 's/^    interval: 60s$/    interval: 5m/' \
  "${WORK_DIR}/signal_to_metrics.yaml"
grep -q '^    interval: 5m$' "${WORK_DIR}/signal_to_metrics.yaml" \
  || fail "Test setup: could not change the interval"

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Rule updated."

API_OUTPUT="$(signal_to_metrics_get "$ORIGIN" "$DATASET" 2>&1)"
echo "$API_OUTPUT"
assert_yaml_equivalent "${WORK_DIR}/signal_to_metrics.yaml" "$API_OUTPUT" \
  || fail "API output does not reflect the updated display name and interval"
info "Update verified via the API."

# ---------------------------------------------------------------------------
# Step 3b: `spec.enabled` round-trips.
#
# spec.enabled is required by ValidateConfig — the provider always sends a value
# for it — so it must participate in drift detection: toggling it here and
# asserting an empty plan afterwards proves the round trip settles.
# ---------------------------------------------------------------------------
info "Step 3b: Disabling the rule, then asserting the plan settles..."

sed -i 's/^  enabled: true$/  enabled: false/' "${WORK_DIR}/signal_to_metrics.yaml"
grep -q '^  enabled: false$' "${WORK_DIR}/signal_to_metrics.yaml" \
  || fail "Test setup: could not switch spec.enabled to false"

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

API_OUTPUT="$(signal_to_metrics_get "$ORIGIN" "$DATASET" 2>&1)"
assert_yaml_equivalent "${WORK_DIR}/signal_to_metrics.yaml" "$API_OUTPUT" \
  || fail "API output does not reflect spec.enabled: false after the disable"

set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_RC=$?
set -e
if [[ "$PLAN_RC" -ne 0 ]]; then
  fail "Plan after toggling spec.enabled reported changes (exit ${PLAN_RC}) — spec.enabled is not settling."
fi
info "Drift check PASSED: spec.enabled round-trips."

# ---------------------------------------------------------------------------
# Step 3c: an explicit `targetDatasetMode: original` is not drift.
#
# The config omits spec.targetDatasetMode. `original` is what the server does
# when it is absent, so a rule that states it explicitly (as the Dash0 UI may)
# must still compare equal. Unit tests prove this at the normalizer level; this
# proves it against what the server actually returns.
# ---------------------------------------------------------------------------
info "Step 3c: Setting targetDatasetMode: original out of band, then asserting a clean plan..."

out_of_band_edit "$ORIGIN" 'doc["spec"]["targetDatasetMode"] = "original"'
signal_to_metrics_get "$ORIGIN" "$DATASET" | grep -q '"targetDatasetMode":"original"' \
  || fail "Test setup: the server did not store targetDatasetMode: original"

set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_RC=$?
set -e
if [[ "$PLAN_RC" -ne 0 ]]; then
  fail "Plan after setting targetDatasetMode: original out of band reported changes (exit ${PLAN_RC}) — the server default reads as drift."
fi
info "Drift check PASSED: targetDatasetMode: original is not drift."

# ---------------------------------------------------------------------------
# Step 3d: a real out-of-band change is drift.
#
# Step 3c proves the normalization hides a server default; this proves the
# comparison still reports a change that is not one.
# ---------------------------------------------------------------------------
info "Step 3d: Changing the display name out of band, then asserting the plan shows it..."

out_of_band_edit "$ORIGIN" 'doc["spec"]["display"]["name"] = "Changed out of band"'

set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_RC=$?
set -e
if [[ "$PLAN_RC" -ne 2 ]]; then
  fail "Plan after changing the display name out of band did not report the change (exit ${PLAN_RC}, want 2)."
fi
info "Drift check PASSED: the display name change is reported."

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
signal_to_metrics_get "$ORIGIN" "$DATASET" | grep -q '"name":"Roundtrip Test Signal To Metrics - Updated"' \
  || fail "Apply did not restore the display name"
info "Apply restored the display name."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying the rule via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Rule destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion, server-side.
#
# assert_deleted_via_tf is deliberately NOT used: it only plans against the
# state tf_destroy just emptied, so it passes whether or not the server deleted
# anything. The rule must come back as a tombstone (or a 404).
# ---------------------------------------------------------------------------
info "Step 6: Verifying the rule is gone server-side..."
assert_signal_to_metrics_deleted "$ORIGIN" "$DATASET"

info "=== dash0_signal_to_metrics roundtrip test PASSED ==="
