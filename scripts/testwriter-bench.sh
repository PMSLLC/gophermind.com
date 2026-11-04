#!/usr/bin/env bash
# Resume failed runs through the planner and print one line per run:
#   id  before=<stage>  after=<stage>  result=<ok|failed: ...>
# Usage: scripts/testwriter-bench.sh <run-id>...
# Uses the same environment as the local service (read from run-server.sh
# with grep, never sourced) and a fresh CLI binary built into a temp folder.
set -u

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <run-id>..." >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
local_dir="$HOME/.gophermind/local"
base="http://127.0.0.1:8090"
limit=2400

# Same exports the service uses, except the tokens (never exported here).
while IFS= read -r line; do
  case "$line" in
    "export GOPHERMIND_TOKEN"*|"export GOPHERMIND_BURROW_TOKEN"*) continue ;;
    "export "*) eval "$line" ;;
  esac
done < <(grep -E '^export [A-Z_]+=' "$local_dir/run-server.sh")

pass_file="$local_dir/vault-passphrase"
if [ -f "$pass_file" ]; then
  pass=""
  IFS= read -r pass < "$pass_file" || [ -n "$pass" ]
  [ -n "$pass" ] && export GOPHERMIND_VAULT_PASSPHRASE="$pass"
fi

# Refuse while the service is busy.
active="$(curl -fsS "$base/app/api/briefs" | sed -n 's/.*"active_run_id":"\([^"]*\)".*/\1/p')"
if [ -n "$active" ]; then
  echo "refusing: run $active is active" >&2
  exit 1
fi
if curl -fsS "$base/app/api/harness" | grep -q '"running":true'; then
  echo "refusing: the harness is running" >&2
  exit 1
fi

tmp_root="${TMPDIR:-/tmp}"
tmp_root="${tmp_root%/}"
tmp="$(mktemp -d)"
cleanup() {
  target="$tmp"
  case "$target" in
    "$tmp_root"/*|/tmp/*|/var/folders/*|/private/var/folders/*|/private/tmp/*)
      echo "removing $target" >&2
      rm -rf -- "${target:?}" ;;
    *) echo "not removing unexpected temp path: $target" >&2 ;;
  esac
}
trap cleanup EXIT

(cd "$root" && go build -o "$tmp/gophermind" ./cmd/gophermind) || { echo "build failed" >&2; exit 1; }

# The CLI does not write run.log (the server does), so read the planner's
# own state: planner_error.json names the stage it stopped at, and
# status.json carries planned_at once planning finished.
err_stage() {
  sed -n 's/.*"stage": *"\([^"]*\)".*/\1/p' "$1" 2>/dev/null | head -n 1
}
err_text() {
  sed -n 's/.*"text": *"\(.*\)",\{0,1\}$/\1/p' "$1" 2>/dev/null | head -n 1
}

for id in "$@"; do
  state="$local_dir/repos/$id/.gophermind/$id/_state"
  before="$(err_stage "$state/planner_error.json")"
  out="$tmp/$id.out"
  perl -e 'alarm shift; exec @ARGV' "$limit" "$tmp/gophermind" brief resume "$id" --yes --gate file > "$out" 2>&1
  rc=$?
  if [ -f "$state/planner_error.json" ]; then
    after="$(err_stage "$state/planner_error.json")"
  elif grep -q '"planned_at"' "$state/status.json" 2>/dev/null; then
    after="planned"
  else
    after="unknown"
  fi
  case "$rc" in
    0|3) result="ok" ;;
    142) result="failed: timed out after ${limit}s" ;;
    *)
      msg="$(err_text "$state/planner_error.json")"
      [ -z "$msg" ] && msg="$(tail -n 1 "$out")"
      result="failed: $(printf '%s' "$msg" | cut -c1-160)" ;;
  esac
  echo "$id  before=${before:-none}  after=$after  result=$result"
done
