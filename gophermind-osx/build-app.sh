#!/bin/bash
# Build GopherMind.app — a proper macOS .app bundle with dock icon.
set -euo pipefail

cd "$(dirname "$0")"

APP_NAME="GopherMind"
BUNDLE_ID="com.gophermind.desktop"
VERSION="0.7.1"
BUILD_DIR="build"
APP_DIR="${BUILD_DIR}/${APP_NAME}.app"
MACOS_DIR="${APP_DIR}/Contents/MacOS"
RES_DIR="${APP_DIR}/Contents/Resources"

rm -rf "${APP_DIR}"
mkdir -p "${MACOS_DIR}" "${RES_DIR}"

# Build the server binary (the app spawns it as a subprocess)
echo "Building gophermind-server..."
(cd ../gophermind-server && go build -ldflags="-s -w" -o ../gophermind-osx/build/gophermind-server .)

# Build the app binary
echo "Building GopherMind app..."
go build -ldflags="-s -w" -o "${MACOS_DIR}/${APP_NAME}" .

# Copy icon
cp iconfile.icns "${RES_DIR}/iconfile.icns"

# Info.plist
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

# PkgInfo
printf 'APPL????' > "${APP_DIR}/Contents/PkgInfo"

echo ""
echo "Built: ${APP_DIR}"
echo "Run:   open ${APP_DIR}"
