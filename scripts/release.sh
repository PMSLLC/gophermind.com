#!/usr/bin/env bash
# Cut a public gophermind release: GitHub + Homebrew (via GoReleaser) and npm.
#
# Usage:
#   scripts/release.sh <version> [options]
#
#   <version>       0.6.0 or v0.6.0 (normalized; the tag is always v-prefixed)
#
# Options:
#   --dry-run       Rehearse everything. Builds a GoReleaser snapshot and runs
#                   `npm publish --dry-run`. Nothing is tagged, pushed, or
#                   published.
#   --yes           Skip the confirmation prompts (for CI). Every irreversible
#                   step still prints what it is about to do.
#   --skip-gate     Skip the predeploy test suite. Prints a loud warning.
#   --allow-branch  Permit releasing from a branch other than main.
#
# Env:
#   MACOS_SIGN_IDENTITY    Developer ID Application cert (name or SHA-1 hash)
#   MACOS_NOTARY_PROFILE   notarytool keychain profile name
#   GITHUB_TOKEN           optional; sourced from `gh auth token` when unset
#   GORELEASER_SKIP        comma-separated GoReleaser steps to skip
#                          (default: scoop,winget — those repos do not exist yet)
#
# Why one script rather than the two halves of docs/RELEASING.md: the npm
# package derives its download URL from its own package.json version
# (npm/scripts/download.js), so if that version and the git tag ever disagree,
# every `npm install gophermind` 404s. Deriving both from a single argument is
# the point of this script. npm/package.json had drifted to 0.1.0 while the
# latest tag was v0.5.0, which is exactly that failure waiting to happen.
#
# The script is RESUMABLE. Each publish step detects work that already exists
# (tag pushed, GitHub release present, npm version published) and skips it, so a
# failure partway through is fixed by re-running the same command.
set -euo pipefail

cd "$(dirname "$0")/.."

REPO="jbrahy/gophermind.com"
NPM_PKG="gophermind"

# ── argument parsing ───────────────────────────────────────────────────
VERSION=""
DRY_RUN=0
ASSUME_YES=0
SKIP_GATE=0
ALLOW_BRANCH=0

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)      DRY_RUN=1 ;;
    --yes|-y)       ASSUME_YES=1 ;;
    --skip-gate)    SKIP_GATE=1 ;;
    --allow-branch) ALLOW_BRANCH=1 ;;
    -h|--help)      sed -n '2,35p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*)             echo "unknown option: $1" >&2; exit 2 ;;
    *)
      if [ -n "$VERSION" ]; then echo "unexpected argument: $1" >&2; exit 2; fi
      VERSION="$1" ;;
  esac
  shift
done

if [ -z "$VERSION" ]; then
  echo "usage: scripts/release.sh <version> [--dry-run] [--yes] [--skip-gate]" >&2
  exit 2
fi

# Normalize: TAG always carries the v, NPM_VERSION never does.
TAG="v${VERSION#v}"
NPM_VERSION="${TAG#v}"

if ! printf '%s' "$NPM_VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
  printf 'error: %s is not a semver version (want 0.6.0 or v0.6.0)\n' "$VERSION" >&2
  exit 2
fi

# ── output helpers ─────────────────────────────────────────────────────
if [ -t 1 ]; then
  bold=$(tput bold 2>/dev/null || true); red=$(tput setaf 1 2>/dev/null || true)
  green=$(tput setaf 2 2>/dev/null || true); yellow=$(tput setaf 3 2>/dev/null || true)
  reset=$(tput sgr0 2>/dev/null || true)
else
  bold=""; red=""; green=""; yellow=""; reset=""
fi

step()  { echo; echo "${bold}==> $*${reset}"; }
ok()    { echo "  ${green}✓${reset} $*"; }
warn()  { echo "  ${yellow}!${reset} $*"; }
die()   { echo "${red}error:${reset} $*" >&2; exit 1; }

# confirm prompts before an irreversible, outward-facing action.
#
# Callers must branch on DRY_RUN themselves before reaching here: a dry run has
# to walk the whole flow, and treating "unconfirmed" as "abort" would stop the
# rehearsal at the first prompt.
confirm() {
  local prompt="$1"
  if [ "$DRY_RUN" = 1 ]; then warn "[dry-run] would: $prompt"; return 1; fi
  if [ "$ASSUME_YES" = 1 ]; then ok "auto-confirmed: $prompt"; return 0; fi
  local answer=""
  printf '  %s%s%s [y/N] ' "$bold" "$prompt" "$reset"
  read -r answer </dev/tty || answer=""
  case "$answer" in [yY]|[yY][eE][sS]) return 0 ;; *) return 1 ;; esac
}

# ── 1. preflight ───────────────────────────────────────────────────────
# Everything that can be checked without side effects is checked FIRST, so a
# missing tool or a dirty tree fails in seconds rather than after a build.
step "Preflight for $TAG"

for tool in git gh go goreleaser npm node; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is not installed"
done
ok "required tools present"

command -v syft >/dev/null 2>&1 || warn "syft not found — SBOM generation will fail (brew install syft)"

if [ -n "$(git status --porcelain)" ]; then
  git status --short
  die "working tree is dirty; commit or stash before releasing"
fi
ok "working tree clean"

branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$branch" != "main" ] && [ "$ALLOW_BRANCH" != 1 ]; then
  die "on branch '$branch', not main (pass --allow-branch to override)"
fi
ok "on branch $branch"

git fetch --quiet origin --tags
if [ -n "$(git rev-list "origin/$branch..HEAD" 2>/dev/null || true)" ]; then
  die "local $branch has commits not pushed to origin; push them first"
fi
if [ -n "$(git rev-list "HEAD..origin/$branch" 2>/dev/null || true)" ]; then
  die "origin/$branch is ahead of local; pull first"
fi
ok "in sync with origin/$branch"

if [ "$DRY_RUN" != 1 ]; then
  : "${MACOS_SIGN_IDENTITY:?set MACOS_SIGN_IDENTITY (see docs/RELEASING.md)}"
  : "${MACOS_NOTARY_PROFILE:?set MACOS_NOTARY_PROFILE (see docs/RELEASING.md)}"
  ok "signing environment set"
fi

gh auth status >/dev/null 2>&1 || die "gh is not authenticated (run: gh auth login)"
ok "gh authenticated"

goreleaser check >/dev/null || die "goreleaser config is invalid"
ok "goreleaser config valid"

# npm auth is only needed for the real publish, but checking it now avoids
# discovering it after the GitHub release is already public.
if [ "$DRY_RUN" != 1 ]; then
  npm whoami >/dev/null 2>&1 || die "npm is not authenticated (run: npm login)"
  ok "npm authenticated as $(npm whoami 2>/dev/null)"
fi

# Detect work already done, so a re-run resumes instead of failing.
TAG_EXISTS_REMOTE=0
git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null 2>&1 && TAG_EXISTS_REMOTE=1

RELEASE_EXISTS=0
gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1 && RELEASE_EXISTS=1

NPM_EXISTS=0
if npm view "$NPM_PKG@$NPM_VERSION" version >/dev/null 2>&1; then NPM_EXISTS=1; fi

[ "$TAG_EXISTS_REMOTE" = 1 ] && warn "tag $TAG already on origin — will not re-tag"
[ "$RELEASE_EXISTS"    = 1 ] && warn "GitHub release $TAG already exists — will not re-run GoReleaser"
[ "$NPM_EXISTS"        = 1 ] && warn "$NPM_PKG@$NPM_VERSION already on npm — will not re-publish"

if [ "$TAG_EXISTS_REMOTE" = 1 ] && [ "$RELEASE_EXISTS" = 1 ] && [ "$NPM_EXISTS" = 1 ]; then
  echo; ok "${bold}$TAG is already fully released.${reset}"
  exit 0
fi

# ── 2. test gate ───────────────────────────────────────────────────────
if [ "$SKIP_GATE" = 1 ]; then
  warn "${yellow}SKIPPING the test gate — you are shipping unverified code.${reset}"
else
  step "Test gate"
  ./scripts/predeploy.sh || die "predeploy gate failed; nothing was released"
  ok "gate passed"
fi

# ── 3. sync the npm version to the tag ─────────────────────────────────
# This must land BEFORE the tag is cut, so the tagged tree contains the version
# that download.js will use to build its asset URL.
step "Sync npm/package.json to $NPM_VERSION"

current_npm="$(node -p 'require("./npm/package.json").version')"
if [ "$current_npm" = "$NPM_VERSION" ]; then
  ok "already $NPM_VERSION"
else
  echo "  $current_npm → $NPM_VERSION"
  if [ "$DRY_RUN" = 1 ]; then
    warn "[dry-run] would bump and commit npm/package.json"
  else
    ( cd npm && npm version "$NPM_VERSION" --no-git-tag-version --allow-same-version >/dev/null )
    git add npm/package.json
    [ -f npm/package-lock.json ] && git add npm/package-lock.json
    git commit -q -m "chore(npm): set package version to $NPM_VERSION

The npm postinstall derives its GitHub Release asset URL from this version
(npm/scripts/download.js), so it must match the git tag exactly or every
install 404s."
    if confirm "push the version bump to origin/$branch?"; then
      git push origin "$branch"
      ok "pushed version bump"
    else
      git reset --hard -q HEAD~1
      die "aborted; version bump rolled back"
    fi
  fi
fi

# ── 4. tag ─────────────────────────────────────────────────────────────
step "Tag $TAG"
if [ "$TAG_EXISTS_REMOTE" = 1 ]; then
  ok "already on origin"
elif [ "$DRY_RUN" = 1 ]; then
  warn "[dry-run] would create and push tag $TAG"
elif confirm "create and push tag $TAG? (this is public)"; then
  git tag -a "$TAG" -m "gophermind $TAG"
  git push origin "$TAG"
  ok "pushed $TAG"
else
  die "aborted before tagging"
fi

# ── 5. Desktop app: build, sign, notarize, staple ──────────────────────
# GoReleaser's release step (next) attaches this via release.extra_files
# (see .goreleaser.yaml) — a glob against a fixed filename, not a build step,
# so the file must already exist in dist-desktop/ before goreleaser runs or
# the whole release command fails after already creating the GitHub Release
# object (a real failure this script hit once: a draft release with no
# assets, because this step was missing entirely). Built into dist-desktop/,
# never dist/: `goreleaser release --clean` empties dist/ as its first
# action, which would delete the artifact between building it and attaching
# it. Mirrors `make release`'s ordering exactly.
step "Desktop app"
if [ "$DRY_RUN" = 1 ]; then
  warn "[dry-run] skipping desktop app build (goreleaser --snapshot below does not need it)"
else
  ./scripts/build-desktop.sh "$NPM_VERSION"
  ok "desktop app built, signed, notarized, stapled"
fi

# ── 6. GoReleaser: GitHub Release + Homebrew cask ──────────────────────
step "Build, sign, notarize, publish (GitHub + Homebrew)"
skip_steps="${GORELEASER_SKIP:-scoop,winget}"

if [ "$RELEASE_EXISTS" = 1 ]; then
  ok "release $TAG already published"
elif [ "$DRY_RUN" = 1 ]; then
  warn "[dry-run] building a snapshot instead of releasing"
  goreleaser release --snapshot --clean --skip="sign,$skip_steps"
  ok "snapshot built in dist/"
else
  echo "  skipping GoReleaser steps: $skip_steps"
  if confirm "run GoReleaser? (publishes a GitHub Release and pushes the Homebrew cask)"; then
    GITHUB_TOKEN="${GITHUB_TOKEN:-$(gh auth token 2>/dev/null)}" \
      goreleaser release --clean --skip="$skip_steps"
    ok "GitHub release + Homebrew cask published"

    echo "  notarizing the published macOS archive..."
    ./scripts/notarize.sh dist/gophermind_*_darwin_all.tar.gz
    ok "notarized"
  else
    die "aborted before publishing; tag $TAG is already pushed (delete it with: git push --delete origin $TAG)"
  fi
fi

# ── 7. verify release assets before npm depends on them ────────────────
# npm's postinstall downloads these by name. Publishing to npm before they
# exist would ship a package that cannot install.
step "Verify release assets"
if [ "$DRY_RUN" = 1 ]; then
  warn "[dry-run] skipping asset verification"
else
  assets="$(gh release view "$TAG" --repo "$REPO" --json assets --jq '.assets[].name')"
  missing=0
  for want in \
    "gophermind_${NPM_VERSION}_darwin_all.tar.gz" \
    "gophermind_${NPM_VERSION}_linux_amd64.tar.gz" \
    "gophermind_${NPM_VERSION}_linux_arm64.tar.gz" \
    "gophermind_${NPM_VERSION}_windows_amd64.zip"
  do
    if printf '%s\n' "$assets" | grep -qx "$want"; then
      ok "$want"
    else
      warn "MISSING: $want"
      missing=1
    fi
  done
  [ "$missing" = 0 ] || die "release is missing assets npm/scripts/download.js expects; fix before publishing to npm"
fi

# ── 8. npm ─────────────────────────────────────────────────────────────
step "Publish to npm"
if [ "$NPM_EXISTS" = 1 ]; then
  ok "$NPM_PKG@$NPM_VERSION already published"
elif [ "$DRY_RUN" = 1 ]; then
  warn "a dry run does not bump package.json, so the version below is the"
  warn "current one ($current_npm), not the $NPM_VERSION a real run would publish."
  ( cd npm && npm publish --access public --dry-run )
  ok "[dry-run] npm publish rehearsed"
else
  warn "npm publish is effectively PERMANENT — a version cannot be reused once taken."
  if confirm "publish $NPM_PKG@$NPM_VERSION to npm?"; then
    ( cd npm && npm publish --access public )
    ok "published $NPM_PKG@$NPM_VERSION"
  else
    warn "skipped npm publish; re-run this script to finish (everything else is done)"
  fi
fi

# ── done ───────────────────────────────────────────────────────────────
step "Released $TAG"
if [ "$DRY_RUN" = 1 ]; then
  echo "  ${yellow}Dry run — nothing was tagged, pushed, or published.${reset}"
  exit 0
fi
cat <<EOF
  GitHub:   https://github.com/$REPO/releases/tag/$TAG
  Homebrew: brew install jbrahy/tap/gophermind
  npm:      npm install -g $NPM_PKG@$NPM_VERSION

  Verify:
    brew untap jbrahy/tap 2>/dev/null; brew install jbrahy/tap/gophermind && gophermind version
    npx $NPM_PKG@$NPM_VERSION version
EOF
