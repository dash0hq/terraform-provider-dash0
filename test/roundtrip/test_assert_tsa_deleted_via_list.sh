#!/usr/bin/env bash
# Negative tests for assert_tsa_deleted_via_list (common.sh).
#
# This helper exists because the generic assert_deleted_via_tf could not fail:
# it only planned against the state tf_destroy had just emptied. A deletion
# assertion that cannot fail is worse than none, so the replacement gets its own
# proof that it fails when it should.
#
# The `dash0` CLI is stubbed as a shell function, so this test makes no API
# calls and needs no credentials or organization-admin role — it never skips.
#
# Scenarios, in the order they are asserted:
#   1. origin absent from the list          -> helper succeeds  (the happy path)
#   2. origin still present                 -> helper fails
#   3. CLI exits non-zero                   -> helper fails     (the regression)
#   4. CLI succeeds but emits non-JSON      -> helper fails
#   5. list returns a full page (>= 500)    -> helper fails     (absence unprovable)
#   6. an item has null metadata            -> helper fails     (shape unreadable)
#
# Each failing scenario asserts on the helper's message as well as its exit
# status. Without that, any non-zero exit satisfies the assertion, so a scenario
# could pass without reaching the branch it names.
#
# Scenario 3 is the one worth keeping: before it was fixed, `set -o pipefail`
# meant a failed CLI call produced a non-zero pipeline status, which the helper
# read as proof of deletion. Every credential problem — including exactly the
# 403 this asset kind raises without organization-admin — silently passed.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

# Keep the retry budget short: scenarios 2 and 5 exhaust it by design.
export TSA_DELETED_LIST_ATTEMPTS=2

TARGET_ORIGIN="tf_11111111-1111-1111-1111-111111111111"

# The stub reads STUB_MODE to decide what to emit and which status to exit.
dash0() {
  case "${STUB_MODE}" in
    absent)
      echo '[{"metadata":{"labels":{"dash0.com/origin":"tf_some-other-origin"}}}]'
      ;;
    present)
      printf '[{"metadata":{"labels":{"dash0.com/origin":"%s"}}}]\n' "$TARGET_ORIGIN"
      ;;
    cli_error)
      echo "Error: User must have admin role within organization" >&2
      return 1
      ;;
    garbage)
      echo "not json at all"
      ;;
    full_page)
      python3 -c 'import json; print(json.dumps([{"metadata":{"labels":{"dash0.com/origin":"tf_x%d" % i}}} for i in range(500)]))'
      ;;
    null_metadata)
      # Valid JSON, unexpected shape: metadata is present but null. The chained
      # lookup raises on it. Uncaught, python exits 1, which is the code for
      # "definitively absent", so the destroy step passed without proving
      # anything.
      echo '[{"metadata":null}]'
      ;;
    *)
      # A mistyped or renamed mode is a bug in this test, not a scenario. Exit
      # outright rather than returning a status the assertions below could
      # mistake for the failure they are looking for.
      echo "test bug: unknown STUB_MODE: ${STUB_MODE}" >&2
      exit 99
      ;;
  esac
}
export -f dash0

FAILURES=0

# Runs the helper in a subshell so its `fail` (which exits 1) does not take this
# script down with it. Sets HELPER_RC and HELPER_OUTPUT for the assertions.
run_helper() {
  local mode="$1"
  set +e
  HELPER_OUTPUT="$( ( STUB_MODE="$mode" assert_tsa_deleted_via_list "$TARGET_ORIGIN" "test-dataset" ) 2>&1 )"
  HELPER_RC=$?
  set -e
}

expect_helper_succeeds() {
  local mode="$1" what="$2"
  run_helper "$mode"
  if [[ "$HELPER_RC" -eq 0 ]]; then
    info "PASS: ${what} — helper succeeded as expected."
  else
    warn "FAIL: ${what} — expected the helper to succeed, got exit ${HELPER_RC}. Output: ${HELPER_OUTPUT}"
    FAILURES=$((FAILURES + 1))
  fi
}

# The expected-message argument is what stops this test from fooling itself: a
# bare "exited non-zero" assertion is satisfied by any failure, including the
# stub's own error path, so a scenario could pass without ever reaching the
# branch it names.
expect_helper_fails() {
  local mode="$1" what="$2" expected="$3"
  run_helper "$mode"
  if [[ "$HELPER_RC" -eq 0 ]]; then
    warn "FAIL: ${what} — the helper PASSED when it must have failed. A deletion assertion that cannot fail is the bug this test exists to catch."
    FAILURES=$((FAILURES + 1))
    return
  fi
  if ! grep -qF -- "$expected" <<<"$HELPER_OUTPUT"; then
    warn "FAIL: ${what} — the helper failed (exit ${HELPER_RC}) but not for the stated reason; expected output containing '${expected}'. Output: ${HELPER_OUTPUT}"
    FAILURES=$((FAILURES + 1))
    return
  fi
  info "PASS: ${what} — helper failed as expected (exit ${HELPER_RC})."
}

info "=== Negative tests: assert_tsa_deleted_via_list ==="

expect_helper_succeeds absent    "origin absent from the list"
expect_helper_fails    present       "origin still present in the list" \
  "still returned by list"
expect_helper_fails    cli_error     "CLI exits non-zero (the 403 / expired-token case)" \
  "'dash0 time-series-aggregations list' failed"
expect_helper_fails    garbage       "CLI succeeds but emits non-JSON" \
  "could not parse list output"
expect_helper_fails    full_page     "list returns a full page, so absence is not provable" \
  "list returned a full page"
expect_helper_fails    null_metadata "an item has null metadata, so its shape cannot be read" \
  "unexpected item shape"

if [[ "$FAILURES" -gt 0 ]]; then
  fail "${FAILURES} assertion(s) about assert_tsa_deleted_via_list did not hold."
fi

info "=== assert_tsa_deleted_via_list negative tests PASSED ==="
