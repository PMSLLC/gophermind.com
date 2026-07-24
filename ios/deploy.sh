#!/usr/bin/env bash
# Build the GopherMind iOS app and install it on a connected iPhone.
# Signs offline with the cached provisioning profile (team from project.yml),
# so it does NOT need an Xcode account in this shell.
#
#   ios/deploy.sh          # build + install + launch on the connected device
#
# Device handling: the on-device tunnel is brought up on demand and drops
# between operations, so we do NOT require `tunnelState == connected` in a plain
# device listing (that filter reported "no device" even with a phone cabled and
# unlocked). Instead we pick any paired physical iPhone, build a generic device
# build (needs no device present), then let `devicectl` establish its own tunnel
# for the install — the sequence that reliably works over both USB and Wi-Fi.
set -uo pipefail
cd "$(dirname "$0")"

command -v xcodegen >/dev/null || { echo "xcodegen not found — 'brew install xcodegen'"; exit 1; }
xcodegen generate >/dev/null

# Pick a paired physical iPhone, preferring a currently-connected transport
# (wired > localNetwork) but accepting any paired one and letting devicectl
# bring the tunnel up.
JSON=$(mktemp)
xcrun devicectl list devices --json-output "$JSON" >/dev/null 2>&1 || true
DEV=$(python3 - "$JSON" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
rank = {"wired": 0, "localNetwork": 1, None: 2}
best, best_rank = None, 99
for dev in d.get("result", {}).get("devices", []):
    hw = dev.get("hardwareProperties", {})
    cp = dev.get("connectionProperties", {})
    if hw.get("deviceType") != "iPhone":
        continue
    if cp.get("pairingState") != "paired":
        continue
    r = rank.get(cp.get("transportType"), 2)
    if r < best_rank:
        best, best_rank = dev.get("identifier", ""), r
print(best or "")
PY
)
rm -f "$JSON"
[ -n "$DEV" ] || { echo "no paired iPhone found — plug it in, unlock it, and trust this Mac"; exit 1; }
echo "Target device: $DEV"

# Generic device build: signs for a real device without needing one attached,
# so the build never depends on the tunnel being up.
DD=$(mktemp -d)
trap 'rm -rf "$DD"' EXIT
xcodebuild -project GopherMind.xcodeproj -scheme GopherMind \
  -destination 'generic/platform=iOS' -configuration Debug -derivedDataPath "$DD" \
  build 2>&1 | grep -iE 'BUILD SUCCEEDED|BUILD FAILED|error:|No profiles|Signing' | tail -4
APP=$(/bin/ls -d "$DD"/Build/Products/Debug-iphoneos/*.app 2>/dev/null | head -1)
[ -n "$APP" ] || { echo "✗ build failed (see output above)"; exit 1; }

# Warm the tunnel, then install + launch. devicectl manages the tunnel for its
# own operation, so these succeed even though a plain `list` shows it down.
xcrun devicectl device info details --device "$DEV" >/dev/null 2>&1 || true
xcrun devicectl device install app --device "$DEV" "$APP" 2>&1 | grep -iE 'installed|bundleID|error' || {
  echo "✗ install failed — if this is a Wi-Fi timeout, connect the cable and retry" >&2
  exit 1
}
xcrun devicectl device process launch --device "$DEV" com.jbrahy.gophermind 2>&1 | grep -i Launched || true
echo "✓ deployed to your iPhone"
