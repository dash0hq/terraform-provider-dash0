#!/usr/bin/env bash
# Targeted test: does the provider load values from dash0 CLI config directory
# automatically?
#
# Creates a dash0 CLI config directory and populates it with dummy containing
# activeProfile & profiles.json and tries to initialise a dash0 TF provider
# with no attributes specified.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Test: does the provider load values from dash0 CLI config directory automatically? ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Create DASH0 Config Directory and unset env variables
# ---------------------------------------------------------------------------

dash0 config profiles create default \
    --api-url "$DASH0_API_URL" \
    --auth-token "$DASH0_AUTH_TOKEN"
dash0 config profiles list
dash0 config profiles select default

CONFIG_PRE=$(dash0 config show)
echo ""
echo "========= Default config used by dash0 CLI ========="
echo "$CONFIG_PRE"
echo "===================================================="

# unset the env variables so that dash0 CLI as well as terraform provider
# uses values from profiles
DASH0_API_URL=""
DASH0_AUTH_TOKEN=""

CONFIG_POST=$(dash0 config show)
echo ""
echo "========= Config after removing API Url and Token from Env ========="
echo "$CONFIG_POST"
echo "===================================================================="

# ---------------------------------------------------------------------------
# Step 1: Write an empty provider definition configuration
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

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
