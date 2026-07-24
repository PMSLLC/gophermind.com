#!/usr/bin/env bash
# Comprehensive pre-deploy test gate.
#
# Every deployment path (scripts/deploy.sh, make deploy-*) runs this FIRST and
# refuses to ship if it fails. It is fail-closed: any stage that fails aborts
# the whole gate with a non-zero exit, so nothing is built or shipped on red.
#
# Stages, in order (cheapest first, so a formatting slip fails in a second
# rather than after a five-minute test run):
#   1. gofmt      — formatting is clean
#   2. go vet     — no vet diagnostics
#   3. go build   — the module compiles
#   4. go test -race ./...  — the full Go suite under the race detector
#   5. iOS tests  — the app's XCTest suite on a simulator (macOS only)
#
# The iOS stage can be skipped with GOPHERMIND_SKIP_IOS=1, but it prints a loud
# warning: skipping tests before a deploy is a deliberate, visible choice.
set -uo pipefail

cd "$(dirname "$0")/.."

bold=$(tput bold 2>/dev/null || true)
red=$(tput setaf 1 2>/dev/null || true)
green=$(tput setaf 2 2>/dev/null || true)
yellow=$(tput setaf 3 2>/dev/null || true)
reset=$(tput sgr0 2>/dev/null || true)

fail() {
  echo "${red}${bold}✗ pre-deploy gate FAILED at: $1${reset}" >&2
  echo "${red}  nothing was built or deployed.${reset}" >&2
  exit 1
}

step() { echo "${bold}▶ $1${reset}"; }

echo "${bold}=== pre-deploy test gate ===${reset}"

# 1. gofmt — exclude generated/vendored output.
step "gofmt"
unformatted=$(gofmt -l . | grep -v '^dist/' || true)
if [ -n "$unformatted" ]; then
  echo "${red}these files are not gofmt-clean:${reset}" >&2
  echo "$unformatted" >&2
  fail "gofmt"
fi
echo "${green}  clean${reset}"

# 2. go vet
step "go vet ./..."
go vet ./... || fail "go vet"
echo "${green}  clean${reset}"

# 3. go build
step "go build ./..."
go build ./... || fail "go build"
echo "${green}  builds${reset}"

# 4. full Go suite under the race detector
step "go test -race ./..."
go test -race ./... || fail "go test -race"
echo "${green}  all Go tests pass${reset}"

# 5. iOS tests (macOS only; the app lives here)
if [ "${GOPHERMIND_SKIP_IOS:-0}" = "1" ]; then
  echo "${yellow}${bold}⚠ SKIPPING iOS tests (GOPHERMIND_SKIP_IOS=1) — the app suite did NOT run${reset}" >&2
elif [ "$(uname)" != "Darwin" ]; then
  echo "${yellow}⚠ not macOS — skipping iOS tests${reset}"
elif [ ! -x ios/test.sh ]; then
  echo "${yellow}⚠ ios/test.sh not found — skipping iOS tests${reset}"
else
  step "iOS XCTest suite"
  ./ios/test.sh || fail "iOS tests"
  echo "${green}  all iOS tests pass${reset}"
fi

echo "${green}${bold}✓ pre-deploy gate PASSED — safe to deploy${reset}"
