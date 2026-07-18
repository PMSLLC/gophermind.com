#!/usr/bin/env bash
# Build the GopherMind iOS app and install it on the connected iPhone.
# Signs offline with the cached provisioning profile (team from project.yml),
# so it does NOT need an Xcode account in this shell.
#
#   ios/deploy.sh          # build + install + launch on the connected device
set -uo pipefail
cd "$(dirname "$0")"

command -v xcodegen >/dev/null || { echo "xcodegen not found — 'brew install xcodegen'"; exit 1; }
xcodegen generate >/dev/null

# Find a connected physical iPhone and its hardware UDID.
JSON=$(mktemp)
xcrun devicectl list devices --json-output "$JSON" >/dev/null 2>&1 || true
DEV=$(python3 - "$JSON" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
for dev in d.get("result", {}).get("devices", []):
    if dev.get("connectionProperties", {}).get("tunnelState") == "connected":
        hw = dev.get("hardwareProperties", {})
        if "iPhone" in (hw.get("marketingName") or "") or hw.get("deviceType") == "iPhone":
            print(hw.get("udid") or dev.get("identifier", ""))
            break
PY
)
rm -f "$JSON"
[ -n "$DEV" ] || { echo "no connected iPhone found — plug it in, unlock it, and trust this Mac"; exit 1; }
echo "Deploying to device $DEV …"

DD=$(mktemp -d)
trap 'rm -rf "$DD"' EXIT
xcodebuild -project GopherMind.xcodeproj -scheme GopherMind \
  -destination "id=$DEV" -configuration Debug -derivedDataPath "$DD" \
  build 2>&1 | grep -iE 'BUILD SUCCEEDED|BUILD FAILED|error:|No profiles|Signing' | tail -4
APP=$(ls -d "$DD"/Build/Products/Debug-iphoneos/*.app 2>/dev/null | head -1)
[ -n "$APP" ] || { echo "✗ build failed (see output above)"; exit 1; }

xcrun devicectl device install app --device "$DEV" "$APP" 2>&1 | grep -iE 'installed|bundleID'
xcrun devicectl device process launch --device "$DEV" com.jbrahy.gophermind 2>&1 | grep -i Launched
echo "✓ deployed to your iPhone"
