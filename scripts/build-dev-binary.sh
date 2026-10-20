#!/usr/bin/env bash
# Build the version-stamped dev binary used by graded /project runs.
#
# Guard around the same ldflags as the Makefile's rebuild-all, without its
# gofmt -w, commit or desktop deploy. Refuses a dirty tree and an unpushed
# HEAD, so the stamped sha names code that exists on a remote.
#
# Usage: scripts/build-dev-binary.sh [--dry-run]
# Env:   GM_REPO     repo to build (default: this script's repo root)
#        GM_DEV_BIN  output path (default: $HOME/.gophermind/bin/gophermind-dev)
set -euo pipefail

# Only the git on PATH, with the inherited git environment removed.
git() { env -u GIT_DIR -u GIT_INDEX_FILE -u GIT_WORK_TREE git "$@"; }

dry_run=0
case "${1:-}" in
  "") ;;
  --dry-run) dry_run=1 ;;
  *) echo "usage: build-dev-binary.sh [--dry-run]" >&2; exit 2 ;;
esac
if [ "$#" -gt 1 ]; then
  echo "usage: build-dev-binary.sh [--dry-run]" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GM_REPO="${GM_REPO:-$(dirname "$script_dir")}"
GM_DEV_BIN="${GM_DEV_BIN:-$HOME/.gophermind/bin/gophermind-dev}"

if [ -n "$(git -C "$GM_REPO" status --porcelain)" ]; then
  echo "refusing to build: the tree is dirty" >&2
  exit 1
fi
if [ -z "$(git -C "$GM_REPO" branch -r --contains HEAD)" ]; then
  echo "refusing to build: HEAD is not pushed" >&2
  exit 1
fi

sha="$(git -C "$GM_REPO" rev-parse --short HEAD)"
date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ldflags="-X gophermind/gophermind-lib/version.Version=dev+$sha -X gophermind/gophermind-lib/version.Commit=$sha -X gophermind/gophermind-lib/version.Date=$date"

# Write only inside the output's own directory, and never through a symlink.
case "$GM_DEV_BIN" in
  /*) ;;
  *) echo "refusing to build: GM_DEV_BIN must be an absolute path: $GM_DEV_BIN" >&2; exit 1 ;;
esac
bin_dir="$(dirname "$GM_DEV_BIN")"
if [ -L "$bin_dir" ]; then
  echo "refusing to build: $bin_dir is a symlink" >&2
  exit 1
fi
if [ -L "$GM_DEV_BIN" ]; then
  echo "refusing to build: $GM_DEV_BIN is a symlink" >&2
  exit 1
fi

if [ "$dry_run" -eq 1 ]; then
  echo "ldflags: $ldflags"
  echo "output: $GM_DEV_BIN"
  exit 0
fi

mkdir -p "$bin_dir"
if [ -L "$bin_dir" ]; then
  echo "refusing to build: $bin_dir is a symlink" >&2
  exit 1
fi
(cd "$GM_REPO" && go build -ldflags "$ldflags" -o "$GM_DEV_BIN" ./cmd/gophermind)

printed="$("$GM_DEV_BIN" version)"
echo "$printed"
case "$printed" in
  *"(commit $sha,"*) ;;
  *)
    echo "built binary reports a different commit than $sha: $printed" >&2
    exit 1
    ;;
esac
echo "binary: $GM_DEV_BIN"
echo "version: dev+$sha"
echo "commit: $sha"
