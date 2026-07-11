# Releasing gophermind (macOS, signed + notarized, via Homebrew)

gophermind ships as a **signed + notarized universal macOS binary** distributed
through a **Homebrew cask**. Releases are cut **locally on a Mac** with
[GoReleaser](https://goreleaser.com). End users install with:

```sh
brew install jbrahy/tap/gophermind
```

> **Why not the Mac App Store?** gophermind runs shell commands and edits files
> across a repo, which the App Sandbox (mandatory for the MAS) forbids. Signed +
> notarized direct distribution is the correct channel and gives the same "trusted
> install" without the sandbox. See the discussion in the project notes.

---

## One-time setup

1. **Install tooling**
   ```sh
   brew install goreleaser
   xcode-select --install   # for codesign / notarytool, if not already present
   ```

2. **Create the Homebrew tap repo** (must be named `homebrew-tap`):
   - Create an empty **public** GitHub repo: `jbrahy/homebrew-tap`.

3. **Developer ID signing identity** — confirm it's in your login keychain:
   ```sh
   security find-identity -v -p codesigning | grep "Developer ID Application"
   ```
   Export its full name for GoReleaser:
   ```sh
   export MACOS_SIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)"
   ```

4. **Notary credentials** — create an App Store Connect API key
   (App Store Connect → Users and Access → Integrations → App Store Connect API),
   download the `.p8`, and store a reusable notarytool profile once:
   ```sh
   xcrun notarytool store-credentials "gophermind" \
     --key /path/to/AuthKey_XXXX.p8 \
     --key-id   <KEY_ID> \
     --issuer   <ISSUER_UUID>
   export MACOS_NOTARY_PROFILE="gophermind"
   ```

5. **GitHub auth** for publishing the release + pushing the cask:
   ```sh
   gh auth login          # or: export GITHUB_TOKEN=<token with repo scope>
   ```

---

## Validate before your first real release

```sh
make check      # goreleaser check — validates .goreleaser.yaml
make snapshot   # builds + archives + generates the cask locally; no sign/notarize/publish
```

Inspect `dist/` — you should see the universal binary, a `.tar.gz`, checksums,
and a generated `Casks/gophermind.rb`.

---

## Cut a release

```sh
export MACOS_SIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)"
export MACOS_NOTARY_PROFILE="gophermind"

git tag v0.2.0
git push origin v0.2.0

make release     # goreleaser: build → sign → notarize → GitHub Release → push cask
```

GoReleaser will:
1. cross-compile `amd64` + `arm64` and merge into one **universal** binary,
2. **codesign** it (Developer ID, hardened runtime, timestamp),
3. **notarize** the archive via `scripts/notarize.sh` (notarytool, `--wait`),
4. build `.deb`/`.rpm`/`.apk` Linux packages and generate an **SBOM** per archive,
5. create the **GitHub Release** with all archives + packages + `checksums.txt`,
6. commit `Casks/gophermind.rb` to `jbrahy/homebrew-tap`.

> **Note:** the release also has Scoop and winget config, but those steps are
> skipped until their repos exist — `jbrahy/scoop-bucket` and a fork of
> `microsoft/winget-pkgs`. Until then, run `make release` (or `goreleaser
> release --clean`) with `--skip=scoop,winget`. SBOM generation needs `syft`
> (`brew install syft`).

> **Duplicate signing certs:** if `security find-identity -v -p codesigning`
> lists two `Developer ID Application` entries with the **same name**, `codesign`
> errors with `ambiguous (matches …)`. Set `MACOS_SIGN_IDENTITY` to the cert's
> **SHA-1 hash** (the 40-hex prefix in that listing) instead of the name.

---

## Publish to npm (after the GitHub release exists)

The npm package (`npm/`) downloads the platform binary from the GitHub Release on
install, so publish it **after** `make release` has uploaded the assets, and keep
its version identical to the tag.

```sh
cd npm
npm version 0.2.0 --no-git-tag-version --allow-same-version   # match the release tag
npm login                                                     # or set NPM_TOKEN in ~/.npmrc
npm publish --access public
```

Sanity-check the download against the real release before/after publishing:

```sh
cd npm && npm pack && GOPHERMIND_DOWNLOAD_BASE=https://github.com/jbrahy/gophermind.com/releases/download/v0.2.0 \
  node scripts/download.js && ./vendor/gophermind version
```

---

## Verify the published artifact

```sh
brew untap jbrahy/tap 2>/dev/null; brew install jbrahy/tap/gophermind
gophermind version

# On the downloaded binary:
codesign -dv --verbose=4 "$(command -v gophermind)"     # Developer ID + hardened runtime
spctl -a -vvv -t install "$(command -v gophermind)"      # accepted by Gatekeeper
```

---

## Notes

- Version/commit/date are stamped via `-ldflags` into `internal/version` and shown
  by `gophermind version`.
- A bare CLI binary can't be *stapled*; Gatekeeper does a one-time online check
  against Apple's notarization records. The cask also strips the quarantine xattr
  on install as a belt-and-suspenders.
- Nothing here embeds your signing identity or Team ID in the repo — both come
  from environment variables at release time.
