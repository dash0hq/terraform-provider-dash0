#!/usr/bin/env bash
# Roundtrip test for dash0_custom_skill.
#
# Needs a dash0 CLI with the `agent0 skills` commands (dash0hq/dash0-cli#337);
# skips with exit code 77 until the bundled CLI has them.
#
# Steps:
#   1. Create the resource via Terraform
#   2. Verify it exists via dash0 CLI
#   3. Update a file, add spec.permissions, and re-apply via Terraform
#   4. Re-apply without changes (idempotency)
#   5. Destroy the resource via Terraform
#   6. Verify deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

custom_skill_preflight "dash0_custom_skill roundtrip (test_custom_skill.sh)"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# Skill names are unique per organization, so concurrent runs need distinct names.
SKILL_NAME="roundtrip-test-skill-$(python3 -c 'import uuid; print(uuid.uuid4().hex[:8])')"

info "=== Roundtrip test: dash0_custom_skill ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create custom skill
# ---------------------------------------------------------------------------
info "Step 1: Creating custom skill via Terraform..."

cat > "${WORK_DIR}/custom_skill.yaml" <<YAMLEOF
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: ${SKILL_NAME}
        description: Roundtrip test skill
        ---
        Follow references/runbook.md.
    - relativePath: references/runbook.md
      content: |
        1. Check the error rate.
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_custom_skill" "test" {
  custom_skill_yaml = file("${path.module}/custom_skill.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_custom_skill.test.origin
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created custom skill with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 2: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying custom skill exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 -X agent0 skills get "$ORIGIN" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find custom skill ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -q "$SKILL_NAME" \
  || fail "CLI output does not contain expected custom skill name"

info "Step 2b: Checking YAML equivalence (uploaded vs downloaded)..."
assert_yaml_equivalent "${WORK_DIR}/custom_skill.yaml" "$CLI_OUTPUT" \
  || fail "Uploaded and downloaded custom skill YAMLs are not equivalent"
info "Custom skill equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 3: Update
# ---------------------------------------------------------------------------
info "Step 3: Updating custom skill (editing a file and adding permissions)..."

cat > "${WORK_DIR}/custom_skill.yaml" <<YAMLEOF
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: ${SKILL_NAME}
        description: Roundtrip test skill
        ---
        Follow references/runbook.md.
    - relativePath: references/runbook.md
      content: |
        1. Check the error rate.
        2. UPDATED: check recent deployments.
  permissions:
    - role: basic_member
      actions: [custom_skill:read, custom_skill:delete]
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Custom skill updated."

CLI_JSON="$(dash0 -X agent0 skills get "$ORIGIN" -o json 2>&1)"
echo "$CLI_JSON" | grep -q "UPDATED" \
  || fail "CLI output does not reflect the file update"
printf '%s' "$CLI_JSON" | python3 -c "
import json, sys
skill = json.load(sys.stdin)
grants = skill.get('spec', {}).get('permissions') or []
member = [g for g in grants if g.get('role') == 'basic_member']
if not member or sorted(member[0].get('actions', [])) != ['custom_skill:delete', 'custom_skill:read']:
    sys.exit('basic_member grant is not read+delete: %r' % grants)
" || fail "CLI output does not reflect the permissions update"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 4: Idempotency
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying custom skill via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Custom skill destroyed."

# ---------------------------------------------------------------------------
# Step 6: Verify deletion
# ---------------------------------------------------------------------------
info "Step 6: Verifying custom skill is gone..."
assert_deleted_via_tf "$WORK_DIR"
assert_custom_skill_deleted "$ORIGIN"

info "=== dash0_custom_skill roundtrip test PASSED ==="
