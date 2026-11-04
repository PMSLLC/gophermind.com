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
# The chat's Burrow lookup uses GM_BURROW_URL and GM_BURROW_TOKEN_FILE (defaults
# when the local Burrow token file exists); GM_BURROW_URL= or
# GM_BURROW_TOKEN_FILE= (set but empty) installs without Burrow.
set -euo pipefail

LABEL="${GM_LOCAL_LABEL:-com.gophermind.local.server}"
PORT="${GM_LOCAL_PORT:-8090}"
# The chat page's model server, without /v1: the client adds the API path.
LLM_ENDPOINT="${GM_LOCAL_LLM_ENDPOINT:-http://127.0.0.1:8081}"
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

# The secrets the example briefs declare (examples/briefs/06-hookrelay.md,
# 07-ledger-sync.md, 08-taskboard.md). A brief that declares secrets cannot load
# unless each one is in the vault, so a name not stored yet gets a placeholder.
# A stored value is never overwritten. The passphrase is read from its file with
# a read loop and handed to each gophermind command through that command's own
# environment, never on argv and never printed.
EXAMPLE_SECRETS="WEBHOOK_SIGNING_KEY SLACK_WEBHOOK_URL LEDGER_API_TOKEN JWT_SIGNING_KEY SMTP_PASSWORD ADMIN_BOOTSTRAP_TOKEN"

seed_placeholder_secrets() {
  [ -s "$BASE/vault-passphrase" ] || return 0
  local pass name stored
  IFS= read -r pass < "$BASE/vault-passphrase"
  stored="$(GOPHERMIND_VAULT_PASSPHRASE="$pass" "$BASE/bin/gophermind" brief vault list)" || die "could not list the vault (see docs/GOAL-EXAMPLES.md, Secrets for the example briefs)"
  for name in $EXAMPLE_SECRETS; do
    if printf '%s\n' "$stored" | grep -qx -- "$name"; then continue; fi
    printf '%s\n' "placeholder-$name-not-a-real-secret" | GOPHERMIND_VAULT_PASSPHRASE="$pass" "$BASE/bin/gophermind" brief vault set "$name" >/dev/null || die "could not store a placeholder for $name"
  done
}

install() {
  command -v go >/dev/null || die "go is not on PATH"
  [ -d "$repo/gophermind-server" ] || die "run this from a gophermind checkout (no gophermind-server in $repo)"

  # Burrow lookup for the chat: GM_BURROW_URL and GM_BURROW_TOKEN_FILE, or the
  # default local Burrow when its token file exists and is non-empty (only the
  # file's existence is tested here, never its contents). Setting either one to
  # the empty string (GM_BURROW_URL=) turns Burrow off. Both must end up set.
  BURROW_URL="${GM_BURROW_URL:-}"
  BURROW_TOKEN_FILE="${GM_BURROW_TOKEN_FILE:-}"
  burrow_off=0
  if [ "${GM_BURROW_URL+x}" = x ] && [ -z "$GM_BURROW_URL" ]; then burrow_off=1; fi
  if [ "${GM_BURROW_TOKEN_FILE+x}" = x ] && [ -z "$GM_BURROW_TOKEN_FILE" ]; then burrow_off=1; fi
  default_burrow_token="$HOME/OtherProjects/PMSLLC/burrow/.local/token"
  BURROW_ENV=""
  if [ "$burrow_off" = 0 ] && [ -n "$BURROW_TOKEN_FILE" ] && [ ! -s "$BURROW_TOKEN_FILE" ]; then
    die "GM_BURROW_TOKEN_FILE ($BURROW_TOKEN_FILE) does not exist or is empty"
  fi
  if [ "$burrow_off" = 0 ]; then
    if [ -z "$BURROW_TOKEN_FILE" ] && [ -s "$default_burrow_token" ]; then BURROW_TOKEN_FILE="$default_burrow_token"; fi
    if [ -n "$BURROW_TOKEN_FILE" ] && [ -z "$BURROW_URL" ]; then BURROW_URL="http://127.0.0.1:8085"; fi
  fi
  if [ "$burrow_off" = 0 ] && [ -z "$BURROW_TOKEN_FILE" ]; then
    echo "note: Burrow lookup is off (no token file found; set GM_BURROW_TOKEN_FILE)"
  fi
  if [ "$burrow_off" = 0 ] && [ -n "$BURROW_URL" ] && [ -n "$BURROW_TOKEN_FILE" ]; then
    # Both land inside "..." in run-server.sh, so these would break or inject.
    # Checked here, before any build, so a bad value leaves nothing half installed.
    burrow_nl=$'\n'
    case "$BURROW_URL$BURROW_TOKEN_FILE" in
      *[\"\$\`\\]*|*"$burrow_nl"*) die "GM_BURROW_URL and GM_BURROW_TOKEN_FILE must not contain a double quote, dollar sign, backtick, backslash or newline" ;;
    esac
    BURROW_ENV="export GOPHERMIND_BURROW_URL=\"$BURROW_URL\"
export GOPHERMIND_BURROW_TOKEN_FILE=\"$BURROW_TOKEN_FILE\""
  fi

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

  # The vault passphrase: random, mode 600, never printed. run-server.sh exports
  # it so briefs that declare secrets can open the vault without a terminal.
  if [ ! -s "$BASE/vault-passphrase" ]; then
    (umask 077; openssl rand -hex 32 > "$BASE/vault-passphrase")
  fi
  chmod 600 "$BASE/vault-passphrase"

  # The wrapper reads the token with a read loop, not `source`, so a literal $
  # in a secret is not expanded.
  cat > "$BASE/run-server.sh" <<EOF
#!/bin/bash
set -eu
IFS= read -r GOPHERMIND_TOKEN < "$BASE/token"
export GOPHERMIND_TOKEN
if [ -s "$BASE/vault-passphrase" ]; then
  IFS= read -r GOPHERMIND_VAULT_PASSPHRASE < "$BASE/vault-passphrase"
  export GOPHERMIND_VAULT_PASSPHRASE
fi
export GOPHERMIND_LOCAL=1
export GOPHERMIND_PORT=$PORT
export GOPHERMIND_ROOT="$BASE/workspace"
export GOPHERMIND_BRIEFS_DIR="$BASE/briefs"
export GOPHERMIND_REPOS_DIR="$BASE/repos"
export GOPHERMIND_HARNESS_BRIEFS="$repo/examples/briefs"
export GOPHERMIND_LLM_ENDPOINT="$LLM_ENDPOINT"
$BURROW_ENV
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

  seed_placeholder_secrets

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
