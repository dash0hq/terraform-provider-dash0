#!/usr/bin/env bash
# Roundtrip test for concurrent dash0_spam_filter writes to the same dataset.
#
# Regression test for https://github.com/dash0hq/terraform-provider-dash0/issues/165:
# a single `terraform apply`/`tofu apply` that creates or updates more than
# one dash0_spam_filter in the same dataset used to fail with an unretried
# "409 dataset version conflict" for all but one resource, because Terraform's
# default parallelism (up to 10) issues those Create/Update/Delete calls
# concurrently and the provider raced the dataset's optimistic-concurrency
# version. The fix serializes spam filter writes per dataset instead of
# retrying after the fact.
#
# This test creates, updates, and destroys several spam filters in the same
# dataset via a single `for_each` resource, each step in exactly one
# tofu apply/destroy, and asserts that a single run converges all of them —
# the symptom of the bug was needing to re-run once per losing resource.
#
# Steps:
#   1. Create N spam filters in one dataset via a single tofu apply
#   2. Verify all N exist via dash0 CLI
#   3. Update all N filters and re-apply in a single tofu apply
#   4. Re-apply without changes (idempotency)
#   5. Destroy all N filters via a single tofu destroy
#   6. Verify all N are gone

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# Comfortably above Terraform's default parallelism of 10 so a single apply
# is guaranteed to issue genuinely concurrent Create/Update/Delete calls
# against the same dataset, not just a small batch that could complete
# serially by chance.
FILTER_COUNT=12

info "=== Roundtrip test: dash0_spam_filter concurrent writes to one dataset ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"
info "Filter count: ${FILTER_COUNT}"

# ---------------------------------------------------------------------------
# Step 0: Write provider configuration
# ---------------------------------------------------------------------------
write_provider_tf "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 1: Create N spam filters in one dataset via a single apply
# ---------------------------------------------------------------------------
info "Step 1: Creating ${FILTER_COUNT} spam filters in one dataset via a single tofu apply..."

python3 - "$WORK_DIR" "$FILTER_COUNT" <<'PYEOF'
import sys

work_dir, count = sys.argv[1], int(sys.argv[2])

keys = ", ".join(f'"filter-{i:02d}" = "concurrent-roundtrip-{i:02d}"' for i in range(count))

main_tf = f"""
locals {{
  spam_filters = {{ {keys} }}
}}

resource "dash0_spam_filter" "test" {{
  for_each = local.spam_filters

  dataset = var.dataset
  spam_filter_yaml = <<-YAML
    apiVersion: v1alpha1
    kind: Dash0SpamFilter
    metadata:
      name: "Concurrent roundtrip filter ${{each.key}}"
      annotations:
        dash0.com/enabled: "true"
    spec:
      contexts:
        - log
      filter:
        - key: "k8s.namespace.name"
          operator: "is"
          value: "${{each.value}}"
  YAML
}}

variable "dataset" {{
  type = string
}}

output "origins" {{
  value = {{ for k, r in dash0_spam_filter.test : k => r.origin }}
}}
"""

with open(f"{work_dir}/main.tf", "w") as f:
    f.write(main_tf)
PYEOF

tf_init "$WORK_DIR"

# The critical assertion for this bug: before the fix, this single apply
# would fail for all but one of the FILTER_COUNT resources with an unretried
# 409 dataset version conflict, and the user had to re-run apply once per
# losing resource. Now it must converge in exactly one run.
TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR" \
  || fail "A single tofu apply did not converge ${FILTER_COUNT} concurrent spam filter creates in dataset ${DATASET} — likely the unretried 409 dataset version conflict from issue #165."

info "All ${FILTER_COUNT} spam filters created in a single apply."

ORIGINS_JSON="$(TF_VAR_dataset="$DATASET" tf_output_json "$WORK_DIR" origins)"

# ---------------------------------------------------------------------------
# Step 2: Verify all filters exist via dash0 CLI
# ---------------------------------------------------------------------------
info "Step 2: Verifying all ${FILTER_COUNT} spam filters exist via dash0 CLI..."

echo "$ORIGINS_JSON" | python3 -c '
import json, sys
origins = json.load(sys.stdin)
for key, origin in sorted(origins.items()):
    print(f"{key}\t{origin}")
' > "${WORK_DIR}/origins.tsv"

while IFS=$'\t' read -r key origin; do
  CLI_OUTPUT="$(dash0 -X spam-filters get "$origin" --dataset "$DATASET" -o yaml 2>&1)" \
    || fail "dash0 CLI could not find spam filter ${key} (origin ${origin})"
  echo "$CLI_OUTPUT" | grep -q "concurrent-roundtrip-" \
    || fail "CLI output for ${key} (origin ${origin}) does not contain expected spam filter content"
done < "${WORK_DIR}/origins.tsv"

info "All ${FILTER_COUNT} spam filters verified via CLI."

# ---------------------------------------------------------------------------
# Step 3: Update all filters and re-apply in a single tofu apply
# ---------------------------------------------------------------------------
info "Step 3: Updating all ${FILTER_COUNT} spam filters and re-applying in a single tofu apply..."

python3 - "$WORK_DIR" "$FILTER_COUNT" <<'PYEOF'
import sys

work_dir, count = sys.argv[1], int(sys.argv[2])

keys = ", ".join(f'"filter-{i:02d}" = "concurrent-roundtrip-{i:02d}-updated"' for i in range(count))

main_tf = f"""
locals {{
  spam_filters = {{ {keys} }}
}}

resource "dash0_spam_filter" "test" {{
  for_each = local.spam_filters

  dataset = var.dataset
  spam_filter_yaml = <<-YAML
    apiVersion: v1alpha1
    kind: Dash0SpamFilter
    metadata:
      name: "Concurrent roundtrip filter ${{each.key}} (updated)"
      annotations:
        dash0.com/enabled: "true"
    spec:
      contexts:
        - log
      filter:
        - key: "k8s.namespace.name"
          operator: "is"
          value: "${{each.value}}"
  YAML
}}

variable "dataset" {{
  type = string
}}

output "origins" {{
  value = {{ for k, r in dash0_spam_filter.test : k => r.origin }}
}}
"""

with open(f"{work_dir}/main.tf", "w") as f:
    f.write(main_tf)
PYEOF

TF_VAR_dataset="$DATASET" tf_apply "$WORK_DIR" \
  || fail "A single tofu apply did not converge ${FILTER_COUNT} concurrent spam filter updates in dataset ${DATASET}."

info "All ${FILTER_COUNT} spam filters updated in a single apply."

while IFS=$'\t' read -r key origin; do
  CLI_OUTPUT="$(dash0 -X spam-filters get "$origin" --dataset "$DATASET" -o yaml 2>&1)"
  echo "$CLI_OUTPUT" | grep -q "concurrent-roundtrip-.*-updated" \
    || fail "CLI output for ${key} (origin ${origin}) does not reflect the update"
done < "${WORK_DIR}/origins.tsv"

info "Update verified via CLI for all ${FILTER_COUNT} spam filters."

# ---------------------------------------------------------------------------
# Step 4: Idempotency — re-apply without changes
# ---------------------------------------------------------------------------
info "Step 4: Re-applying without changes (idempotency test)..."
assert_idempotent "$WORK_DIR"

# ---------------------------------------------------------------------------
# Step 5: Destroy all filters via a single tofu destroy
# ---------------------------------------------------------------------------
info "Step 5: Destroying all ${FILTER_COUNT} spam filters via a single tofu destroy..."
TF_VAR_dataset="$DATASET" tf_destroy "$WORK_DIR" \
  || fail "A single tofu destroy did not converge ${FILTER_COUNT} concurrent spam filter deletes in dataset ${DATASET}."
info "All ${FILTER_COUNT} spam filters destroyed in a single destroy."

# ---------------------------------------------------------------------------
# Step 6: Verify all filters are gone
# ---------------------------------------------------------------------------
info "Step 6: Verifying all ${FILTER_COUNT} spam filters are gone..."
while IFS=$'\t' read -r key origin; do
  assert_deleted_via_cli "dash0 -X spam-filters get" "$origin" "$DATASET"
done < "${WORK_DIR}/origins.tsv"

info "=== dash0_spam_filter concurrent writes roundtrip test PASSED ==="
