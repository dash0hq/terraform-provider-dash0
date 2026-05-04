#!/usr/bin/env bash
# Targeted test: does the server add keep_firing_for when only `for` is set?
#
# Creates a check rule with `for: 5m0s` and NO `keep_firing_for`, then checks
# if the plan detects drift (i.e., the server enriched the response).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

info "=== Test: does the server add keep_firing_for? ==="
info "Working directory: ${WORK_DIR}"
info "Dataset: ${DATASET}"


# ---------------------------------------------------------------------------
# Step 0: Write provider configuration
# ---------------------------------------------------------------------------
DASH0_API_URL="" DASH0_AUTH_TOKEN="" write_provider_tf "$WORK_DIR"
