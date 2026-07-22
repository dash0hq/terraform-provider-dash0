#!/usr/bin/env bash
# Roundtrip test for `terraform import` on an organization-scoped resource
# (dash0_team). Mirrors test_import_notification_channel.sh — teams are
# org-scoped, so the import ID is `<identifier>` alone (no dataset prefix) and
# lives at `.id` (top-level) in `dash0 teams list -o json` output.
#
# Steps:
#   1. Create team via dash0 CLI (out-of-band, no Terraform)
#   2. Discover its identifier via `dash0 -X teams list`
#   3. Export current YAML (unwrapping the CLI's `.team` envelope) + write
#      resource shell
#   4. `terraform import` with `<identifier>` (no dataset prefix)
#   5. Assert plan reports no changes
#   6. Verify identifier preservation in state
#   7. Modify + apply — prove the imported resource is manageable
#   8. Destroy + verify server-side deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_team) ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create team via dash0 CLI (out-of-band).
# ---------------------------------------------------------------------------
info "Step 1: Creating team via dash0 CLI..."

# The team name has a unique suffix so parallel runs and prior aborts don't
# collide on the team display name during identifier discovery.
TEAM_DISPLAY_NAME="roundtrip-import-team-$$-$RANDOM"

dash0 -X teams create "$TEAM_DISPLAY_NAME" \
  --color-from "#6366F1" --color-to "#8B5CF6" >/dev/null \
  || fail "Failed to create team via dash0 CLI"
info "Team created via CLI: ${TEAM_DISPLAY_NAME}"

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the CLI listing.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 -X teams list -o json \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
name = '$TEAM_DISPLAY_NAME'
for it in items:
    if it.get('name') == name:
        print(it['id'])
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for team ${TEAM_DISPLAY_NAME}"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created team, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export current YAML (unwrap `.team`) + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

# `dash0 teams get -o yaml` returns a wrapper document; the CRD lives under
# `.team`. Unwrap it so the file matches what the provider's team_yaml
# attribute expects.
dash0 -X teams get "$IDENTIFIER" -o yaml \
  | python3 -c "
import sys, yaml
doc = yaml.safe_load(sys.stdin.read())
team = doc.get('team', doc) if isinstance(doc, dict) else doc
sys.stdout.write(yaml.safe_dump(team, sort_keys=False))
" > "${WORK_DIR}/team.yaml" \
  || fail "Failed to export team YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_team" "imported" {
  team_yaml = file("${path.module}/team.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_team.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import with `<identifier>` only — no dataset prefix.
# Teams are organization-scoped, matching the notification-channel import path.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import (identifier only, no dataset)..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_team.imported" "$IDENTIFIER" \
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
STATE_ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
if [[ "$STATE_ORIGIN" != "$IDENTIFIER" ]]; then
  fail "Expected imported origin '${IDENTIFIER}' in state, got '${STATE_ORIGIN}'"
fi
if [[ "$STATE_ORIGIN" == tf_* ]]; then
  fail "Imported origin '${STATE_ORIGIN}' unexpectedly carries the tf_ prefix (would indicate re-anchoring)"
fi
info "Identifier preservation check PASSED."

# ---------------------------------------------------------------------------
# Step 7: Modify + apply.
# ---------------------------------------------------------------------------
info "Step 7: Modifying + applying to prove imported resource is manageable..."

python3 - "${WORK_DIR}/team.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc.setdefault("spec", {}).setdefault("display", {})["description"] = "updated-after-import"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 -X teams get "$IDENTIFIER" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "updated-after-import" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported team via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion..."
if dash0 -X teams get "$IDENTIFIER" -o yaml >/dev/null 2>&1; then
  fail "Team '${IDENTIFIER}' still exists after terraform destroy"
fi
info "Server-side deletion confirmed."

info "=== dash0_team import roundtrip test PASSED ==="
