#!/usr/bin/env bash
# Notarize a signed macOS binary, in the build pipeline, BEFORE it is archived
# and published.
#
# Why this runs here rather than after `goreleaser release`: the Makefile used
# to notarize the archive after GoReleaser had already uploaded it, which left
# a window where the published tarball was signed but not notarized. Anyone who
# downloaded in that window got "Apple could not verify gophermind is free of
# malware", and macOS caches that verdict locally, so notarizing afterwards did
# not help them. It also meant a GoReleaser failure at any publish step (a
# missing winget/scoop repo, say) skipped notarization entirely.
#
# A bare CLI binary cannot carry a stapled ticket (`stapler staple` fails with
# error 73), so Gatekeeper resolves it online. That makes ordering the only
# thing protecting a downloader, which is why it happens here.
#
# No-ops without MACOS_NOTARY_PROFILE so credential-free snapshot builds still
# succeed, matching codesign.sh.
set -euo pipefail

binary="${1:?usage: notarize-binary.sh <signed-binary>}"

if [[ -z "${MACOS_NOTARY_PROFILE:-}" ]]; then
  echo "notarize: MACOS_NOTARY_PROFILE not set - skipping (snapshot/dev build)"
  exit 0
fi
if [[ -z "${MACOS_SIGN_IDENTITY:-}" ]]; then
  echo "notarize: unsigned build - skipping"
  exit 0
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# notarytool takes an archive, never a bare executable.
ditto -c -k --keepParent "$binary" "$workdir/upload.zip"

echo "notarizing $(basename "$binary") ..."
xcrun notarytool submit "$workdir/upload.zip" \
  --keychain-profile "$MACOS_NOTARY_PROFILE" \
  --wait

echo "notarized: $binary"
