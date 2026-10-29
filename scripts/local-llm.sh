#!/usr/bin/env bash
# Run the local language model for GopherMind (gpt-oss-20b, from OpenAI, official
# ggml-org GGUF) as a launchd user agent: it starts at login, restarts if it
# dies, and never touches the network.
#
#   scripts/local-llm.sh install     (re)write the wrapper and agent, load it
#   scripts/local-llm.sh status      is it loaded, running, answering
#   scripts/local-llm.sh restart     restart the running agent
#   scripts/local-llm.sh uninstall   unload and remove the agent (the model file stays)
#
# The model file must already be at $MODEL (see scripts/local-service.sh for the
# state directory). Settings can be overridden from the environment on install.
set -euo pipefail

LABEL="${GM_LLM_LABEL:-com.gophermind.local.llm}"
PORT="${GM_LLM_PORT:-8081}"
BASE="${GM_LOCAL_HOME:-$HOME/.gophermind/local}"
MODEL="${GM_LLM_MODEL:-$BASE/models/gpt-oss-20b-MXFP4.gguf}"
ALIAS="${GM_LLM_ALIAS:-gpt-oss-20b}"
CTX="${GM_LLM_CTX:-65536}"      # total, shared by the slots below
SLOTS="${GM_LLM_SLOTS:-2}"
EFFORT="${GM_LLM_EFFORT:-low}"  # reasoning effort when a request does not say
LOGDIR="$HOME/Library/Logs/gophermind-local"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DOMAIN="gui/$(id -u)"

die() { echo "error: $*" >&2; exit 1; }

healthy() { curl -fsS -m 3 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; }

install() {
  local server
  server="$(command -v llama-server)" || die "llama-server is not on PATH (brew install llama.cpp)"
  [ -s "$MODEL" ] || die "model file not found: $MODEL"
  if healthy && ! launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    die "port $PORT already answers and is not this agent; stop that process first"
  fi
  mkdir -p "$BASE" "$LOGDIR" "$(dirname "$PLIST")"

  # --offline: never fetch anything. --jinja: the model's own chat template.
  # --no-webui: the API only. Loopback only.
  cat > "$BASE/run-llm.sh" <<EOF
#!/bin/bash
set -eu
export HF_HUB_OFFLINE=1 LLAMA_OFFLINE=1
exec "$server" --model "$MODEL" --alias "$ALIAS" \\
  --host 127.0.0.1 --port $PORT --offline --jinja --no-webui \\
  -ngl 99 -fa on -c $CTX -np $SLOTS --reasoning-effort $EFFORT
EOF
  chmod 700 "$BASE/run-llm.sh"

  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BASE/run-llm.sh</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>$LOGDIR/llm.log</string>
  <key>StandardErrorPath</key><string>$LOGDIR/llm.log</string>
</dict>
</plist>
EOF
  plutil -lint "$PLIST" >/dev/null

  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  # bootout returns before launchd has finished removing the job.
  for _ in $(seq 1 20); do
    launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1 || break
    sleep 0.5
  done
  launchctl bootstrap "$DOMAIN" "$PLIST"
  launchctl kickstart -k "$DOMAIN/$LABEL"

  for _ in $(seq 1 120); do
    healthy && break
    sleep 1
  done
  healthy || die "the model server did not answer on port $PORT in 120s; see $LOGDIR/llm.log"
  echo "running: http://127.0.0.1:$PORT/v1  (model $ALIAS, log $LOGDIR/llm.log)"
}

status() {
  if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    echo "agent: loaded ($LABEL)"
    launchctl print "$DOMAIN/$LABEL" | grep -E "^\s+(state|pid|last exit code) =" || true
  else
    echo "agent: not loaded"
  fi
  if healthy; then echo "http: answering on port $PORT"; else echo "http: not answering on port $PORT"; fi
  return 0
}

restart() { launchctl kickstart -k "$DOMAIN/$LABEL"; }

uninstall() {
  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  rm -f -- "${PLIST:?}"
  echo "agent removed; the model file stays in $(dirname "$MODEL")"
}

case "${1:-install}" in
  install) install ;;
  status) status ;;
  restart) restart ;;
  uninstall) uninstall ;;
  *) die "usage: $0 install|status|restart|uninstall" ;;
esac
