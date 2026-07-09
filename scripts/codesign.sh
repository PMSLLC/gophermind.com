#!/usr/bin/env bash
# Code-sign a macOS binary with a Developer ID Application identity, IF one is
# configured. Reading the identity from the environment here (rather than
# templating it into the GoReleaser hook) means a credential-free snapshot build
# simply skips signing instead of failing.
#
# Usage: scripts/codesign.sh <binary>
# Env:   MACOS_SIGN_IDENTITY — e.g. "Developer ID Application: Name (TEAMID)".
#        When unset/empty, signing is skipped (dev/snapshot builds).
set -euo pipefail

binary="${1:?usage: codesign.sh <binary>}"

if [[ -z "${MACOS_SIGN_IDENTITY:-}" ]]; then
  echo "codesign: MACOS_SIGN_IDENTITY not set — skipping signing (snapshot/dev build)"
  exit 0
fi

codesign --sign "$MACOS_SIGN_IDENTITY" --timestamp --options runtime --force "$binary"
echo "codesigned: $binary"
