#!/usr/bin/env bash
# Roundtrip test for dash0_slo.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_slo ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create SLO
# ---------------------------------------------------------------------------
info "Step 1: Creating SLO via Terraform..."

cat > "${WORK_DIR}/slo.yaml" <<'YAMLEOF'
apiVersion: openslo/v1
kind: SLO
metadata:
  name: roundtrip-test-slo
  annotations:
    dash0.com/display-name: Roundtrip test SLO
    dash0.com/enabled: "true"
spec:
  description: 99 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99% availability
      target: 0.99
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_slo" "test" {
  dataset  = var.dataset
  slo_yaml = file("${path.module}/slo.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_slo.test.origin
}

output "url" {
  value = dash0_slo.test.url
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created SLO with origin: ${ORIGIN}"

# Verify the computed url attribute is a Dash0 web app deep link.
URL="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" url)"
info "SLO url: ${URL}"
echo "$URL" | grep -Eq '^https://app\..+/goto/.+' \
  || fail "SLO url output '${URL}' is not a valid deep link"
info "SLO url check PASSED."

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying SLO exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 slos get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find SLO ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test-slo" \
  || fail "CLI output does not contain expected SLO name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/slo.yaml" "$CLI_OUTPUT" \
  || fail "Uploaded and downloaded SLO YAMLs are not equivalent"
info "SLO equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating SLO (changing objective target and description)..."

cat > "${WORK_DIR}/slo.yaml" <<'YAMLEOF'
apiVersion: openslo/v1
kind: SLO
metadata:
  name: roundtrip-test-slo
  annotations:
    dash0.com/display-name: Roundtrip test SLO
    dash0.com/enabled: "true"
spec:
  description: 99.5 percent of checkout HTTP requests succeed over a rolling 28-day window.
  service: checkout
  budgetingMethod: Occurrences
  timeWindow:
    - duration: 28d
      isRolling: true
  indicator:
    metadata:
      name: checkout-success-ratio
    spec:
      ratioMetric:
        counter: true
        good:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout",http_response_status_code!~"5.."}'
        total:
          metricSource:
            type: Prometheus
            spec:
              query: 'http_server_request_duration_seconds_count{service_name="checkout"}'
  objectives:
    - displayName: 99.5% availability
      target: 0.995
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "SLO updated."

CLI_OUTPUT="$(dash0 slos get "$ORIGIN" --dataset "$DATASET" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "0.995" \
  || fail "CLI output does not reflect the update"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying SLO via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "SLO destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying SLO is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_slo roundtrip test PASSED ==="
