#!/usr/bin/env bash
# Targeted test: with an empty provider block (no url/auth_token/profile) and
# no DASH0_* environment variables set, the provider must resolve credentials
# from the active dash0 CLI profile in ~/.dash0.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Test: provider with empty attributes resolves credentials from the active CLI profile ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"

# ---------------------------------------------------------------------------
# Step 0: Create a dash0 CLI active profile from the env vars, then unset the
# env vars so only the CLI profile is left for the provider to consume.
# ---------------------------------------------------------------------------
dash0 config profiles create default \
    --api-url "$DASH0_API_URL" \
    --auth-token "$DASH0_AUTH_TOKEN"
dash0 config profiles select default

# ---------------------------------------------------------------------------
# Step 1: Write an empty provider block. With DASH0_API_URL / DASH0_AUTH_TOKEN
# / DASH0_PROVIDER_PROFILE all empty, write_provider_tf emits a provider block
# with no attributes.
# ---------------------------------------------------------------------------
DASH0_API_URL="" DASH0_AUTH_TOKEN="" DASH0_PROVIDER_PROFILE="" \
    write_provider_tf "$WORK_DIR"

info "Generated provider.tf:"
cat "${WORK_DIR}/provider.tf"

# ---------------------------------------------------------------------------
# Step 2: Use a minimal dashboard as the resource under test.
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
# Step 3: Apply with the env vars cleared so the provider must use the CLI
# profile.
# ---------------------------------------------------------------------------
DASH0_API_URL="" DASH0_AUTH_TOKEN="" tf_init "$WORK_DIR"
DASH0_API_URL="" DASH0_AUTH_TOKEN="" TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

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
# Step 5: Destroy (env vars still cleared so destroy also runs via the CLI
# profile).
# ---------------------------------------------------------------------------
info "Step 5: Destroying test dashboard via Terraform..."
DASH0_API_URL="" DASH0_AUTH_TOKEN="" TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Dashboard destroyed and provider configuration validated"
