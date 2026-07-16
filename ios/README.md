# GopherMind iOS

Native SwiftUI client scaffold. Project files are generated with
[XcodeGen](https://github.com/yonaskolb/XcodeGen) from `project.yml` — the
`.xcodeproj` itself is not committed.

## Setup

```sh
brew install xcodegen   # if not already installed
cd ios
xcodegen generate
open GopherMind.xcodeproj
```

Regenerate whenever `project.yml` or the file layout under `GopherMind/` /
`GopherMindTests/` changes.

## Build & test from the CLI

```sh
xcodebuild -project GopherMind.xcodeproj -scheme GopherMind \
  -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' build

xcodebuild -project GopherMind.xcodeproj -scheme GopherMind \
  -destination 'platform=iOS Simulator,name=iPhone 16 Pro' test
```

## Status

This is the A1 scaffold: app shell, a Settings screen backed by Keychain
(bearer token, HMAC secret) and UserDefaults (server URL, approval timeout).
No networking/streaming yet — that lands in A2.

Bundle id `com.jbrahy.gophermind` must match the APNs
`GOPHERMIND_APNS_BUNDLE_ID` and the provisioning profile used to sign the app
(see `docs/mobile-serve.md`).
