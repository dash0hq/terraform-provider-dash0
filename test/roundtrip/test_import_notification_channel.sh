#!/usr/bin/env bash
# Roundtrip test for `terraform import` on an organization-scoped resource
# (dash0_notification_channel). Complements test_import_dashboard.sh by
# exercising the second import-ID code path: `<identifier>` alone, no dataset
# prefix, matching what docs/guides/import-existing-assets.md documents for
# notification channels.
#
# Steps:
#   1. Create notification channel via dash0 CLI (out-of-band, no Terraform)
#   2. Discover its identifier via `dash0 -X notification-channels list`
#   3. Export current YAML + write resource shell
#   4. `terraform import` with `<identifier>` (no dataset prefix)
#   5. Assert plan reports no changes
#   6. Verify identifier preservation in state
#   7. Modify + apply — prove the imported resource is manageable
#   8. Destroy + verify server-side deletion

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Roundtrip test: terraform import (dash0_notification_channel) ==="
info "Working directory: ${WORK_DIR}"

# ---------------------------------------------------------------------------
# Step 0: Provider config
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create notification channel via dash0 CLI (out-of-band).
# ---------------------------------------------------------------------------
info "Step 1: Creating notification channel via dash0 CLI..."

cat > "${WORK_DIR}/notification_channel.yaml" <<'YAMLEOF'
kind: Dash0NotificationChannel
metadata:
  name: roundtrip-import-channel
spec:
  type: webhook
  config:
    url: https://example.com/webhook/roundtrip-import
YAMLEOF

dash0 -X notification-channels create -f "${WORK_DIR}/notification_channel.yaml" >/dev/null \
  || fail "Failed to create notification channel via dash0 CLI"
info "Notification channel created via CLI."

# ---------------------------------------------------------------------------
# Step 2: Discover the identifier from the CLI listing.
# ---------------------------------------------------------------------------
info "Step 2: Discovering identifier via dash0 CLI..."

IDENTIFIER="$(dash0 -X notification-channels list -o json \
  | python3 -c "
import json, sys
items = json.load(sys.stdin)
for it in items:
    if it.get('metadata', {}).get('name') == 'roundtrip-import-channel':
        print(it['metadata']['labels']['dash0.com/id'])
        break
")"
[[ -n "$IDENTIFIER" ]] || fail "Could not discover identifier for roundtrip-import-channel"
info "Identifier: ${IDENTIFIER}"

if [[ "$IDENTIFIER" == tf_* ]]; then
  fail "Expected a non-Terraform identifier from a CLI-created channel, got: ${IDENTIFIER}"
fi

# ---------------------------------------------------------------------------
# Step 3: Export current YAML + write resource shell.
# ---------------------------------------------------------------------------
info "Step 3: Exporting YAML via CLI + writing Terraform config..."

dash0 -X notification-channels get "$IDENTIFIER" -o yaml > "${WORK_DIR}/notification_channel.yaml" \
  || fail "Failed to export notification channel YAML"

cat > "${WORK_DIR}/main.tf" <<'EOF'
resource "dash0_notification_channel" "imported" {
  notification_channel_yaml = file("${path.module}/notification_channel.yaml")
}

variable "dataset" {
  type = string
}

output "origin" {
  value = dash0_notification_channel.imported.origin
}
EOF

tf_init "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 4: terraform import with `<identifier>` only — no dataset prefix.
# This is the code path the docs highlight as the notification-channel exception.
# ---------------------------------------------------------------------------
info "Step 4: Importing via terraform import (identifier only, no dataset)..."

TF_VAR_dataset="$DATASET" tf_import "$WORK_DIR" "dash0_notification_channel.imported" "$IDENTIFIER" \
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

python3 - "${WORK_DIR}/notification_channel.yaml" <<'PYEOF'
import sys, yaml
path = sys.argv[1]
with open(path) as f:
    doc = yaml.safe_load(f)
doc.setdefault("spec", {}).setdefault("config", {})["url"] = "https://example.com/webhook/updated-after-import"
with open(path, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR"

CLI_OUTPUT="$(dash0 -X notification-channels get "$IDENTIFIER" -o yaml 2>&1)"
echo "$CLI_OUTPUT" | grep -q "updated-after-import" \
  || fail "CLI output does not reflect the post-import update"
info "Update-after-import verified via CLI."

assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 8: Destroy + verify server-side deletion.
# ---------------------------------------------------------------------------
info "Step 8: Destroying imported channel via Terraform..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR"

info "Step 8b: Verifying server-side deletion..."
if dash0 -X notification-channels get "$IDENTIFIER" -o yaml >/dev/null 2>&1; then
  fail "Notification channel '${IDENTIFIER}' still exists after terraform destroy"
fi
info "Server-side deletion confirmed."

info "=== dash0_notification_channel import roundtrip test PASSED ==="
