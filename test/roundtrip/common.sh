#!/usr/bin/env bash
# Common utilities for roundtrip tests.

set -euo pipefail

# ---------------------------------------------------------------------------
# Credentials are passed as environment variables by run_all.sh / Docker.
# ---------------------------------------------------------------------------
: "${DASH0_API_URL:?Environment variable DASH0_API_URL must be set}"
: "${DASH0_AUTH_TOKEN:?Environment variable DASH0_AUTH_TOKEN must be set}"
: "${DASH0_DATASET:=default}"
export DASH0_API_URL DASH0_AUTH_TOKEN DASH0_DATASET

# Use OpenTofu (tofu) as Terraform CLI.
TF="tofu"

# A fixed dataset name so we don't litter the real default dataset.
DATASET="${DASH0_DATASET}"

# ---------------------------------------------------------------------------
# Colour helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Colour

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $*"; exit 1; }

# ---------------------------------------------------------------------------
# Terraform helpers
# ---------------------------------------------------------------------------

# Write the provider configuration into the given working directory.
# Usage: write_provider_tf <dir>
write_provider_tf() {
  local dir="$1"
  cat > "${dir}/provider.tf" <<EOF
terraform {
  required_providers {
    dash0 = {
      source  = "dash0hq/dash0"
      version = "0.0.1"
    }
  }
}

provider "dash0" {
  url        = "${DASH0_API_URL}"
  auth_token = "${DASH0_AUTH_TOKEN}"
}
EOF
}

# Run tofu init with the local mirror.
tf_init() {
  local dir="$1"
  (
    cd "$dir"
    cat > .terraformrc <<TFRC
provider_installation {
  filesystem_mirror {
    path    = "${HOME}/terraform-provider-mirror"
    include = ["registry.opentofu.org/dash0hq/*"]
  }
  direct {
    exclude = ["registry.opentofu.org/dash0hq/*"]
  }
}
TFRC
    TF_CLI_CONFIG_FILE="${dir}/.terraformrc" $TF init -input=false
  )
}

tf_apply() {
  local dir="$1"
  (cd "$dir" && TF_CLI_CONFIG_FILE="${dir}/.terraformrc" $TF apply -auto-approve -input=false)
}

tf_destroy() {
  local dir="$1"
  (cd "$dir" && TF_CLI_CONFIG_FILE="${dir}/.terraformrc" $TF destroy -auto-approve -input=false)
}

tf_output() {
  local dir="$1" name="$2"
  (cd "$dir" && TF_CLI_CONFIG_FILE="${dir}/.terraformrc" $TF output -raw "$name" 2>/dev/null)
}

tf_plan_detailed_exitcode() {
  local dir="$1"
  (cd "$dir" && TF_CLI_CONFIG_FILE="${dir}/.terraformrc" $TF plan -detailed-exitcode -input=false)
}

# ---------------------------------------------------------------------------
# YAML semantic equivalence check.
# ---------------------------------------------------------------------------

# assert_yaml_equivalent <uploaded_file> <cli_yaml_string>
#
# Parses both YAMLs, strips server-managed fields (origin, dataset, version
# labels, top-level id, etc.), and does a deep structural comparison.
# Fails the test with a diff if they are not equivalent.
assert_yaml_equivalent() {
  local uploaded_file="$1"
  local cli_yaml="$2"
  local downloaded_file
  downloaded_file="$(mktemp)"
  printf '%s\n' "$cli_yaml" > "$downloaded_file"

  python3 - "$uploaded_file" "$downloaded_file" <<'PYEOF'
import sys, copy, json, yaml, difflib

# Server-managed label keys to ignore when comparing metadata.
STRIP_LABEL_KEYS = {
    "dash0.com/origin",
    "dash0.com/dataset",
    "dash0.com/version",
}

# Top-level keys that exist only client-side and are not returned by the server.
SKIP_TOP_KEYS = {"apiVersion"}

def is_empty(val):
    """Return True if val is an empty container ({}, [], None)."""
    return val is None or val == {} or val == []

def is_subset(expected, actual, path=""):
    """Check that every field in `expected` is present and equal in `actual`.

    Server-added fields in `actual` that are not in `expected` are ignored.
    Empty containers in expected that are missing from actual are tolerated.
    Returns a list of (path, expected_value, actual_value) mismatches.
    """
    diffs = []

    if isinstance(expected, dict) and isinstance(actual, dict):
        for key in expected:
            # Skip client-only top-level keys (e.g. apiVersion).
            if not path and key in SKIP_TOP_KEYS:
                continue
            # Skip server-managed label/annotation keys.
            if path.endswith(".metadata.labels") and key in STRIP_LABEL_KEYS:
                continue
            if path.endswith(".metadata.annotations") and key in STRIP_LABEL_KEYS:
                continue
            child_path = f"{path}.{key}" if path else key
            if key not in actual:
                # Tolerate empty containers that the server omits.
                if not is_empty(expected[key]):
                    diffs.append((child_path, expected[key], "<missing>"))
            else:
                diffs.extend(is_subset(expected[key], actual[key], child_path))
    elif isinstance(expected, list) and isinstance(actual, list):
        if len(expected) != len(actual):
            diffs.append((path + "[]", f"len={len(expected)}", f"len={len(actual)}"))
        else:
            for i, (e, a) in enumerate(zip(expected, actual)):
                diffs.extend(is_subset(e, a, f"{path}[{i}]"))
    else:
        # Scalar comparison — compare as strings to handle int/str mismatches
        # (e.g. YAML may parse "2" as int 2).
        if str(expected) != str(actual):
            diffs.append((path, expected, actual))

    return diffs

# ── Load both documents ───────────────────────────────────────────────────
with open(sys.argv[1]) as f:
    uploaded = yaml.safe_load(f)
with open(sys.argv[2]) as f:
    downloaded = yaml.safe_load(f)

if uploaded is None:
    print("ERROR: uploaded YAML is empty/null", file=sys.stderr)
    sys.exit(1)
if downloaded is None:
    print("ERROR: downloaded YAML is empty/null", file=sys.stderr)
    sys.exit(1)

diffs = is_subset(uploaded, downloaded)

if not diffs:
    sys.exit(0)

print("Uploaded fields not matching in downloaded resource:", file=sys.stderr)
for path, exp, act in diffs:
    print(f"  {path}: expected={exp!r}, got={act!r}", file=sys.stderr)
sys.exit(1)
PYEOF
  local rc=$?
  rm -f "$downloaded_file"
  return $rc
}

# ---------------------------------------------------------------------------
# Deletion verification with retry (handles eventual consistency).
# ---------------------------------------------------------------------------

# assert_deleted_via_cli <cli_command> <id> <dataset>
# e.g. assert_deleted_via_cli "dash0 synthetic-checks get" "$ORIGIN" "$DATASET"
#
# Retries up to 10 times with 3s delay. Succeeds if the CLI returns a non-zero
# exit code or output contains "not found".
assert_deleted_via_cli() {
  local cli_get_cmd="$1"
  local id="$2"
  local dataset="$3"
  local max_retries=10
  local delay=3

  for i in $(seq 1 "$max_retries"); do
    set +e
    local output
    output="$($cli_get_cmd "$id" --dataset "$dataset" -o yaml 2>&1)"
    local rc=$?
    set -e

    if [[ "$rc" -ne 0 ]] || echo "$output" | grep -qi "not found"; then
      info "Resource confirmed deleted via CLI (attempt ${i})."
      return 0
    fi

    if [[ "$i" -lt "$max_retries" ]]; then
      warn "Resource still exists via CLI (attempt ${i}/${max_retries}), retrying in ${delay}s..."
      sleep "$delay"
    fi
  done

  fail "Resource ${id} still returned by CLI after ${max_retries} attempts."
}

# assert_deleted_via_tf <work_dir>
#
# After destroy, verifies that tofu plan shows the resource needs to be created
# (exit code 2 = changes needed). This confirms the server-side state is clean
# from the provider's perspective.
assert_deleted_via_tf() {
  local dir="$1"
  set +e
  TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$dir"
  local exit_code=$?
  set -e

  if [[ "$exit_code" -eq 2 ]]; then
    info "Resource confirmed deleted (Terraform plans to re-create it)."
  elif [[ "$exit_code" -eq 0 ]]; then
    fail "Terraform shows no changes after destroy — resource may not have been deleted."
  else
    fail "Terraform plan failed with exit code ${exit_code} after destroy."
  fi
}

# ---------------------------------------------------------------------------
# Idempotency check with retry (handles eventual consistency).
# ---------------------------------------------------------------------------

# assert_idempotent <work_dir> [max_retries] [delay]
#
# Runs `tofu plan -detailed-exitcode` and expects exit code 0 (no changes).
# Retries up to max_retries times (default 5) with delay seconds (default 3)
# between attempts to tolerate eventual consistency of server-managed fields
# like permissions that are stored separately and enriched on retrieval.
assert_idempotent() {
  local dir="$1"
  local max_retries="${2:-5}"
  local delay="${3:-3}"

  for i in $(seq 1 "$max_retries"); do
    set +e
    TF_VAR_dataset="$DATASET" tf_plan_detailed_exitcode "$dir"
    local exit_code=$?
    set -e

    if [[ "$exit_code" -eq 0 ]]; then
      info "Idempotency check PASSED: no changes detected (attempt ${i})."
      return 0
    elif [[ "$exit_code" -eq 2 ]]; then
      if [[ "$i" -lt "$max_retries" ]]; then
        warn "Idempotency check detected changes (attempt ${i}/${max_retries}), retrying in ${delay}s..."
        sleep "$delay"
      else
        fail "Idempotency check FAILED: Terraform detected changes on a no-op re-apply (after ${max_retries} attempts)."
      fi
    else
      fail "Idempotency check ERROR: tofu plan exited with code ${exit_code}."
    fi
  done
}

# ---------------------------------------------------------------------------
# YAML equivalence check with retry (handles eventual consistency).
# ---------------------------------------------------------------------------

# assert_yaml_equivalent_eventually <uploaded_file> <cli_command> <id> <dataset> [max_retries] [delay]
#
# Fetches the resource via CLI and checks YAML equivalence against the uploaded
# file. Retries on failure to tolerate eventual consistency (e.g., permissions
# not yet enriched in the API response).
assert_yaml_equivalent_eventually() {
  local uploaded_file="$1"
  local cli_cmd="$2"
  local id="$3"
  local dataset="$4"
  local max_retries="${5:-5}"
  local delay="${6:-3}"

  for i in $(seq 1 "$max_retries"); do
    local cli_output
    cli_output="$($cli_cmd get "$id" --dataset "$dataset" -o yaml 2>&1)" \
      || fail "CLI could not find resource ${id}"

    set +e
    assert_yaml_equivalent "$uploaded_file" "$cli_output"
    local rc=$?
    set -e

    if [[ "$rc" -eq 0 ]]; then
      info "YAML equivalence check PASSED (attempt ${i})."
      return 0
    fi

    if [[ "$i" -lt "$max_retries" ]]; then
      warn "YAML equivalence check failed (attempt ${i}/${max_retries}), retrying in ${delay}s..."
      sleep "$delay"
    else
      echo "$cli_output"
      fail "Uploaded and downloaded YAMLs are not equivalent (after ${max_retries} attempts)."
    fi
  done
}

# ---------------------------------------------------------------------------
# Cleanup helper — removes a temp directory on EXIT.
# Usage: at the top of each test:  WORK_DIR=$(mktemp -d) ; trap "rm -rf $WORK_DIR" EXIT
# ---------------------------------------------------------------------------
