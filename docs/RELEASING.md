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

   **If that command lists the same name twice, use the hash instead.** A
   keychain can hold two valid certificates with an identical subject (a
   renewal imported alongside the original, most often). `codesign` then
   refuses to guess and the release fails part-way through with:

   ```
   Developer ID Application: Your Name (TEAMID): ambiguous (matches ...)
   ```

   The 40-character hex string in the left-hand column of `find-identity` is
   unambiguous, so export that rather than the name:

   ```sh
   export MACOS_SIGN_IDENTITY=4680437160A64398AA7A9CC611D19E7765BFA4EE
   ```

   Deleting the redundant certificate from Keychain Access is the tidier fix,
   but check which one your other workflows reference before removing either.

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

One command does everything — gate, tag, GitHub, Homebrew, npm:

```sh
export MACOS_SIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)"
export MACOS_NOTARY_PROFILE="gophermind"

make publish VERSION=0.6.0 DRY_RUN=1   # rehearse first: nothing is published
make publish VERSION=0.6.0             # the real thing
```

`scripts/release.sh` stops for a `y/N` confirmation before every irreversible
step (pushing the version bump, pushing the tag, running GoReleaser, publishing
to npm), so nothing becomes public without you saying so. Pass `--yes` for CI.

It is also **resumable**: it detects a tag already pushed, a GitHub release
already present, and an npm version already published, and skips those steps.
If npm publishing fails after the GitHub release succeeded, re-run the same
command — it will pick up where it left off rather than redo the release.

The single `VERSION` argument is why the script exists. The npm package builds
its download URL from its own `package.json` version
(`npm/scripts/download.js`), so if that ever disagrees with the git tag, every
`npm install gophermind` 404s. The script derives both from one input and
commits the bump *before* tagging, so the tagged tree always holds the right
version.

<details>
<summary>Running the halves by hand</summary>

```sh
git tag v0.6.0 && git push origin v0.6.0
make release     # goreleaser only: build → sign → notarize → GitHub Release → cask
```

`make release` does **not** publish to npm; see the npm section below.
</details>

## The desktop app

The macOS desktop app (`GopherMind Desktop.app`) ships on the same release, but
GoReleaser cannot build it: GoReleaser produces Go binaries, and a Wails app is
a `.app` bundle with an `Info.plist`, an icon and a compiled frontend inside.
`scripts/build-desktop.sh` builds it, and `make release` runs that script before
invoking GoReleaser, deriving the version from the tag so the two cannot
disagree.

```sh
make desktop-app VERSION=0.7.0    # build it on its own
```

Three things about this differ from the CLI's path, and each caused a real
problem before it was written down:

1. **The artifact is staged in `dist-desktop/`, not `dist/`.** `goreleaser
   release --clean` empties `dist/` as its first action, so anything built
   there beforehand is deleted before it can be attached.

2. **The bundle is stapled; the CLI binary is not.** A bare executable cannot
   carry a stapled notarization ticket, so the CLI depends on Gatekeeper's
   online check. A bundle can, and a stapled app opens on a machine that is
   offline or behind a firewall that blocks Apple's notary service. The script
   staples the `.app` and then **re-zips it**, because the zip that was
   submitted for notarization contains the unstapled bundle. Shipping the
   submitted zip is the easy mistake here and it only shows up on someone
   else's locked-down machine.

3. **`--force` is required when signing.** `wails build` leaves an ad-hoc
   signature on the bundle, and `codesign` will not replace an existing
   signature without it.

Verify a built artifact the way a user's machine will:

```sh
ditto -x -k dist-desktop/GopherMind-Desktop_*_darwin_universal.zip /tmp/verify
xcrun stapler validate "/tmp/verify/GopherMind Desktop.app"   # offline check
spctl --assess --type execute --verbose=4 "/tmp/verify/GopherMind Desktop.app"
```

`spctl` must say `source=Notarized Developer ID`. Anything else means users get
the "cannot be opened" dialog.

Note that `goreleaser release --snapshot` skips publishing entirely and never
evaluates `release.extra_files`, so a green snapshot is **not** evidence the
desktop artifact was produced. Look in `dist-desktop/`.

### Publishing the cask

`packaging/gophermind-desktop.rb.tmpl` is rendered to
`dist-desktop/gophermind-desktop.rb` with the version and checksum filled in.
Copy it into `jbrahy/homebrew-tap` under `Casks/`:

```sh
brew install --cask jbrahy/tap/gophermind-desktop
```

It is a separate cask from `gophermind` (the CLI); both can be installed
independently, since the app embeds its own server rather than shelling out to
the CLI binary. That push is deliberately a human step rather than something
the release script does.

---

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

`make publish` already does this — the steps below are the manual fallback.

The npm package (`npm/`) downloads the platform binary from the GitHub Release on
install, so publish it **after** the release assets exist, and keep its version
identical to the tag. `scripts/release.sh` verifies every asset
`npm/scripts/download.js` expects is actually present before it will publish,
so a package that cannot install never reaches the registry.

```sh
cd npm
npm version 0.6.0 --no-git-tag-version --allow-same-version   # match the release tag
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
