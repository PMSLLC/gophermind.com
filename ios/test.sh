#!/usr/bin/env bash
# Run the GopherMind iOS unit tests on an iPhone simulator.
#
#   ios/test.sh                                          # whole suite
#   ios/test.sh -only-testing:GopherMindTests/PairingConfigTests            # one class
#   ios/test.sh -only-testing:GopherMindTests/PairingConfigTests/testRejectsGarbage  # one test
#
# Extra args are passed straight through to `xcodebuild ... test`.
set -uo pipefail
cd "$(dirname "$0")"

command -v xcodegen >/dev/null || { echo "xcodegen not found — 'brew install xcodegen'"; exit 1; }
xcodegen generate >/dev/null

# Auto-pick the first available iPhone simulator (portable across machines).
SIM=$(xcrun simctl list devices available \
        | grep -oE 'iPhone [0-9][^(]*\([0-9A-F-]{36}\)' \
        | grep -oE '[0-9A-F-]{36}' | head -1)
[ -n "$SIM" ] || { echo "no iPhone simulator available — open Xcode > Settings > Components"; exit 1; }
xcrun simctl boot "$SIM" 2>/dev/null || true

echo "Testing on simulator $SIM …"
# Keep the full log: the summary filter below matches "error:" lines but not the
# headers above them, so on its own it can render a destination failure as if
# xcodebuild had picked some other device. On failure, show the raw tail.
log=$(mktemp -t gophermind-ios-test)
xcodebuild -project GopherMind.xcodeproj -scheme GopherMind \
  -destination "id=$SIM" test "$@" >"$log" 2>&1
status=$?
grep -iE "Test Suite '.*xctest' (passed|failed)|Executed [0-9]+ tests|\*\* TEST (SUCCEEDED|FAILED)|error:|: (error|failing)" "$log"
if [ "$status" -eq 0 ]; then
  rm -f "$log"
  echo "✓ tests passed"
else
  echo "✗ tests failed (exit $status)"
  echo "--- unfiltered xcodebuild output (last 40 lines; full log: $log) ---" >&2
  tail -40 "$log" >&2
fi
exit "$status"
