#!/usr/bin/env bash
# Roundtrip test for dash0_sampling_rule.
#
# The dash0 CLI has no sampling-rules command, so the rule is read back through
# the raw API command (`dash0 -X api`, see sampling_rule_get in common.sh).
# Sampling rule deletes are hard, so deletion is proven by a 404 on GET.
#
# The rule's only condition is an OTTL expression that matches no span, so
# enabling it in the shared test dataset never changes which traces are kept.
#
# Steps:
#   0. Preflight — creating a sampling rule requires the admin role
#   1. Create via Terraform, assert a tf_-prefixed origin
#   2. Verify via the API + YAML equivalence
#   3. Update (display name + rate limit) and verify via the API
#  3b. Toggle `spec.enabled` to false and assert it round-trips and settles —
#      the end-to-end proof that spec.enabled participates in drift detection
#   4. Idempotency
#   5. Destroy
#   6. Verify deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_sampling_rule ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# Step 0a: Preflight — see sampling_rule_preflight in common.sh for why this can skip.
info "Step 0a: Preflight — checking sampling rules API access..."
sampling_rule_preflight "dash0_sampling_rule roundtrip lifecycle test"

# ---------------------------------------------------------------------------
# Step 0b: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create the rule
# ---------------------------------------------------------------------------
info "Step 1: Creating sampling rule via Terraform..."

cat > "${WORK_DIR}/sampling_rule.yaml" <<'YAMLEOF'
kind: Dash0Sampling
metadata:
  name: roundtrip-test-sampling-rule
spec:
  enabled: true
  display:
    name: Roundtrip Test Sampling Rule
  conditions:
    kind: ottl
    spec:
      ottl: span.name == "dash0-terraform-roundtrip-matches-nothing"
  rateLimit:
    rate: 100
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_sampling_rule" "test" {
  dataset            = var.dataset
  sampling_rule_yaml = file("${path.module}/sampling_rule.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_sampling_rule.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created sampling rule with origin: ${ORIGIN}"

[[ "$ORIGIN" == tf_* ]] \
  || fail "Expected a Terraform-generated origin with the tf_ prefix, got: ${ORIGIN}"
info "Origin prefix check PASSED."

# ---------------------------------------------------------------------------
# Step 2: Verify via the API
# ---------------------------------------------------------------------------
info "Step 2: Verifying the rule exists via the API..."

API_OUTPUT="$(sampling_rule_get "$ORIGIN" "$DATASET" 2>&1)" \
  || fail "API could not find sampling rule ${ORIGIN}: ${API_OUTPUT}"
echo "$API_OUTPUT"

echo "$API_OUTPUT" | grep -q "Roundtrip Test Sampling Rule" \
  || fail "API output does not contain the expected display name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/sampling_rule.yaml" "$API_OUTPUT" \
  || fail "Uploaded and downloaded sampling rules are not equivalent."
info "YAML equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating the rule (display name and rate limit)..."

cat > "${WORK_DIR}/sampling_rule.yaml" <<'YAMLEOF'
kind: Dash0Sampling
metadata:
  name: roundtrip-test-sampling-rule
spec:
  enabled: true
  display:
    name: Roundtrip Test Sampling Rule - Updated
  conditions:
    kind: ottl
    spec:
      ottl: span.name == "dash0-terraform-roundtrip-matches-nothing"
  rateLimit:
    rate: 50
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Rule updated."

API_OUTPUT="$(sampling_rule_get "$ORIGIN" "$DATASET" 2>&1)"
echo "$API_OUTPUT"
assert_yaml_equivalent "${WORK_DIR}/sampling_rule.yaml" "$API_OUTPUT" \
  || fail "API output does not reflect the updated display name and rate limit"
info "Update verified via the API."

# ---------------------------------------------------------------------------
# Step 3b: `spec.enabled` round-trips.
#
# spec.enabled is required by ValidateConfig — the provider always sends a value
# for it — so it must participate in drift detection: toggling it here and
# asserting an empty plan afterwards proves the round trip settles.
# ---------------------------------------------------------------------------
info "Step 3b: Disabling the rule, then asserting the plan settles..."

sed -i 's/^  enabled: true$/  enabled: false/' "${WORK_DIR}/sampling_rule.yaml"
grep -q '^  enabled: false$' "${WORK_DIR}/sampling_rule.yaml" \
  || fail "Test setup: could not switch spec.enabled to false"

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

API_OUTPUT="$(sampling_rule_get "$ORIGIN" "$DATASET" 2>&1)"
assert_yaml_equivalent "${WORK_DIR}/sampling_rule.yaml" "$API_OUTPUT" \
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
# ---------------------------------------------------------------------------
info "Step 6: Verifying the rule is gone server-side..."
assert_sampling_rule_deleted "$ORIGIN" "$DATASET"

info "=== dash0_sampling_rule roundtrip test PASSED ==="
