#!/usr/bin/env bash
# Roundtrip test for the dash0_log_event and dash0_deployment_event actions.
#
# Actions have no state to read back via the API — the only way to confirm
# delivery is to query the telemetry they emit. This test invokes both
# actions standalone (`terraform apply -invoke=...`, no resource involved)
# and verifies the resulting records via `dash0 logs query`.
#
# Actions require Terraform 1.14+, which OpenTofu does not implement, so this
# test uses the real `terraform` binary and the tf_actions_* helpers from
# common.sh instead of the tofu-based tf_* helpers every other test uses. It
# also requires an OTLP endpoint (DASH0_OTLP_URL) and a static `auth_`-token
# profile: the Dash0 OTLP/HTTP ingress endpoint these actions send to does not
# accept OAuth access tokens, even though the Dash0 API does.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

: "${DASH0_OTLP_URL:?Environment variable DASH0_OTLP_URL must be set (required by the dash0_log_event and dash0_deployment_event actions); set it directly or via a dash0 CLI profile with an OTLP URL configured}"

require_terraform_actions_support

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# Unique per run so the CLI query below cannot match a previous run's records.
TEST_ID="rt-actions-$$-${RANDOM}"

info "=== Roundtrip test: dash0_log_event / dash0_deployment_event actions ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"
info "Test marker: ${TEST_ID}"

# ---------------------------------------------------------------------------
# Step 0: Provider config (includes otlp_url, required by both actions)
# ---------------------------------------------------------------------------
write_provider_tf_with_otlp "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Declare both actions. Both are tagged with the same service.name so
# a single CLI filter surfaces both records.
# ---------------------------------------------------------------------------
cat > "${WORK_DIR}/main.tf" <<EOF
action "dash0_deployment_event" "test" {
  config {
    service_name                = "${TEST_ID}"
    service_version             = "1.0.0"
    deployment_environment_name = "roundtrip-test"
    deployment_status           = "succeeded"
    dataset                     = "${DATASET}"
  }
}

action "dash0_log_event" "test" {
  config {
    body = "Roundtrip test log event ${TEST_ID}"
    resource_attributes = {
      "service.name" = "${TEST_ID}"
    }
    severity_text = "INFO"
    dataset       = "${DATASET}"
  }
}
EOF

tf_actions_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 2: Invoke dash0_deployment_event standalone (no resource involved).
# ---------------------------------------------------------------------------
info "Step 2: Invoking dash0_deployment_event..."
tf_actions_invoke "$WORK_DIR" "action.dash0_deployment_event.test"

# ---------------------------------------------------------------------------
# Step 3: Invoke dash0_log_event standalone.
# ---------------------------------------------------------------------------
info "Step 3: Invoking dash0_log_event..."
tf_actions_invoke "$WORK_DIR" "action.dash0_log_event.test"

# ---------------------------------------------------------------------------
# Step 4: Verify both records landed via `dash0 logs query`.
# ---------------------------------------------------------------------------
info "Step 4: Verifying delivery via dash0 CLI..."

FILTER="service.name is ${TEST_ID}"

assert_log_record_via_cli "$DATASET" "$FILTER" "dash0.deployment"
info "Deployment event confirmed via CLI."

assert_log_record_via_cli "$DATASET" "$FILTER" "Roundtrip test log event ${TEST_ID}"
info "Log event confirmed via CLI."

info "=== dash0_log_event / dash0_deployment_event actions roundtrip test PASSED ==="
