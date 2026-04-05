#!/usr/bin/env bash
# Run all roundtrip tests inside a Docker container.
#
# Usage: ./test/roundtrip/run_all.sh [test_name.sh ...]
#
# Examples:
#   ./test/roundtrip/run_all.sh                    # run all tests
#   ./test/roundtrip/run_all.sh test_dashboard.sh   # run one test
#
# Prerequisites:
#   - Docker must be running
#   - The dash0 CLI must be configured with an active profile (~/.dash0/)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

IMAGE_NAME="dash0-roundtrip-tests"

# ---------------------------------------------------------------------------
# Build the Docker image (provider + tools + test scripts).
# ---------------------------------------------------------------------------
echo "Building Docker image (provider + test harness)..."
docker build \
  -t "$IMAGE_NAME" \
  -f "${SCRIPT_DIR}/Dockerfile" \
  "$REPO_DIR"
echo ""

# ---------------------------------------------------------------------------
# Resolve credentials: prefer env vars, fall back to dash0 CLI active profile.
# ---------------------------------------------------------------------------
if [[ -n "${DASH0_API_URL:-}" && -n "${DASH0_AUTH_TOKEN:-}" ]]; then
  DASH0_DATASET="${DASH0_DATASET:-default}"
  echo "Using credentials from environment variables."
elif [[ -f ~/.dash0/activeProfile && -f ~/.dash0/profiles.json ]]; then
  ACTIVE_PROFILE="$(cat ~/.dash0/activeProfile)"
  PROFILE_JSON="$(python3 -c "
import json
profiles = json.load(open('$HOME/.dash0/profiles.json'))['profiles']
p = next(p for p in profiles if p['name'] == '$ACTIVE_PROFILE')
print(json.dumps(p['configuration']))
")"
  DASH0_API_URL="$(echo  "$PROFILE_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["apiUrl"])')"
  DASH0_AUTH_TOKEN="$(echo "$PROFILE_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["authToken"])')"
  DASH0_DATASET="$(echo  "$PROFILE_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("dataset","default"))')"
  echo "Using credentials from dash0 CLI profile: ${ACTIVE_PROFILE}"
else
  echo "ERROR: Set DASH0_API_URL and DASH0_AUTH_TOKEN env vars, or configure a dash0 CLI profile." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Decide which tests to run.
# ---------------------------------------------------------------------------
if [[ $# -gt 0 ]]; then
  TESTS=("$@")
else
  TESTS=(
    test_check_rule.sh
    test_dashboard.sh
    test_recording_rule_group.sh
    test_synthetic_check.sh
    test_view.sh
  )
fi

# ---------------------------------------------------------------------------
# Run each test in a fresh container.
# ---------------------------------------------------------------------------
PASSED=0
FAILED=0
FAILED_NAMES=()

for test in "${TESTS[@]}"; do
  echo "========================================================"
  echo "Running: ${test}"
  echo "========================================================"
  set +e
  docker run --rm \
    -e DASH0_API_URL="$DASH0_API_URL" \
    -e DASH0_AUTH_TOKEN="$DASH0_AUTH_TOKEN" \
    -e DASH0_DATASET="$DASH0_DATASET" \
    "$IMAGE_NAME" \
    "/tests/${test}"
  rc=$?
  set -e
  echo ""

  if [[ $rc -eq 0 ]]; then
    PASSED=$((PASSED + 1))
  else
    FAILED=$((FAILED + 1))
    FAILED_NAMES+=("$test")
  fi
done

echo "========================================================"
echo "RESULTS"
echo "========================================================"
echo -e "${GREEN}Passed: ${PASSED}${NC}"
echo -e "${RED}Failed: ${FAILED}${NC}"

if [[ ${#FAILED_NAMES[@]} -gt 0 ]]; then
  echo ""
  echo "Failed tests:"
  for name in "${FAILED_NAMES[@]}"; do
    echo -e "  ${RED}- ${name}${NC}"
  done
  exit 1
fi

echo ""
echo -e "${GREEN}All roundtrip tests passed!${NC}"
