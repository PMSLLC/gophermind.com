#!/usr/bin/env bash
# Notarize the gophermind binary contained in a release archive.
#
# Usage: scripts/notarize.sh <notary-keychain-profile> <archive.tar.gz>
#
# The archive holds a Developer ID-signed universal binary. Apple's notarytool
# accepts a zip/dmg/pkg (not a tar.gz), so we extract the binary, zip it with
# ditto, and submit. Notarization registers the binary's code hash with Apple;
# the identical binary shipped in the tar.gz then passes Gatekeeper's online
# check. (A bare CLI binary can't be stapled, hence the online check.)
set -euo pipefail

profile="${1:?usage: notarize.sh <notary-keychain-profile> <archive.tar.gz>}"
archive="${2:?usage: notarize.sh <notary-keychain-profile> <archive.tar.gz>}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

tar -xzf "$archive" -C "$workdir" gophermind
ditto -c -k --keepParent "$workdir/gophermind" "$workdir/gophermind.zip"

echo "notarizing $(basename "$archive") ..."
xcrun notarytool submit "$workdir/gophermind.zip" \
  --keychain-profile "$profile" \
  --wait

echo "notarized: $archive"
