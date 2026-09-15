#!/usr/bin/env bash
# Roundtrip test for dash0_time_series_aggregation.
#
# Mirrors test_view.sh, minus its step-3 `url` assertion: time series
# aggregations have no deeplink asset type, so the resource deliberately
# exposes no `url` attribute.
#
# Steps:
#   0. Preflight — every TSA endpoint requires the organization-admin role
#   1. Create via Terraform, assert a tf_-prefixed origin
#   2. Verify via the dash0 CLI + YAML equivalence
#   3. Update (sample interval + display name) and verify via the CLI
#  3b. Toggle `spec.enabled` to false and assert it round-trips and settles —
#      the end-to-end proof that spec.enabled participates in drift detection
#      and that the server-defaulted spec.priority does not leak into it
#   4. Idempotency
#   5. Destroy
#   6. Verify deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_time_series_aggregation ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# Step 0a: Preflight — see tsa_preflight in common.sh for why this can skip.
info "Step 0a: Preflight — checking TSA API access..."
tsa_preflight "dash0_time_series_aggregation roundtrip lifecycle test"

# ---------------------------------------------------------------------------
# Step 0b: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create the aggregation
# ---------------------------------------------------------------------------
info "Step 1: Creating time series aggregation via Terraform..."

cat > "${WORK_DIR}/tsa.yaml" <<'YAMLEOF'
apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: roundtrip-test-tsa
spec:
  enabled: true
  display:
    name: Roundtrip Test TSA
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 5m
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_time_series_aggregation" "test" {
  dataset                      = var.dataset
  time_series_aggregation_yaml = file("${path.module}/tsa.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_time_series_aggregation.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created time series aggregation with origin: ${ORIGIN}"

[[ "$ORIGIN" == tf_* ]] \
  || fail "Expected a Terraform-generated origin with the tf_ prefix, got: ${ORIGIN}"
info "Origin prefix check PASSED."

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying the aggregation exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 time-series-aggregations get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find time series aggregation ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "Roundtrip Test TSA" \
  || fail "CLI output does not contain the expected display name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent_eventually "${WORK_DIR}/tsa.yaml" "dash0 time-series-aggregations" "$ORIGIN" "$DATASET"

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating the aggregation (sample interval and display name)..."

cat > "${WORK_DIR}/tsa.yaml" <<'YAMLEOF'
apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: roundtrip-test-tsa
spec:
  enabled: true
  display:
    name: Roundtrip Test TSA - Updated
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 10m
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Aggregation updated."

CLI_OUTPUT="$(dash0 time-series-aggregations get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT"
echo "$CLI_OUTPUT" | grep -qi "Updated" \
  || fail "CLI output does not reflect the updated display name"
echo "$CLI_OUTPUT" | grep -q "10m" \
  || fail "CLI output does not reflect the updated sample interval"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 3b: `spec.enabled` round-trips, and `spec.priority` does not leak.
#
# spec.enabled is required by ValidateConfig — the provider always sends a value
# for it — so it must participate in drift detection: toggling it here and
# asserting an empty plan afterwards proves the round trip settles.
#
# The same config omits spec.priority, which the server defaults to 2. That
# field IS conditionally ignored, so the empty plan simultaneously proves the
# default does not leak into drift detection. Unit tests can only prove this at
# the normalizer level.
# ---------------------------------------------------------------------------
info "Step 3b: Disabling the aggregation, then asserting the plan settles..."

cat > "${WORK_DIR}/tsa.yaml" <<'YAMLEOF'
apiVersion: dash0.com/v1alpha1
kind: Dash0TimeSeriesAggregation
metadata:
  name: roundtrip-test-tsa
spec:
  enabled: false
  display:
    name: Roundtrip Test TSA - Updated
  match:
    metricNameMatcher:
      operator: is
      value: http.server.request.duration
  sample:
    interval: 10m
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 time-series-aggregations get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qE '^[[:space:]]*enabled:[[:space:]]*false' \
  || fail "CLI output does not reflect spec.enabled: false after the disable"

set +e
TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$WORK_DIR"
PLAN_RC=$?
set -e
if [[ "$PLAN_RC" -ne 0 ]]; then
  fail "Plan after toggling spec.enabled reported changes (exit ${PLAN_RC}) — spec.enabled is not settling, or the server-defaulted spec.priority is leaking into drift detection."
fi
info "Drift check PASSED: spec.enabled round-trips and spec.priority does not leak."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying the aggregation via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Aggregation destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion, server-side.
#
# assert_deleted_via_tf is deliberately NOT used here: it only plans against the
# state tf_destroy just emptied, so it reports a pending create — and passes —
# whether or not the server actually deleted anything. Only `list` proves it.
# ---------------------------------------------------------------------------
info "Step 6: Verifying the aggregation is gone server-side..."
assert_tsa_deleted_via_list "$ORIGIN" "$DATASET"

info "=== dash0_time_series_aggregation roundtrip test PASSED ==="
