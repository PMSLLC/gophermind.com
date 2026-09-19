#!/usr/bin/env bash
# Build, sign, notarize and staple the GopherMind Desktop macOS app.
#
# Usage: scripts/build-desktop.sh <version> [--skip-build]
# Env:   MACOS_SIGN_IDENTITY   Developer ID Application identity or its SHA-1
#                             hash. Unset means "skip signing" (dev builds).
#        MACOS_NOTARY_PROFILE  notarytool keychain profile. Unset means
#                             "skip notarization".
#
# GoReleaser cannot build this artifact: it produces Go binaries, and a Wails
# app is a .app bundle with an Info.plist, an icon and a frontend baked in. So
# the desktop app is built here and attached to the same GitHub release through
# GoReleaser's release.extra_files.
#
# The ORDER below is the part worth getting right, and it differs from the CLI's:
#
#   1. build      wails, universal (amd64 + arm64)
#   2. sign       Developer ID, hardened runtime, secure timestamp
#   3. zip        ditto --keepParent, because notarytool takes a zip/dmg/pkg
#                 and never a bare .app
#   4. notarize   submit --wait
#   5. STAPLE     onto the .app, then re-zip
#
# Step 5 is why this is not just a copy of notarize-binary.sh. A bare CLI binary
# cannot carry a stapled ticket, so the CLI relies on Gatekeeper's online check.
# A bundle CAN, and a stapled app launches cleanly on a machine that is offline
# or behind a firewall that blocks Apple's notary service. Skipping the staple
# produces an app that works on the machine that built it and shows "cannot be
# opened" on a locked-down one, which is the worst way to find out.
set -euo pipefail

version="${1:?usage: build-desktop.sh <version> [--skip-build]}"
version="${version#v}"
skip_build=0
[[ "${2:-}" == "--skip-build" ]] && skip_build=1

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
desktop_dir="$repo_root/desktop"
app="$desktop_dir/build/bin/GopherMind Desktop.app"
# NOT dist/: `goreleaser release --clean` wipes that directory as its first
# action, which would delete this artifact between building it and attaching
# it. A separate staging directory sidesteps the ordering entirely.
dist="$repo_root/dist-desktop"
zip_name="GopherMind-Desktop_${version}_darwin_universal.zip"
zip_path="$dist/$zip_name"

blue() { printf '\033[1;34m==> %s\033[0m\n' "$1"; }
ok()   { printf '\033[0;32m  ok %s\033[0m\n' "$1"; }
warn() { printf '\033[0;33m  ! %s\033[0m\n' "$1"; }

# 1. Build.
if [[ "$skip_build" == 0 ]]; then
  blue "building GopherMind Desktop $version (darwin/universal)"
  ( cd "$desktop_dir" && wails build -clean -platform darwin/universal )
else
  warn "--skip-build: reusing $app"
fi
[[ -d "$app" ]] || { echo "error: no app bundle at $app" >&2; exit 1; }

# `wails build -platform darwin/universal` leaves an unsigned, ad-hoc,
# single-arch intermediate binary named after wails.json's "name" field
# ("GopherMind Desktop", with a space) sitting alongside the real lipo-merged
# universal binary ("GopherMindDesktop", from "outputfilename") in
# Contents/MacOS/ -- both from the same build, not stale leftovers. codesign
# only touches what Info.plist's CFBundleExecutable and sealed resources
# reference, so that stray binary stays unsigned through the whole pipeline
# and Apple's notary service, which scans every executable in the bundle
# rather than just the one the app launches, rejects the entire submission
# over it ("The binary is not signed with a valid Developer ID certificate").
# Remove anything in Contents/MacOS other than the real executable before
# signing.
real_exe="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$app/Contents/Info.plist")"
for f in "$app/Contents/MacOS/"*; do
  [[ "$(basename "$f")" == "$real_exe" ]] && continue
  warn "removing stray build artifact: $(basename "$f")"
  rm -f "$f"
done

# Clear previous artifacts. This is a staging directory, not a cache: leaving
# an older version behind means the release glob can pick it up as well as the
# current one, which is how v0.7.1 first went out carrying the 0.7.0 app too.
rm -rf "$dist"
mkdir -p "$dist"

# 1b. Stamp the version into the bundle, BEFORE signing.
#
# Wails renders Info.plist from build/darwin/Info.plist using
# {{.Info.ProductVersion}}, which comes from wails.json. That key was absent,
# so Wails substituted its default of 1.0.0 and every build of this app
# announced itself as 1.0.0 in Finder's Get Info and anywhere else the bundle
# version is read - including the release that shipped as 0.7.0.
#
# The value is written here rather than into wails.json so it tracks the tag by
# construction instead of by someone remembering to bump a file. wails.json
# carries 0.0.0-dev, which is what a plain `wails build` produces and is
# honestly unmistakable for a release.
#
# This must happen before codesign: the signature covers Info.plist, so editing
# it afterwards invalidates the signature and the app fails to launch.
blue "stamping version $version into the bundle"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $version" "$app/Contents/Info.plist"
ok "CFBundleShortVersionString=$version CFBundleVersion=$version"

# `wails build -clean` rewrites the bundle's contents but leaves the .app
# directory node's own mtime alone, so Finder shows the date of some earlier
# build for a freshly built app - which is how the 0.7.0 release came to look
# a day old on the machine that had just installed it. ditto then preserves
# that through the zip, so it misleads every user too, not just the builder.
# mtime is not covered by the code signature, but this runs before codesign
# anyway so nothing is touched after signing.
touch "$app"

# 2. Sign.
#
# Wails self-signs the bundle during `wails build` (an ad-hoc signature), so
# --force is required: without it codesign refuses to replace the existing
# signature and the app ships ad-hoc signed, which Gatekeeper rejects.
#
# No --deep. Apple deprecated it, and it signs nested code with the SAME flags,
# which is wrong for anything that needs its own entitlements. This bundle has
# a single executable, so signing the bundle is sufficient and honest.
if [[ -n "${MACOS_SIGN_IDENTITY:-}" ]]; then
  blue "signing"
  codesign --sign "$MACOS_SIGN_IDENTITY" \
           --timestamp \
           --options runtime \
           --force \
           "$app"
  codesign --verify --strict --verbose=2 "$app" 2>&1 | sed 's/^/    /'
  ok "signed with Developer ID, hardened runtime"
else
  warn "MACOS_SIGN_IDENTITY unset, skipping signing (dev build)"
fi

# 3. Zip for submission.
blue "packaging"
rm -f "$zip_path"
ditto -c -k --keepParent "$app" "$zip_path"
ok "$zip_name ($(du -h "$zip_path" | cut -f1))"

# 4 and 5. Notarize, then staple onto the bundle and re-zip.
if [[ -n "${MACOS_NOTARY_PROFILE:-}" && -n "${MACOS_SIGN_IDENTITY:-}" ]]; then
  blue "notarizing (this waits on Apple, typically a few minutes)"
  xcrun notarytool submit "$zip_path" \
    --keychain-profile "$MACOS_NOTARY_PROFILE" \
    --wait

  blue "stapling the ticket onto the bundle"
  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
  ok "stapled"

  # The zip submitted above holds the UNSTAPLED app. Rebuild it so what ships
  # is the stapled bundle; shipping the submitted zip is an easy mistake that
  # only shows up on a machine that cannot reach Apple.
  rm -f "$zip_path"
  ditto -c -k --keepParent "$app" "$zip_path"
  ok "re-zipped the stapled bundle"

  blue "verifying as Gatekeeper will see it"
  spctl --assess --type execute --verbose=4 "$app" 2>&1 | sed 's/^/    /'
else
  warn "MACOS_NOTARY_PROFILE or MACOS_SIGN_IDENTITY unset, skipping notarization"
fi

# Checksum, for the cask and for anyone verifying a download by hand.
shasum -a 256 "$zip_path" | awk '{print $1}' > "$zip_path.sha256"
sha="$(cat "$zip_path.sha256")"

# Render the cask from its template so version and checksum cannot drift apart.
tmpl="$repo_root/packaging/gophermind-desktop.rb.tmpl"
if [[ -f "$tmpl" ]]; then
  out="$dist/gophermind-desktop.rb"
  sed -e "s/{{VERSION}}/$version/g" -e "s/{{SHA256}}/$sha/g" "$tmpl" > "$out"
  ok "rendered $(basename "$out")"
fi

blue "done"
echo "  artifact: $zip_path"
echo "  sha256:   $sha"
echo
echo "  GoReleaser attaches the zip to the GitHub release (release.extra_files)."
echo "  Copy dist-desktop/gophermind-desktop.rb into jbrahy/homebrew-tap Casks/ to"
echo "  publish the cask; that push is deliberately a separate, human step."
