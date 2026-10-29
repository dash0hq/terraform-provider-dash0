#!/usr/bin/env bash
# Roundtrip test for `terraform import` on dash0_custom_skill, an
# organization-scoped resource imported by `<identifier>` alone, with no
# dataset prefix. A skill created without an origin has none to import by, so
# it is imported by its dash0.com/id and its `origin` attribute stays null.
#
# Needs a dash0 CLI with the `agent0 skills` commands (dash0hq/dash0-cli#337);
# skips with exit code 77 until the bundled CLI has them.
#
# Steps:
#   1. Create custom skill via dash0 CLI (out-of-band, no Terraform)
#   2. Discover its identifier via `dash0 -X agent0 skills list`
#   3. Export current YAML + write resource shell
#   4. `terraform import` with `<identifier>` (no dataset prefix)
#   5. Assert plan reports no changes
#   6. Verify identifier preservation in state
#   7. Modify + apply — prove the imported resource is manageable
#   8. Destroy + verify server-side deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

custom_skill_preflight "dash0_custom_skill import roundtrip (test_import_custom_skill.sh)"

WORK_DIR="$(mktemp -d)"
IDENTIFIER=""
DESTROYED=0

cleanup() {
  if [[ "$DESTROYED" -eq 0 && -n "$IDENTIFIER" ]]; then
    dash0 -X agent0 skills delete "$IDENTIFIER" --force >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

# Skill names are unique per organization, so concurrent runs need distinct names.
SKILL_NAME="roundtrip-import-skill-$(python3 -c 'import uuid; print(uuid.uuid4().hex[:8])')"

info "=== Roundtrip test: terraform import (dash0_custom_skill) ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create custom skill via dash0 CLI (out-of-band).
# ---------------------------------------------------------------------------
info "Step 1: Creating custom skill via dash0 CLI..."

cat > "${WORK_DIR}/custom_skill.yaml" <<YAMLEOF
kind: Dash0Skill
spec:
  files:
    - relativePath: SKILL.md
      content: |
        ---
        name: ${SKILL_NAME}
        description: Roundtrip import test skill
        ---
        Follow the runbook.
YAMLEOF

dash0 -X agent0 skills create -f "${WORK_DIR}/custom_skill.yaml" >/dev/null \
  || fail "Failed to create custom skill via dash0 CLI"
info "Custom skill created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the CLI listing.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 -X agent0 skills list -o json \
  | python3 -c "
import json, sys
for skill in json.load(sys.stdin) or []:
    if skill.get('metadata', {}).get('name') == sys.argv[1]:
        print(skill['metadata']['labels']['dash0.com/id'])
        break
" "$SKILL_NAME")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for ${SKILL_NAME}"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created skill, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 -X agent0 skills get "$IDENTIFIER" -o yaml > "${WORK_DIR}/custom_skill.yaml" \
  || fail "Failed to export custom skill YAML"

# The export carries the per-user dash0.com/enabled annotation, which the
# resource rejects, and other server-managed fields.
python3 - "${WORK_DIR}/custom_skill.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc.get("metadata", {}).pop("annotations", None)
doc.get("spec", {}).pop("source", None)
doc.get("spec", {}).pop("permittedActions", None)
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_custom_skill" "imported" {
  custom_skill_yaml = file("${path.module}/custom_skill.yaml")
}

variable "dataset" {
  type = string
}

output "id" {
  value = dash0_custom_skill.imported.id
}

output "origin" {
  value = dash0_custom_skill.imported.origin == null ? "" : dash0_custom_skill.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import with `<identifier>` only — no dataset prefix.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import (identifier only, no dataset)..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_custom_skill.imported" "$IDENTIFIER" \
  || fail "terraform import failed"
info "Import completed."

# ---------------------------------------------------------------------------
# Step 5: Assert plan reports no changes.
# ---------------------------------------------------------------------------
info "Step 5: Asserting terraform plan reports no changes after import..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Identifier preservation.
# ---------------------------------------------------------------------------
info "Step 6: Verifying identifier preservation in state..."
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR" >/dev/null
STATE_ID="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" id)"
STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
if [[ "$STATE_ID" != "$IDENTIFIER" ]]; then
  fail "Expected imported id '${IDENTIFIER}' in state, got '${STATE_ID}'"
fi
if [[ -n "$STATE_ORIGIN" ]]; then
  fail "Expected no origin for a skill created without one, got '${STATE_ORIGIN}' (would indicate re-anchoring)"
fi
info "Identifier preservation check PASSED."

# ---------------------------------------------------------------------------
# Step 7: Modify + apply.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/custom_skill.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
for skill_file in doc["spec"]["files"]:
    if skill_file["relativePath"] == "SKILL.md":
        skill_file["content"] += "Updated after import.\n"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 -X agent0 skills get "$IDENTIFIER" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "Updated after import" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported skill via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
DESTROYED=1

info "Step 8b: Verifying server-side deletion..."
assert_custom_skill_deleted "$IDENTIFIER"

info "=== dash0_custom_skill import roundtrip test PASSED ==="
