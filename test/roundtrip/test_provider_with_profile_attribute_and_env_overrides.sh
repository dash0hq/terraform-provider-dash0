#!/usr/bin/env bash
# Targeted test: env vars override CLI profile credentials.
#
# A CLI profile is created with a deliberately invalid auth token, then a
# provider block is generated that references that profile by name (and has
# no url/auth_token attributes). The env vars are kept with valid credentials.
# If env precedence works, the test passes; if profile credentials were used,
# the apply would fail with an auth error.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Test: env vars override credentials defined in a CLI profile ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Create a CLI profile with the correct URL but an invalid auth token.
# Env vars (containing valid credentials) remain set.
# ---------------------------------------------------------------------------
dash0 config profiles create default \
    --api-url "$DASH0_API_URL" \
    --auth-token "auth_invalid_token_should_not_be_used"
dash0 config profiles select default

info "Profile 'default' configured with an invalid auth token."
info "Env vars DASH0_API_URL / DASH0_AUTH_TOKEN remain set with valid credentials."

# ---------------------------------------------------------------------------
# Step 1: Provider block with `profile = "default"` and no url/auth_token.
# Env vars supply the credentials at apply time.
# ---------------------------------------------------------------------------
DASH0_API_URL="" DASH0_AUTH_TOKEN="" DASH0_PROVIDER_PROFILE="default" \
    write_provider_tf "$WORK_DIR"

info "Generated provider.tf:"
cat "${WORK_DIR}/provider.tf"

# ---------------------------------------------------------------------------
# Step 2: Minimal dashboard.
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
  dataset        = var.dataset
  dashboard_yaml = file("${path.module}/test-dashboard.yaml")
}

variable "dataset" {
  type = string
}

output "dashboard_id" {
  value = dash0_dashboard.my_dashboard.origin
}
HCLEOF

# ---------------------------------------------------------------------------
# Step 3: Apply with env vars set — they must take precedence over the bad
# profile credentials.
# ---------------------------------------------------------------------------
tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

DASHBOARD_ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" dashboard_id)"
info "Created a new Dashboard with id: ${DASHBOARD_ID}"

# ---------------------------------------------------------------------------
# Step 4: Verify via dash0 CLI.
# ---------------------------------------------------------------------------
info "Step 4: Verifying dashboard exists via dash0 CLI..."
CLI_OUTPUT="$(dash0 dashboards get "$DASHBOARD_ID" --dataset "$DATASET" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find dashboard ${DASHBOARD_ID}"
echo "$CLI_OUTPUT" | grep -qi "test-dashboard" \
  || fail "CLI output does not contain expected dashboard name"

# ---------------------------------------------------------------------------
# Step 5: Destroy.
# ---------------------------------------------------------------------------
info "Step 5: Destroying test dashboard via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Dashboard destroyed and env-over-profile precedence validated"
