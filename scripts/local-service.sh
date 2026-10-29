#!/usr/bin/env bash
# Run gophermind-server on this laptop as a launchd user agent: it starts at
# login, restarts if it dies, and keeps its state under ~/.gophermind/local.
#
#   scripts/local-service.sh install     build, install, (re)load the agent
#   scripts/local-service.sh status      is it loaded, running, answering
#   scripts/local-service.sh restart     restart the running agent
#   scripts/local-service.sh uninstall   unload and remove the agent (state stays)
#
# The server runs in local mode (GOPHERMIND_LOCAL=1): 127.0.0.1 only, no login.
# Settings below can be overridden from the environment when installing.
set -euo pipefail

LABEL="${GM_LOCAL_LABEL:-com.gophermind.local.server}"
PORT="${GM_LOCAL_PORT:-8090}"
LLM_ENDPOINT="${GM_LOCAL_LLM_ENDPOINT:-http://127.0.0.1:8000/v1}"
# Offline by default: a brief run must never wait on a module proxy, a Go
# toolchain download or a model hub. GM_LOCAL_ONLINE=1 at install time leaves
# those settings unset, for building programs that need downloaded modules.
if [ "${GM_LOCAL_ONLINE:-0}" = "1" ]; then OFFLINE_ENV=""; else OFFLINE_ENV='export GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1'; fi
BASE="${GM_LOCAL_HOME:-$HOME/.gophermind/local}"
LOGDIR="$HOME/Library/Logs/gophermind-local"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DOMAIN="gui/$(id -u)"

repo="$(cd "$(dirname "$0")/.." && pwd)"

die() { echo "error: $*" >&2; exit 1; }

healthy() { curl -fsS -m 3 "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; }

install() {
  command -v go >/dev/null || die "go is not on PATH"
  [ -d "$repo/gophermind-server" ] || die "run this from a gophermind checkout (no gophermind-server in $repo)"

  # Another process on the port that launchd does not own would make the agent
  # crash-loop. Say so instead of killing something that is not ours.
  if healthy && ! launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    die "port $PORT already answers and is not this agent; stop that process first"
  fi

  sha="$(git -C "$repo" rev-parse --short HEAD)"
  date="$(git -C "$repo" show -s --format=%cI HEAD)"
  if [ -n "$(git -C "$repo" status --porcelain --untracked-files=no)" ]; then
    echo "note: the working tree has uncommitted changes; building them as $sha+dirty"
    sha="$sha+dirty"
  fi
  ldflags="-X gophermind/gophermind-lib/version.Version=dev+$sha -X gophermind/gophermind-lib/version.Commit=$sha -X gophermind/gophermind-lib/version.Date=$date"

  mkdir -p "$BASE/bin" "$BASE/briefs" "$BASE/repos" "$BASE/workspace" "$LOGDIR" "$(dirname "$PLIST")"
  chmod 700 "$BASE"

  echo "building $sha"
  (cd "$repo/gophermind-server" && go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$BASE/bin/gophermind-server.new" .)
  (cd "$repo" && go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$BASE/bin/gophermind.new" ./cmd/gophermind)
  mv -f "$BASE/bin/gophermind-server.new" "$BASE/bin/gophermind-server"
  mv -f "$BASE/bin/gophermind.new" "$BASE/bin/gophermind"
  echo "$sha" > "$BASE/INSTALLED_COMMIT"

  if [ ! -s "$BASE/token" ]; then
    (umask 077; openssl rand -hex 24 > "$BASE/token")
  fi
  chmod 600 "$BASE/token"

  # The wrapper reads the token with a read loop, not `source`, so a literal $
  # in a secret is not expanded.
  cat > "$BASE/run-server.sh" <<EOF
#!/bin/bash
set -eu
IFS= read -r GOPHERMIND_TOKEN < "$BASE/token"
export GOPHERMIND_TOKEN
export GOPHERMIND_LOCAL=1
export GOPHERMIND_PORT=$PORT
export GOPHERMIND_ROOT="$BASE/workspace"
export GOPHERMIND_BRIEFS_DIR="$BASE/briefs"
export GOPHERMIND_REPOS_DIR="$BASE/repos"
export GOPHERMIND_HARNESS_BRIEFS="$repo/examples/briefs"
export GOPHERMIND_LLM_ENDPOINT="$LLM_ENDPOINT"
export PATH="$BASE/bin:/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"
$OFFLINE_ENV
exec "$BASE/bin/gophermind-server" -wg-interface=
EOF
  chmod 700 "$BASE/run-server.sh"

  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BASE/run-server.sh</string></array>
  <key>WorkingDirectory</key><string>$BASE/workspace</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$LOGDIR/server.log</string>
  <key>StandardErrorPath</key><string>$LOGDIR/server.log</string>
</dict>
</plist>
EOF
  plutil -lint "$PLIST" >/dev/null

  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  # bootout returns before launchd has finished removing the job; loading it
  # again too soon fails with "Input/output error".
  for _ in $(seq 1 20); do
    launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1 || break
    sleep 0.5
  done
  launchctl bootstrap "$DOMAIN" "$PLIST"
  launchctl kickstart -k "$DOMAIN/$LABEL"

  for _ in $(seq 1 30); do
    healthy && break
    sleep 1
  done
  healthy || die "the server did not answer on port $PORT in 30s; see $LOGDIR/server.log"
  echo "running: http://127.0.0.1:$PORT/  (commit $sha, log $LOGDIR/server.log)"
}

status() {
  if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    echo "agent: loaded ($LABEL)"
    launchctl print "$DOMAIN/$LABEL" | grep -E "^\s+(state|pid|last exit code) =" || true
  else
    echo "agent: not loaded"
  fi
  if healthy; then echo "http: answering on port $PORT"; else echo "http: not answering on port $PORT"; fi
  [ -f "$BASE/INSTALLED_COMMIT" ] && echo "commit: $(cat "$BASE/INSTALLED_COMMIT")"
  return 0
}

restart() { launchctl kickstart -k "$DOMAIN/$LABEL"; }

uninstall() {
  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  rm -f -- "${PLIST:?}"
  echo "agent removed; state and token stay in $BASE"
}

case "${1:-install}" in
  install) install ;;
  status) status ;;
  restart) restart ;;
  uninstall) uninstall ;;
  *) die "usage: $0 install|status|restart|uninstall" ;;
esac
