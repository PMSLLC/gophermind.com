# Releasing gophermind 0.10.0

Steps John runs by hand, in order. Each step is a command block plus one line on what it proves.
Steps marked **IRREVERSIBLE** make something public or permanent; read the line above them before pressing Enter.

Why this is not just `make publish VERSION=0.10.0`: `scripts/release.sh` is the reference order (preflight, gate, npm version bump, tag, desktop app, GoReleaser, asset check, npm), but two things rule it out here (`CONTEXT.md`, Gotchas, and the v0.9.0 notes):

1. GoReleaser's own asset upload fails on every asset with a `307` and no `Location` header, after it has already created the GitHub Release object and pushed the Homebrew cask. So GoReleaser is run with `--skip=publish` and the GitHub release is created with `gh release create` from the local `dist/` and `dist-desktop/` files, and the Homebrew casks are copied into `jbrahy/homebrew-tap` and pushed by hand.
2. Nothing in the git wrapper stops the script. The shim at `~/.local/bin/git-wrapper/git` (source `~/OtherProjects/PMSLLC/git-wrapper/internal/scan/command.go`) only gates `clone` and the content-acquiring commands (`fetch`, `pull`, `checkout`, `merge`, `switch`, `submodule`, `restore`, `reset`, `stash`, `worktree`, `am`, `apply`) in a repo with an active hook, and this repo is on its allowlist (`~/.config/git-wrapper/allowlist.json`). `push` and `tag` pass straight through; a `--gw-force` on either is stripped and does nothing, which is why no command below carries it. The push and tag block you have seen from Claude is `~/.claude/hooks/block-dangerous-git.sh`, which does not apply to your shell. So the script's y/N prompts (read from `/dev/tty`) are the only thing between `scripts/release.sh 0.10.0` (or `make publish VERSION=0.10.0`) and a public release: it would commit and push main, tag, push the tag, let GoReleaser create the release object and push the cask, then publish to npm. Never run it without `--dry-run`; the steps below replace it.

Everything else below mirrors the script, with one deliberate omission noted in step 3b. `scripts/release.sh 0.10.0 --dry-run` is still the right rehearsal (step 1c); it tags, pushes and publishes nothing.

Facts that apply to every step:

- Tag `v0.10.0`, npm version `0.10.0`, repo `jbrahy/gophermind.com`, tap `jbrahy/homebrew-tap`.
- Two Developer ID certificates share one name, so always pass the SHA-1, never the name: `MACOS_SIGN_IDENTITY=4680437160A64398AA7A9CC611D19E7765BFA4EE`. Notary profile: `MACOS_NOTARY_PROFILE=gophermind`. (`docs/RELEASING.md`, one-time setup.) They are exported at the top of step 3, not earlier: the GoReleaser `universal_binaries` post hooks (`scripts/codesign.sh`, `scripts/notarize-binary.sh`) read them whenever they are set, `--skip=sign` does not cover those hooks, and a rehearsal with them set would sign and notarize a snapshot binary.
- `npm/package.json` is at `0.9.0` and must say `0.10.0` in the tagged tree: `npm/scripts/download.js` builds its download URL from that version, so a mismatch makes every `npm install gophermind` 404.
- The pre-push hook (`make install-hooks`, `.git/hooks/pre-push` does `exec ./scripts/predeploy.sh`) runs the same gate as step 1b on every push, tag pushes and deletes included. If its iOS stage flakes, retry unchanged; never `--no-verify`.
- The server and the native app (`gophermind-osx`) must ship together this release: the pipeline contract changed (`schema_version` 1), and a server and app that disagree show a message in the pipeline panel. The Wails app in `desktop/` has no pipeline panel; since v0.9.0 it changed five lines of prompt text in `desktop/frontend/src/App.tsx`.
- `scripts/release.sh` dies on a dirty tree (`working tree is dirty`) and on unpushed commits (`local main has commits not pushed to origin`), and a non-snapshot `goreleaser release` (step 3b) refuses a dirty tree too. A `go build ./...` run in `desktop/` leaves an untracked `desktop/desktop` binary (about 30 MB); delete it, never `git clean` or `git stash`.
- Every run of `goreleaser release --clean` (steps 1c and 3b) deletes `dist/`, which currently holds the only local copy of the shipped v0.9.0 set (`gophermind_0.9.0_*`, SBOMs, `checksums.txt`, `dist/homebrew/Casks/gophermind.rb`, dated Sep 29), and `scripts/build-desktop.sh` (step 3a) does `rm -rf` on `dist-desktop/`, which holds the 0.9.0 desktop zip and its `.sha256`. Copies are attached to the v0.9.0 GitHub release, so this is recoverable, but if you want them kept locally move them aside first (`mkdir -p ~/gm-0.9.0-artifacts && mv dist dist-desktop ~/gm-0.9.0-artifacts/`). GoReleaser's `before` hook also runs `go mod tidy` on every run, so `go.mod` and `go.sum` may change.

Set the environment once in the shell you will use for steps 1 to 6:

```sh
cd ~/OtherProjects/PMSLLC/gophermind.com
export GITHUB_TOKEN="$(gh auth token)"
for t in git gh go goreleaser npm node syft wails; do command -v "$t" >/dev/null || echo "missing: $t"; done
git rev-parse --abbrev-ref HEAD                 # main
git status --short                              # must print nothing (delete a stray desktop/desktop if it shows)
```

Proves the tools the script's preflight checks are present, you are on `main`, and the tree is clean. `git status --short` must print nothing from here on; if it ever shows something else, resolve it yourself before step 1 (if `desktop/desktop` keeps coming back from a build, add `/desktop/desktop` to `.gitignore` and include it in the 1a commit).

## 1. Gate

### 1a. Prepare the tree (changelog, npm version, go.mod)

`CHANGELOG.md` on main already carries `## [0.10.0] - 2026-10-21` under an empty `## [Unreleased]` (prepared 2026-10-21 under `docs/GOAL-FINISH.md` task 6). Change the date to the day step 4 actually runs.

```sh
grep -n '^## \[' CHANGELOG.md | head -3                   # [Unreleased], [0.10.0] - <date>, [0.9.0] in that order
sed -n '/^## \[Unreleased\]/,/^## \[0.10.0\]/p' CHANGELOG.md | grep -c '^- '   # 0: the Unreleased section is empty

( cd npm && npm version 0.10.0 --no-git-tag-version --allow-same-version )
node -p 'require("./npm/package.json").version'          # 0.10.0

go mod tidy && git status --short                         # GoReleaser's before hook runs go mod tidy; a dirty tree there fails the build

git add CHANGELOG.md npm/package.json go.mod go.sum       # add .gitignore too if you changed it
git commit -m "chore(release): changelog and npm version for 0.10.0"
git status --short                                        # nothing
```

Proves the tree that will be tagged carries the changelog, the npm version the downloader needs, and a tidy `go.mod`.

### 1b. Run the gate

```sh
./scripts/predeploy.sh
```

Proves gofmt, `go vet ./...`, `go build ./...`, `go test -race ./...` and the iOS XCTest suite all pass on the exact tree you are about to release (expect well over an hour: the executor package under `-race` is slow; `GOPHERMIND_SKIP_IOS=1` is allowed but loud).

### 1c. Push main and rehearse the release

```sh
git push origin main                                      # the pre-push hook runs the gate again (another hour); this pushes anything not yet pushed plus 1a
git rev-list --left-right --count origin/main...HEAD      # 0 0
./scripts/release.sh 0.10.0 --dry-run --skip-gate
```

Proves the script's preflight is green (clean tree, in sync with origin, `gh` authenticated, `goreleaser check` valid), a GoReleaser snapshot builds in `dist/` without signing or notarizing (the two Apple variables are not exported yet), and `npm publish --dry-run` packs the files `package.json` lists. Nothing is tagged, pushed or published by the dry run. Notes:

- `--skip-gate` is deliberate: the gate has just run twice (1b and the hook) on this exact tree, and without the flag the script runs it a third time. The script prints a loud "SKIPPING the test gate" warning; that is expected here.
- The dry run must come after the push: its preflight dies with `local main has commits not pushed to origin; push them first` otherwise.
- This run's snapshot deletes `dist/` (the 0.9.0 artifacts, see Facts) and runs `go mod tidy`. If `git status --short` shows `go.mod` or `go.sum` afterwards, the 1a tidy was incomplete; commit the result and push again before step 2.
- If the dry run prints `git-wrapper: BLOCKED` on its `git fetch --quiet origin --tags`, the repo has dropped off the wrapper allowlist; run `git fetch --gw-force origin --tags` once and re-run the dry run. That is the only place `--gw-force` has an effect.

## 2. Tag

```sh
git status --short                      # nothing
git tag -a v0.10.0 -m "gophermind v0.10.0"
git describe --tags --abbrev=0          # v0.10.0
```

Proves the release commit is named. The tag is local only for now and can still be deleted with `git tag -d v0.10.0`; it is pushed in step 4 and becomes public there.

## 3. GoReleaser build (desktop app first, then the CLI)

Export the Apple credentials now, for steps 3a and 3b only:

```sh
export MACOS_SIGN_IDENTITY=4680437160A64398AA7A9CC611D19E7765BFA4EE
export MACOS_NOTARY_PROFILE=gophermind
```

### 3a. Desktop app: build, sign, notarize, staple

```sh
./scripts/build-desktop.sh 0.10.0
ls -l dist-desktop/
git status --short                      # nothing: the build must not leave desktop/desktop or anything else behind
```

Proves `dist-desktop/` holds `GopherMind-Desktop_0.10.0_darwin_universal.zip`, its `.sha256` (a bare 64-hex hash, no filename) and the rendered cask `gophermind-desktop.rb`, and that Apple accepted the submission (the script runs `notarytool submit --wait`, staples, re-zips the stapled bundle and shows `spctl` saying `source=Notarized Developer ID`). It waits on Apple, typically a few minutes. The script starts with `rm -rf` on `dist-desktop/` (the 0.9.0 desktop zip goes). The app is staged in `dist-desktop/` on purpose: step 3b empties `dist/`.

### 3b. CLI: build, sign, notarize, render the cask, no publish

```sh
git status --short                      # must print nothing: a non-snapshot goreleaser run refuses a dirty tree
goreleaser release --clean --skip=publish,scoop,winget
git status --short                      # nothing (the before hook ran go mod tidy)
```

Proves every archive, Linux package, SBOM and `checksums.txt` builds reproducibly into `dist/`, the universal macOS binary is codesigned and notarized inside the pipeline (`scripts/codesign.sh`, `scripts/notarize-binary.sh`), and the Homebrew cask is rendered to `dist/homebrew/Casks/gophermind.rb`. `--skip=publish` is the workaround for the broken upload; it still renders the cask. `--clean` empties `dist/` first. About five minutes. One thing the script does here is skipped on purpose: after its GoReleaser run `scripts/release.sh` also runs `./scripts/notarize.sh dist/gophermind_*_darwin_all.tar.gz`, a second notarization of the same binary; the in-pipeline hook already notarized the identical binary, so running it would only add a third submission and break the count in 3c.

### 3c. Check the local artifacts before anything goes public

```sh
ls dist/gophermind_0.10.0_* dist/checksums.txt dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip* | wc -l   # 19 (v0.9.0 shipped 19)
( cd dist && shasum -a 256 -c checksums.txt )
xcrun notarytool history --keychain-profile gophermind | head -12       # the two newest submissions (CLI binary, desktop app) say Accepted
mkdir -p /tmp/gm-0.10.0 && tar -xzf dist/gophermind_0.10.0_darwin_all.tar.gz -C /tmp/gm-0.10.0 gophermind
/tmp/gm-0.10.0/gophermind version
codesign -dv --verbose=4 /tmp/gm-0.10.0/gophermind 2>&1 | grep -E 'Authority=Developer ID|flags=.*runtime'
grep -E 'version|sha256' dist/homebrew/Casks/gophermind.rb
shasum -a 256 dist/gophermind_0.10.0_darwin_all.tar.gz
grep -E 'version|sha256' dist-desktop/gophermind-desktop.rb
cat dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip.sha256
shasum -a 256 dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip     # same hash as the line above
ditto -x -k dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip /tmp/gm-0.10.0/app
xcrun stapler validate "/tmp/gm-0.10.0/app/GopherMind Desktop.app"
spctl --assess --type execute --verbose=4 "/tmp/gm-0.10.0/app/GopherMind Desktop.app"
```

Proves the binary reports `0.10.0`, is Developer ID signed with the hardened runtime, both casks' `version` and `sha256` match the artifacts they point at, and the desktop bundle is stapled and accepted offline. Fix anything wrong here, not after step 4.

## 4. GitHub release

**IRREVERSIBLE.** Pushing the tag and creating the release are public. A tag can be deleted (`git push --delete origin v0.10.0`) only while nothing has consumed it; the release page is seen immediately. The tag push runs the pre-push gate again (the hook fires on every push, over an hour); expect the wait, retry unchanged if iOS flakes, never `--no-verify`. The `--delete` rollback push runs it too.

```sh
git push origin v0.10.0

awk '/^## \[0.10.0\]/{f=1;next} /^## \[/{f=0} f' CHANGELOG.md > /tmp/gm-0.10.0/notes.md
wc -l /tmp/gm-0.10.0/notes.md

gh release create v0.10.0 --repo jbrahy/gophermind.com --verify-tag \
  --title "gophermind v0.10.0" --notes-file /tmp/gm-0.10.0/notes.md \
  dist/checksums.txt dist/gophermind_0.10.0_* \
  dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip \
  dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip.sha256
```

Proves the release exists with the changelog as its notes and all 19 files attached. If `gh` fails partway, `gh release view v0.10.0 --json isDraft,assets` tells you what landed; a draft can be deleted and retried safely (Gotchas).

Then verify the uploads byte for byte, because step 6 depends on four of these names:

```sh
gh release view v0.10.0 --repo jbrahy/gophermind.com --json assets --jq '.assets | length'     # 19
for want in gophermind_0.10.0_darwin_all.tar.gz gophermind_0.10.0_linux_amd64.tar.gz gophermind_0.10.0_linux_arm64.tar.gz gophermind_0.10.0_windows_amd64.zip; do
  gh release view v0.10.0 --repo jbrahy/gophermind.com --json assets --jq '.assets[].name' | grep -qx "$want" && echo "ok $want" || echo "MISSING $want"
done
mkdir -p /tmp/gm-0.10.0/dl && gh release download v0.10.0 --repo jbrahy/gophermind.com --dir /tmp/gm-0.10.0/dl
( cd /tmp/gm-0.10.0/dl && shasum -a 256 -c checksums.txt \
  && [ "$(shasum -a 256 GopherMind-Desktop_0.10.0_darwin_universal.zip | awk '{print $1}')" = "$(cat GopherMind-Desktop_0.10.0_darwin_universal.zip.sha256)" ] && echo desktop-zip ok )
```

Proves what GitHub serves is what you built (the same check `scripts/release.sh` runs before it will touch npm), including the four archives `npm/scripts/download.js` downloads. The desktop `.sha256` is compared by hand because `scripts/build-desktop.sh` writes it as a bare hash, which `shasum -c` cannot read.

## 5. Homebrew

**IRREVERSIBLE in effect.** The push is public and `brew` users pick it up immediately; it can only be corrected by another commit to the tap.

```sh
rm -rf /tmp/homebrew-tap && gh repo clone jbrahy/homebrew-tap /tmp/homebrew-tap
cp dist/homebrew/Casks/gophermind.rb        /tmp/homebrew-tap/Casks/gophermind.rb
cp dist-desktop/gophermind-desktop.rb       /tmp/homebrew-tap/Casks/gophermind-desktop.rb
git -C /tmp/homebrew-tap diff --stat
git -C /tmp/homebrew-tap diff | grep -E '^[-+] +(version|sha256)'
git -C /tmp/homebrew-tap add Casks/gophermind.rb Casks/gophermind-desktop.rb
git -C /tmp/homebrew-tap commit -m "Brew cask update for gophermind version v0.10.0"
git -C /tmp/homebrew-tap push origin HEAD
```

Proves both casks moved from 0.9.0 (CLI) and the previous desktop version to 0.10.0 with the sha256 values you checked in step 3c, and nothing else in the tap changed. The CLI cask prints a brew deprecation warning about `postflight` (GoReleaser template); known, harmless.

## 6. npm

**IRREVERSIBLE and PERMANENT.** A published version number can never be reused, only deprecated. Do this only after step 4's asset check passed.

```sh
cd npm
node -p 'require("./package.json").version'     # 0.10.0, and it is the committed, tagged value
npm whoami || npm login
npm publish --access public                      # asks for the OTP
cd ..
```

Proves `gophermind@0.10.0` is on the registry. If it says "Cannot publish over previously staged version" after an OTP failure, change nothing, wait a few minutes and run the identical command again (Gotchas). Then prove a fresh install can fetch the binary it needs:

```sh
npm view gophermind@0.10.0 version
cd npm && npm pack && GOPHERMIND_DOWNLOAD_BASE=https://github.com/jbrahy/gophermind.com/releases/download/v0.10.0 node scripts/download.js && ./vendor/gophermind version; cd ..
```

Proves the published package's `postinstall` resolves to the release assets from step 4 and the downloaded binary reports `0.10.0`.

## 7. Server deploy to the Mac mini

Deploy by git, never by copying a file. The mini is the Fedora Asahi box with systemd (`gophermind-server/deploy/README.md`), reached as `ssh home`; it has no GitHub key, so use `ssh -A`. The unit in the repo (`gophermind-server/deploy/gophermind.service`) says `WorkingDirectory=/opt/gophermind`, `ExecStart=/opt/gophermind/gophermind-server` and `EnvironmentFile=/opt/gophermind/.env`, the global notes say services live under `/data/srv/<service>`, and the README itself says its layout is unverified for the mini. The checkout path is not documented anywhere. So: **verify on the mini** before relying on any path below; the unit file on the box is the source of truth.

```sh
ssh -A home
systemctl cat gophermind | grep -E 'ExecStart|WorkingDirectory|EnvironmentFile'   # the real binary and env paths
systemctl is-active gophermind && /bin/ls -l "$(systemctl show -p ExecStart --value gophermind | sed -n 's/.*path=\([^ ;]*\).*/\1/p')"
```

Proves which binary the unit runs and where. Find the checkout the same way (it is the directory whose `git remote -v` names `jbrahy/gophermind.com`; if `ssh home` times out, do the full `scutil --nc stop jbrahy`, wait, `scutil --nc start jbrahy` cycle from `~/Projects/aws-terraform/docs/migration/home-mini-vpn-ssh.md`).

```sh
# still on the mini; fill in the two paths you just verified
CHECKOUT=<checkout path verified above>
BIN=<ExecStart path verified above>
cd "$CHECKOUT" && git fetch origin --tags && git checkout --detach v0.10.0 && git rev-parse --short HEAD
cd "$CHECKOUT/gophermind-server" && go build -ldflags="-s -w" -o "$BIN.new" . && "$BIN.new" --help >/dev/null 2>&1; echo "built"
cp -p "$BIN" "$BIN.0.9.0" && mv "$BIN.new" "$BIN"      # mv, not cp: cp onto a running binary fails with "Text file busy"
sudo systemctl restart gophermind && sleep 3 && systemctl is-active gophermind
curl -s http://127.0.0.1:8090/healthz; echo; curl -s http://127.0.0.1:8090/readyz; echo
envfile="$(systemctl show -p EnvironmentFiles --value gophermind | cut -d' ' -f1 | sed 's/^-//')"
GOPHERMIND_TOKEN="$(sed -n 's/^GOPHERMIND_TOKEN=//p' "$envfile" | tr -d '"' | tr -d "'")"    # strips quotes the env file may carry; a quoted Bearer value gets 401
curl -s -D - -o /dev/null -H "Authorization: Bearer $GOPHERMIND_TOKEN" http://127.0.0.1:8090/pipeline/runs | grep -i 'HTTP/\|X-Gophermind-Pipeline-Schema'
sudo journalctl -u gophermind -n 20 --no-pager | grep -i 'llm_endpoint_configured\|model discovery' ; exit
```

Proves the mini runs the tagged commit (`git rev-parse` on the box), the unit restarted onto the new binary, `/healthz` and `/readyz` answer, and the new pipeline contract is live (`200` and `X-Gophermind-Pipeline-Schema: 1` on `/pipeline/runs`). `journalctl` needs `sudo` unless `jbrahy` has journal access. The previous binary stays beside it as `<bin>.0.9.0` for rollback (`mv` it back and restart). Never echo the token.

## 8. Desktop install

Local to your Mac; reversible by reinstalling the previous version.

The Wails app (`GopherMind Desktop.app`) ships through the cask pushed in step 5. It has no pipeline panel, so it does not depend on step 7:

```sh
brew update
brew upgrade --cask gophermind-desktop || brew install --cask jbrahy/tap/gophermind-desktop
```

Proves the cask resolves to 0.10.0 and installs the stapled bundle. Known since 0.9.0: the cask on this Mac is registered at 0.7.1 and `brew upgrade` can error "App source is not there"; if so, install from the artifact you verified in step 3c instead (`ditto -x -k dist-desktop/GopherMind-Desktop_0.10.0_darwin_universal.zip /Applications`), which `scripts/deploy-desktop-app.sh` also treats as the install location.

The native app (`gophermind-osx`, `/Applications/GopherMind.app`) is the one that speaks the pipeline contract. It is built and installed by its own script; export the identity on that exact invocation (Gotchas):

```sh
cd gophermind-osx && VERSION=0.10.0 MACOS_SIGN_IDENTITY=4680437160A64398AA7A9CC611D19E7765BFA4EE bash build-app.sh install; cd ..
codesign -dv --verbose=4 /Applications/GopherMind.app 2>&1 | grep -E 'Authority=Developer ID|Identifier='
/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' /Applications/GopherMind.app/Contents/Info.plist   # 0.10.0
```

Proves the app you use against the mini carries the same pipeline contract as the server from step 7 (open it, connect the remote backend, and check the pipeline panel shows runs rather than the version-mismatch message).

## 9. Website check (and deploy only if needed)

`site/index.html` has no hard-coded version, so no content change is needed for 0.10.0. The "Plan and build from a brief" section was already deployed from `main` (`.github/workflows/pages.yml` runs on any push that touches `site/**`, and on `workflow_dispatch`).

The check (read-only):

```sh
gh run list --repo jbrahy/gophermind.com --workflow pages.yml --limit 1
curl -s -o /dev/null -w '%{http_code}\n' https://gophermind.com/          # 200
curl -s https://gophermind.com/ | grep -c 'Plan and build from a brief'   # 1
```

The deploy, only if the last run is not a success on current main. This is a public website deploy, held to the same rule as the server deploy and the desktop install: John runs it, nobody in a workflow does.

```sh
gh workflow run pages.yml --repo jbrahy/gophermind.com && sleep 90 && gh run list --repo jbrahy/gophermind.com --workflow pages.yml --limit 1
```

Proves the Pages deploy is green and the live site serves the current `site/` with its custom domain (`site/CNAME` is checked by the workflow).

## 10. Post-release verification

```sh
npm view gophermind@0.10.0 version                                                      # 0.10.0
npx -y gophermind@0.10.0 version                                                        # downloads from the release, prints 0.10.0
gh release view v0.10.0 --repo jbrahy/gophermind.com --json isDraft,tagName,assets --jq '{draft:.isDraft, tag:.tagName, assets:(.assets|length)}'   # draft false, 19 assets
brew update && brew upgrade --cask gophermind && gophermind version                     # 0.10.0 (use brew install --cask jbrahy/tap/gophermind only if it is not installed)
brew info --cask gophermind | head -1                                                   # gophermind: 0.10.0
brew info --cask gophermind-desktop | head -1                                           # gophermind-desktop: 0.10.0
codesign -dv --verbose=4 "$(command -v gophermind)" 2>&1 | grep Authority=Developer      # Developer ID, hardened runtime
spctl -a -vvv -t install "$(command -v gophermind)"                                     # accepted
curl -s -o /dev/null -w '%{http_code}\n' https://gophermind.com/                        # 200
ssh home 'curl -s http://127.0.0.1:8090/healthz; echo; systemctl is-active gophermind'   # ok, active
```

Proves all four channels (GitHub, Homebrew, npm, website) and the server serve 0.10.0, which is the state `CONTEXT.md` recorded for 0.8.0 and what 0.9.0 lacked (npm was never published). The script's own final banner says `brew untap jbrahy/tap; brew install jbrahy/tap/gophermind`; that line assumes a fresh machine. On this Mac the cask is already installed at 0.9.0, `brew install` of an installed cask does not upgrade it, and `brew untap` refuses while its casks are installed, so that line would print 0.9.0 and make a good release look broken.

## If something goes wrong

- Steps 1a, 1b, 2, 3 and 3c: nothing is public. Fix, rebuild, repeat; `git tag -d v0.10.0` removes a local tag. Step 1c pushed main (the changelog entry and `npm/package.json` at 0.10.0), which is fine to leave; main advertises an npm version that does not exist until step 6, and a tag is local until step 4.
- Step 4, `gh release create` failed partway: `gh release view v0.10.0 --json isDraft,assets`; a draft is invisible to the public and can be deleted and retried (`gh release delete v0.10.0`). A published release with wrong assets: `gh release upload --clobber` the corrected files; do not re-tag. `git push --delete origin v0.10.0` runs the pre-push gate like any push.
- Step 5: push a corrected cask; users who already upgraded will upgrade again.
- Step 6: cannot be undone. `npm deprecate gophermind@0.10.0 "<reason>"` and ship 0.10.1.
- Step 7: `mv "$BIN.0.9.0" "$BIN" && sudo systemctl restart gophermind` on the mini, then `git checkout --detach v0.9.0` in the checkout so the box states what it runs.
- Afterwards: `rm -rf -- "${tmp:?}"` only after `tmp=/tmp/gm-0.10.0; echo "$tmp"`; remove `/tmp/homebrew-tap` the same way.