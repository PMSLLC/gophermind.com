#!/bin/bash
# Build GopherMind.app — a proper macOS .app bundle with dock icon.
#
# Usage:
#   bash build-app.sh          # build only
#   bash build-app.sh install  # build + install to /Applications
set -euo pipefail

cd "$(dirname "$0")"

APP_NAME="GopherMind"
BUNDLE_ID="com.gophermind.desktop"
VERSION="0.7.1"
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

# --- Copy icon ---
if [ ! -f iconfile.icns ]; then
    echo "ERROR: iconfile.icns not found in $(pwd)" >&2
    exit 1
fi
cp iconfile.icns "${RES_DIR}/iconfile.icns"

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

# --- Verify bundle ---
echo ""
echo "Verifying bundle..."
[ -f "${MACOS_DIR}/${APP_NAME}" ] || { echo "ERROR: app binary missing" >&2; exit 1; }
[ -f "${RES_DIR}/gophermind-server" ] || { echo "ERROR: server binary missing" >&2; exit 1; }
[ -f "${RES_DIR}/iconfile.icns" ] || { echo "ERROR: icon missing" >&2; exit 1; }
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
