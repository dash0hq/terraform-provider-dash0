#!/usr/bin/env bash
# Roundtrip test for the dash0_team data source.
#
# The data source exists for teams that Terraform does not manage, such as
# teams synced from an identity provider. A real SCIM team cannot be created
# here, so the test creates the team out-of-band with the dash0 CLI, which is
# the same situation from Terraform's point of view.
#
# Steps:
#   1. Create a team via the dash0 CLI under a per-run display name
#   2. Discover its id via `dash0 teams list` (the expected value)
#   3. Look the team up by name and by id via Terraform
#   4. Assert both lookups return the CLI's id and display name
#   5. Re-plan without changes (idempotency)
#   6. Assert a lookup by an unknown name fails the plan
#   7. Delete the team via the CLI and assert the lookup by name now fails

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
# Not a slug on purpose: the CLI sets metadata.name to the slug of the display
# name, so a slug-shaped name would hide a lookup on the wrong field.
TEAM_DISPLAY_NAME="Roundtrip DS Team $$-$RANDOM"
IDENTIFIER=""
gone="no"

discover_identifier() {
  dash0 -X teams list -o json | python3 -c "
import json, sys
for it in json.load(sys.stdin):
    if it.get('name') == sys.argv[1]:
        print(it['id'])
        break
" "$TEAM_DISPLAY_NAME"
}

cleanup() {
  if [[ "$gone" != "yes" ]]; then
    [[ -n "$IDENTIFIER" ]] || IDENTIFIER="$(discover_identifier 2>/dev/null)" || true
    if [[ -n "$IDENTIFIER" ]]; then
      warn "Deleting team ${IDENTIFIER} created by this run..."
      dash0 -X teams delete "$IDENTIFIER" --force \
        || warn "Could not delete team ${IDENTIFIER}. Delete it by hand."
    fi
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

info "=== Roundtrip test: dash0_team data source ==="
info "Working directory: ${WORK_DIR}"

write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create the team out-of-band.
# ---------------------------------------------------------------------------
info "Step 1: Creating team via dash0 CLI..."
dash0 -X teams create "$TEAM_DISPLAY_NAME" \
  --color-from "#6366F1" --color-to "#8B5CF6" >/dev/null \
  || fail "Failed to create team via dash0 CLI"
info "Team created via CLI: ${TEAM_DISPLAY_NAME}"

# ---------------------------------------------------------------------------
# Step 2: Discover the id. The team list reads from a database replica, so a
# new team can take a moment to appear.
# ---------------------------------------------------------------------------
info "Step 2: Discovering team id via dash0 CLI..."
for i in $(seq 1 10); do
  IDENTIFIER="$(discover_identifier)" || true
  [[ -n "$IDENTIFIER" ]] && break
  warn "Team not listed yet (attempt ${i}/10), retrying in 3s..."
  sleep 3
done
[[ -n "$IDENTIFIER" ]] || fail "Team ${TEAM_DISPLAY_NAME} never appeared in dash0 teams list"
info "Team id: ${IDENTIFIER}"

# ---------------------------------------------------------------------------
# Step 3: Look the team up by name and by id.
# ---------------------------------------------------------------------------
info "Step 3: Looking up the team via Terraform..."
cat > "${WORK_DIR}/main.tf" <<EOF
data "dash0_team" "by_name" {
  name = "${TEAM_DISPLAY_NAME}"
}

data "dash0_team" "by_id" {
  id = "${IDENTIFIER}"
}

output "by_name_id" {
  value = data.dash0_team.by_name.id
}

output "by_id_name" {
  value = data.dash0_team.by_id.name
}
EOF

tf_init "$WORK_DIR"
applied="no"
for i in $(seq 1 5); do
  if tf_apply "$WORK_DIR"; then
    applied="yes"
    break
  fi
  warn "Lookup failed (attempt ${i}/5), retrying in 3s..."
  sleep 3
done
[[ "$applied" == "yes" ]] || fail "Terraform could not look up team ${TEAM_DISPLAY_NAME}"

# ---------------------------------------------------------------------------
# Step 4: Compare against the CLI.
# ---------------------------------------------------------------------------
info "Step 4: Comparing lookups with the CLI..."
BY_NAME_ID="$(tf_output "$WORK_DIR" by_name_id)"
BY_ID_NAME="$(tf_output "$WORK_DIR" by_id_name)"
[[ "$BY_NAME_ID" == "$IDENTIFIER" ]] \
  || fail "Lookup by name returned id '${BY_NAME_ID}', expected '${IDENTIFIER}'"
[[ "$BY_ID_NAME" == "$TEAM_DISPLAY_NAME" ]] \
  || fail "Lookup by id returned name '${BY_ID_NAME}', expected '${TEAM_DISPLAY_NAME}'"
info "Both lookups match the CLI."

# ---------------------------------------------------------------------------
# Step 5: Idempotency.
# ---------------------------------------------------------------------------
info "Step 5: Re-planning without changes..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: An unknown name fails the plan.
# ---------------------------------------------------------------------------
info "Step 6: Looking up an unknown team name..."
MISSING_DIR="$(mktemp -d -p "$WORK_DIR")"
write_provider_tf "$MISSING_DIR"
cat > "${MISSING_DIR}/main.tf" <<EOF
data "dash0_team" "missing" {
  name = "${TEAM_DISPLAY_NAME}-missing"
}
EOF
tf_init "$MISSING_DIR"
set +e
PLAN_OUTPUT="$(tf_plan_detailed_exitcode "$MISSING_DIR" 2>&1)"
set -e
echo "$PLAN_OUTPUT" | grep -q "Dash0 Team Not Found" \
  || fail "Lookup of an unknown name did not fail with 'Dash0 Team Not Found': ${PLAN_OUTPUT}"
info "Unknown name fails the plan."

# ---------------------------------------------------------------------------
# Step 7: After the team is deleted, the lookup by name fails.
# ---------------------------------------------------------------------------
info "Step 7: Deleting the team via CLI..."
dash0 -X teams delete "$IDENTIFIER" --force >/dev/null \
  || fail "Failed to delete team ${IDENTIFIER} via dash0 CLI"
gone="yes"

# Keep only the lookup by name. The lookup by id fails with the same summary,
# so it would make this step pass even if the lookup by name still matched.
cat > "${WORK_DIR}/main.tf" <<EOF
data "dash0_team" "by_name" {
  name = "${TEAM_DISPLAY_NAME}"
}
EOF

failed="no"
for i in $(seq 1 10); do
  set +e
  PLAN_OUTPUT="$(tf_plan_detailed_exitcode "$WORK_DIR" 2>&1)"
  set -e
  if echo "$PLAN_OUTPUT" | grep -q "Dash0 Team Not Found"; then
    failed="yes"
    break
  fi
  warn "Deleted team still found (attempt ${i}/10), retrying in 3s..."
  sleep 3
done
[[ "$failed" == "yes" ]] || fail "Lookup of the deleted team did not fail: ${PLAN_OUTPUT}"
info "Lookup of the deleted team fails the plan."

info "=== dash0_team data source roundtrip test PASSED ==="
