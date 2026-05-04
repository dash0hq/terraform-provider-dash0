#!/usr/bin/env bash
# Targeted test: Do the ENV variables when specified override values defined in profile?
#
# Creates a dash0 CLI config directory and populates it with a profile which has invalid
# auth token, but we still keep the env variables specified with correct values. We initialise
# provider with empty attributes and a profile name defined to pick up the incorrect profile
# from dash0 CLI config files, but since ENV variables take precedence the profile attribute
# should not affect providers working and should continue working

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Test: Do the ENV variables when specified override values defined in profile? ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Create an dash0 Config Directory with an invalid authToken
# ---------------------------------------------------------------------------

dash0 config profiles create default \
    --api-url "$DASH0_API_URL" \
    --auth-token "something-that-makes-profile-invalid"
dash0 config profiles select default

echo ""
echo "========= dash0 `default` Profile with invalid Auth Token ========="
dash0 config profiles list
echo "=============================================================================="

CONFIG_PRE=$(dash0 config show)
echo ""
echo "========== Config currently used by dash0 CLI =========="
echo "$CONFIG_PRE"
echo "=========================================================="

cat <<EOF

*********
We will not be removing env variables, we want to verify that the incorrect
auth-token setup in the profile does not affect either the CLI or provider
when an env variable is present with the correct value because env has higher
precedence than a profile definition.
*********

EOF

# ---------------------------------------------------------------------------
# Step 1: Write a provider definition with profile name specified and url, authToken as empty strings
# ---------------------------------------------------------------------------
DASH0_PROVIDER_PROFILE="default" DASH0_API_URL="" DASH0_AUTH_TOKEN="" write_provider_tf "$WORK_DIR"

echo "provider.tf with empty url and authToken definition";

cat "${WORK_DIR}/provider.tf"

# ---------------------------------------------------------------------------
# Step 2: Write a main.tf which tries to create a dashboard
# ---------------------------------------------------------------------------

cat > "${WORK_DIR}/test-dashboard.yaml" <<'YAMLEOF'
apiVersion: perses.dev/v1alpha1
kind: PersesDashboard
metadata:
  name: test-dashboard
  labels: {}
spec:
  duration: 30m
  display:
    description: ""
    name: test-dashboard
  layouts:
    - kind: Grid
      spec:
        items: []
  panels: {}
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'HCLEOF'
resource "dash0_dashboard" "my_dashboard" {
  dataset        = "default"
  dashboard_yaml = file("${path.module}/test-dashboard.yaml")
}

output "dashboard_id" {
    value = dash0_dashboard.my_dashboard.origin
}
HCLEOF

# ---------------------------------------------------------------------------
# Step 3: Try TF Apply
# ---------------------------------------------------------------------------
tf_init "$WORK_DIR"
tf_apply "$WORK_DIR"

DASHBOARD_ID="$(tf_output "$WORK_DIR" dashboard_id)"
info "Created a new Dashboard with id: ${DASHBOARD_ID}"

# ---------------------------------------------------------------------------
# Step 4: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 4: Verifying dashboard exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 dashboards get "$DASHBOARD_ID" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find dashboard ${DASHBOARD_ID}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "test-dashboard" \
  || fail "CLI output does not contain expected dashboard name"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying Test Dashboard via Terraform..."
tf_destroy "$WORK_DIR"
info "Dashboard destroyed and provider configuration validated"
