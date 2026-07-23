#!/usr/bin/env bash
# Roundtrip test for dash0_team.
#
# Teams are organization-scoped (no dataset). Membership references
# organization members by email; the server resolves emails to internal IDs
# during reconciliation and echoes internal IDs on the read path. The
# Terraform provider rewrites those back to emails so state matches the
# user-authored YAML.
#
# Steps:
#   1. Discover two organization members via `dash0 members list`
#   2. Create the team via Terraform (members referenced by email)
#   3. Verify it exists via `dash0 teams get`
#   4. Update the HCL (rename display description + swap a member)
#   5. Verify update via CLI
#   6. Re-apply without changes (idempotency)
#   7. Destroy via Terraform
#   8. Verify deletion via Terraform plan (would recreate)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: dash0_team ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Discover two organization members via CLI
# ---------------------------------------------------------------------------
info "Step 1: Discovering organization members via dash0 CLI..."

MEMBERS_JSON="$(dash0 -X members list -o json 2>&1)" \
  || fail "dash0 CLI could not list members"

# Pluck the first two email addresses out of the JSON. Members without an
# email are skipped (the server never returns them, but be defensive).
readarray -t MEMBER_EMAILS < <(python3 - "$MEMBERS_JSON" <<'PYEOF'
import json, sys
members = json.loads(sys.argv[1])
emails = []
for m in members:
    email = m.get("email") or (m.get("spec", {}).get("display", {}).get("email"))
    if email:
        emails.append(email)
        if len(emails) >= 3:
            break
for e in emails:
    print(e)
PYEOF
)

if [[ "${#MEMBER_EMAILS[@]}" -lt 2 ]]; then
  fail "Need at least two organization members with emails for this test; found ${#MEMBER_EMAILS[@]}"
fi

MEMBER_A="${MEMBER_EMAILS[0]}"
MEMBER_B="${MEMBER_EMAILS[1]}"
# MEMBER_C is optional. If only two members are available, MEMBER_C reuses
# MEMBER_A on update — the update still exercises the membership-shift path
# because MEMBER_B is dropped.
if [[ "${#MEMBER_EMAILS[@]}" -ge 3 ]]; then
  MEMBER_C="${MEMBER_EMAILS[2]}"
else
  MEMBER_C="${MEMBER_A}"
fi
info "Using members for create: ${MEMBER_A}, ${MEMBER_B}"
info "Using members for update: ${MEMBER_A}, ${MEMBER_C}"

# ---------------------------------------------------------------------------
# Step 2: Create team via Terraform
# ---------------------------------------------------------------------------
info "Step 2: Creating team via Terraform..."

# Fixture derived from the shared create.yaml — technical name backend-team,
# display "Backend Team", two members by email.
cat > "${WORK_DIR}/team.yaml" <<YAMLEOF
apiVersion: dash0.com/v1alpha1
kind: Dash0Team
metadata:
  name: roundtrip-test-team
spec:
  display:
    name: Roundtrip Test Team
    description: Owns backend services and the data platform.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - ${MEMBER_A}
    - ${MEMBER_B}
YAMLEOF

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_team" "test" {
  team_yaml = file("${path.module}/team.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_team.test.origin
}

output "id" {
  value = dash0_team.test.id
}
EOF

tf_init "$WORK_DIR"
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

ORIGIN="$(TF_VAR_dataset="$DATASET" tf_output "$WORK_DIR" origin)"
info "Created team with origin: ${ORIGIN}"

# ---------------------------------------------------------------------------
# Step 3: Verify via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 3: Verifying team exists via dash0 CLI..."

CLI_OUTPUT="$(dash0 -X teams get "$ORIGIN" -o yaml 2>&1)" \
  || fail "dash0 CLI could not find team ${ORIGIN}"
echo "$CLI_OUTPUT"

echo "$CLI_OUTPUT" | grep -qi "roundtrip-test-team\|Roundtrip Test Team" \
  || fail "CLI output does not contain expected team name"
echo "$CLI_OUTPUT" | grep -qi "$MEMBER_A" \
  || fail "CLI output does not contain expected member ${MEMBER_A}"

info "Step 3b: Checking YAML equivalence (uploaded vs downloaded)..."
# `dash0 teams get -o yaml` returns a wrapper document (checkRules, dashboards,
# datasets, members, syntheticChecks, team, views); the CRD envelope lives
# under `.team`. Extract it so the comparison is apples-to-apples.
#
# Members are resolved to emails on both sides by the shared
# ResolveTeamMembersToEmails helper (used by the provider's read path and the
# CLI's export path), so email-only uploads round-trip identically.
TEAM_ONLY_YAML="$(printf '%s\n' "$CLI_OUTPUT" | python3 -c "
import sys, yaml
doc = yaml.safe_load(sys.stdin.read())
team = doc.get('team', doc) if isinstance(doc, dict) else doc
sys.stdout.write(yaml.safe_dump(team, sort_keys=False))
")"
assert_yaml_equivalent "${WORK_DIR}/team.yaml" "$TEAM_ONLY_YAML" \
  || fail "Uploaded and downloaded team YAMLs are not equivalent"
info "Team equivalence check PASSED."

# ---------------------------------------------------------------------------
# Step 4: Update (rename description, swap one member)
# ---------------------------------------------------------------------------
info "Step 4: Updating team (description + membership swap)..."

cat > "${WORK_DIR}/team.yaml" <<YAMLEOF
apiVersion: dash0.com/v1alpha1
kind: Dash0Team
metadata:
  name: roundtrip-test-team
spec:
  display:
    name: Roundtrip Test Team (Updated)
    description: Owns backend services, the data platform, and the on-call rotation.
    color:
      from: "#6366F1"
      to: "#8B5CF6"
  members:
    - ${MEMBER_A}
    - ${MEMBER_C}
YAMLEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"
info "Team updated."

CLI_OUTPUT="$(dash0 -X teams get "$ORIGIN" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -qi "Updated\|UPDATED" \
  || fail "CLI output does not reflect the display update"
info "Update verified via CLI."

# ---------------------------------------------------------------------------
# Step 5: Idempotency
# ---------------------------------------------------------------------------
info "Step 5: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 6: Destroy
# ---------------------------------------------------------------------------
info "Step 6: Destroying team via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"
info "Team destroyed."

# ---------------------------------------------------------------------------
# Step 7: Verify deletion
# ---------------------------------------------------------------------------
info "Step 7: Verifying team is gone..."
assert_deleted_via_tf "$WORK_DIR"

info "=== dash0_team roundtrip test PASSED ==="
