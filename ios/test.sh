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
xcodebuild -project GopherMind.xcodeproj -scheme GopherMind \
  -destination "id=$SIM" test "$@" 2>&1 \
  | grep -iE "Test Suite '.*xctest' (passed|failed)|Executed [0-9]+ tests|\*\* TEST (SUCCEEDED|FAILED)|error:|: (error|failing)"
status=${PIPESTATUS[0]}
[ "$status" -eq 0 ] && echo "✓ tests passed" || echo "✗ tests failed (exit $status)"
exit "$status"
