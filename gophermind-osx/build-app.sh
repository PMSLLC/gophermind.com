#!/bin/bash
# Build GopherMind.app — a proper macOS .app bundle with dock icon.
#
# Usage:
#   bash build-app.sh          # build only
#   bash build-app.sh install  # build + install to /Applications
#
# Env:
#   MACOS_SIGN_IDENTITY  Developer ID Application identity, or its SHA-1.
#                        Unset means an unsigned dev build that runs only on
#                        this Mac. Pass the SHA-1 if the name is ambiguous:
#                        duplicate certs with the same common name are common
#                        after a renewal, and codesign refuses to guess.
#                          security find-identity -v -p codesigning
#   VERSION              Override the bundle version (default: nearest git tag).
set -euo pipefail

cd "$(dirname "$0")"

APP_NAME="GopherMind"
# com.jbrahy.* matches the signing identity every other GopherMind target uses
# (com.jbrahy.gophermind.desktop for the Wails app, com.jbrahy.gophermind for
# iOS). The .osx suffix keeps this distinct from the Wails app, which still
# ships in releases, so the two can be installed side by side until cutover.
BUNDLE_ID="com.jbrahy.gophermind.osx"
# Derived from the nearest git tag so it tracks releases by construction rather
# than by someone remembering to bump a literal here. That literal is how the
# Wails app once shipped announcing 1.0.0, and how the v0.7.1 release went out
# carrying the 0.7.0 bundle (see scripts/build-desktop.sh). Override for a
# release build: VERSION=0.8.0 bash build-app.sh
#
# The leading v is stripped and the bare tag used unchanged, because
# CFBundleShortVersionString must be period-separated integers -- the full
# `git describe` (v0.7.1-119-gabc1234) is not a legal value.
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')}"
VERSION="${VERSION:-0.0.0}"
BUILD_DIR="build"
APP_DIR="${BUILD_DIR}/${APP_NAME}.app"
MACOS_DIR="${APP_DIR}/Contents/MacOS"
RES_DIR="${APP_DIR}/Contents/Resources"

# --- Clean ---
rm -rf "${APP_DIR}"
mkdir -p "${MACOS_DIR}" "${RES_DIR}"

# --- Build server binary (the app spawns it as a subprocess) ---
echo "Building gophermind-server..."
(cd ../gophermind-server && go build -ldflags="-s -w" -o ../gophermind-osx/build/gophermind-server .)

# --- Build app binary ---
echo "Building GopherMind app..."
go build -ldflags="-s -w" -o "${MACOS_DIR}/${APP_NAME}" .

# --- Bundle server binary so the app can find it at runtime ---
cp build/gophermind-server "${RES_DIR}/gophermind-server"
chmod 755 "${RES_DIR}/gophermind-server"

# --- Bundle example briefs so the File > Examples menu item finds them ---
cp -R examples "${RES_DIR}/examples"

# --- Copy icon ---
if [ ! -f iconfile.icns ]; then
    echo "ERROR: iconfile.icns not found in $(pwd)" >&2
    exit 1
fi
cp iconfile.icns "${RES_DIR}/iconfile.icns"

# --- Bundle the menu-bar status item glyph (findMenubarIcon in statusitem.go
# looks for it here, sibling to gophermind-server) ---
if [ ! -f menubar-icon.png ]; then
    echo "ERROR: menubar-icon.png not found in $(pwd)" >&2
    exit 1
fi
cp menubar-icon.png "${RES_DIR}/menubar-icon.png"

# --- Info.plist ---
cat > "${APP_DIR}/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>${APP_NAME}</string>
	<key>CFBundleDisplayName</key>
	<string>${APP_NAME}</string>
	<key>CFBundleIdentifier</key>
	<string>${BUNDLE_ID}</string>
	<key>CFBundleVersion</key>
	<string>${VERSION}</string>
	<key>CFBundleShortVersionString</key>
	<string>${VERSION}</string>
	<key>CFBundleExecutable</key>
	<string>${APP_NAME}</string>
	<key>CFBundleIconFile</key>
	<string>iconfile</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSPrincipalClass</key>
	<string>NSApplication</string>
</dict>
</plist>
EOF

# --- PkgInfo ---
printf 'APPL????' > "${APP_DIR}/Contents/PkgInfo"

# --- Sign ---
#
# Unsigned, this bundle runs on the machine that built it and nowhere else:
# Gatekeeper rejects it on any Mac it is copied to. Signing is opt-in through
# MACOS_SIGN_IDENTITY so a plain dev build stays zero-setup, matching
# scripts/build-desktop.sh.
#
# Inside-out order matters. The app embeds gophermind-server in Resources/, and
# a bundle's signature covers its nested code, so signing the outer bundle
# first and the inner binary second invalidates the outer signature. No --deep:
# Apple deprecated it and it would apply one set of flags to both.
#
# Signing is the last step that touches the bundle -- Info.plist, the icon and
# PkgInfo are all covered by the signature, so anything written after this
# point would break it.
if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
    echo ""
    echo "Signing..."
    codesign --sign "${MACOS_SIGN_IDENTITY}" --timestamp --options runtime --force \
        "${RES_DIR}/gophermind-server"
    codesign --sign "${MACOS_SIGN_IDENTITY}" --timestamp --options runtime --force \
        "${APP_DIR}"
    codesign --verify --strict --verbose=2 "${APP_DIR}" 2>&1 | sed 's/^/    /'
    echo "Signed with Developer ID, hardened runtime."
else
    echo ""
    echo "WARNING: MACOS_SIGN_IDENTITY unset, skipping signing (dev build)." >&2
    echo "         This bundle will not launch on another Mac." >&2
fi

# --- Verify bundle ---
echo ""
echo "Verifying bundle..."
[ -f "${MACOS_DIR}/${APP_NAME}" ] || { echo "ERROR: app binary missing" >&2; exit 1; }
[ -f "${RES_DIR}/gophermind-server" ] || { echo "ERROR: server binary missing" >&2; exit 1; }
[ -d "${RES_DIR}/examples/briefs" ] || { echo "ERROR: example briefs missing" >&2; exit 1; }
[ -f "${RES_DIR}/iconfile.icns" ] || { echo "ERROR: icon missing" >&2; exit 1; }
[ -f "${RES_DIR}/menubar-icon.png" ] || { echo "ERROR: menubar icon missing" >&2; exit 1; }
[ -f "${APP_DIR}/Contents/Info.plist" ] || { echo "ERROR: Info.plist missing" >&2; exit 1; }
plutil -lint "${APP_DIR}/Contents/Info.plist" > /dev/null || { echo "ERROR: Info.plist is malformed" >&2; exit 1; }
echo "Bundle OK."

# --- Install (optional) ---
if [ "${1:-}" = "install" ]; then
    echo ""
    echo "Installing to /Applications..."
    # Kill running instance if any
    pkill -x "${APP_NAME}" 2>/dev/null || true
    sleep 1
    rm -rf "/Applications/${APP_NAME}.app"
    cp -R "${APP_DIR}" "/Applications/${APP_NAME}.app"
    # Clear macOS icon cache so the new icon shows up immediately
    rm -rf ~/Library/Caches/com.apple.iconservices.store 2>/dev/null || true
    rm -rf ~/Library/Caches/com.apple.dock.iconcache 2>/dev/null || true
    killall Dock 2>/dev/null || true
    killall Finder 2>/dev/null || true
    echo "Installed: /Applications/${APP_NAME}.app"
    echo "Run:       open /Applications/${APP_NAME}.app"
else
    echo ""
    echo "Built: ${APP_DIR}"
    echo "Run:   open ${APP_DIR}"
    echo "Install: bash build-app.sh install"
fi
