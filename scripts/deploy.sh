#!/usr/bin/env bash
# Canonical deploy path for gophermind. It ALWAYS runs the comprehensive
# pre-deploy test gate first (scripts/predeploy.sh) and refuses to ship if the
# gate fails — the whole point is that tests cannot be skipped on the way to a
# deployment.
#
#   scripts/deploy.sh local            # gate, then rebuild the local binary
#   scripts/deploy.sh server           # gate, then build+ship to the server
#   scripts/deploy.sh phone            # gate, then build+install on the iPhone
#   scripts/deploy.sh all              # gate once, then local + server + phone
#
# Env:
#   GOPHERMIND_SKIP_IOS=1   skip the iOS stage of the gate (loud warning)
#   SERVER_HOST=host        the deploy host (required for the server/all targets;
#                           set in .env or export it — see .env.example)
set -uo pipefail
cd "$(dirname "$0")/.."

TARGET="${1:-}"
case "$TARGET" in
  local|server|phone|all) ;;
  *) echo "usage: scripts/deploy.sh {local|server|phone|all}" >&2; exit 2 ;;
esac

[ -f .env ] && set -a && source .env && set +a
if [ "$TARGET" = "server" ] || [ "$TARGET" = "all" ]; then
  SERVER_HOST="${SERVER_HOST:?SERVER_HOST not set — add it to .env or export it (see .env.example)}"
fi
SERVER_HOST="${SERVER_HOST:-}"
SERVER_BIN="/usr/local/bin/gophermind"
SERVER_UNIT="gophermind.service"

bold=$(tput bold 2>/dev/null || true); reset=$(tput sgr0 2>/dev/null || true)

# ---- THE GATE: no deploy proceeds unless this passes ------------------------
./scripts/predeploy.sh || {
  echo "deploy aborted: pre-deploy gate failed." >&2
  exit 1
}

COMMIT=$(git rev-parse --short HEAD)
DATE=$(git log -1 --format=%cI)

# Derived from the latest tag, not hardcoded. This line read "0.5.0+dev" from
# the day 0.5.0 was current until v0.7.0 shipped, so every local and server
# deploy in between reported a version it was not: the one number you check to
# find out what is running was the one number guaranteed to be stale. The
# fallback covers a shallow clone or a repo with no tags yet.
BASE_VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
LDFLAGS="-X gophermind/internal/version.Version=${BASE_VERSION:-0.0.0}+dev -X gophermind/internal/version.Commit=${COMMIT} -X gophermind/internal/version.Date=${DATE}"

deploy_local() {
  echo "${bold}▶ local: rebuilding ./gophermind${reset}"
  go build -ldflags "$LDFLAGS" -o gophermind ./cmd/gophermind
  ./gophermind --version
}

deploy_server() {
  echo "${bold}▶ server: building linux/amd64 and shipping to ${SERVER_HOST}${reset}"
  local tmp; tmp=$(mktemp -d)
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w $LDFLAGS" -o "$tmp/gophermind" ./cmd/gophermind
  local sum; sum=$(shasum -a 256 "$tmp/gophermind" | cut -d' ' -f1)
  scp -o ConnectTimeout=10 "$tmp/gophermind" "${SERVER_HOST}:/tmp/gophermind-new"
  ssh -o BatchMode=yes "$SERVER_HOST" "set -e
    got=\$(shasum -a 256 /tmp/gophermind-new | cut -d' ' -f1)
    [ \"\$got\" = \"$sum\" ] || { echo 'checksum mismatch after upload' >&2; exit 1; }
    chmod +x /tmp/gophermind-new
    sudo cp -p $SERVER_BIN ${SERVER_BIN}.bak-\$(date -u +%Y%m%dT%H%M%SZ)
    sudo install -o root -g root -m 0755 /tmp/gophermind-new $SERVER_BIN
    sudo systemctl restart $SERVER_UNIT; sleep 3
    echo \"state=\$(systemctl is-active $SERVER_UNIT)  version=\$($SERVER_BIN --version)\"
    curl -sf --max-time 8 http://127.0.0.1:8090/healthz >/dev/null && echo 'healthz ok' || { echo 'healthz FAILED' >&2; exit 1; }"
  rm -rf "$tmp"
}

deploy_phone() {
  echo "${bold}▶ phone: building and installing GopherMind.app${reset}"
  ./ios/deploy.sh
}

case "$TARGET" in
  local)  deploy_local ;;
  server) deploy_server ;;
  phone)  deploy_phone ;;
  all)    deploy_local; deploy_server; deploy_phone ;;
esac

echo "${bold}✓ deploy complete: $TARGET @ ${COMMIT}${reset}"
