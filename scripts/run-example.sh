#!/usr/bin/env bash
# Submit one brief to the local GopherMind server, wait for the run to end, and
# print what happened.
#
#   scripts/run-example.sh examples/briefs/09-slugify.md [max-minutes]
#
# max-minutes defaults to 50. Exit code: 0 when the run is done, 1 when it ended
# any other way, 2 when it was still running at the limit (look again with
# scripts/run-example.sh --watch <run-id>), 3 when the brief was refused.
# GM_BASE overrides the server address (default http://127.0.0.1:8090).
set -uo pipefail

BASE="${GM_BASE:-http://127.0.0.1:8090}"
STATE_DIR="${GM_STATE:-$HOME/.gophermind/local}"

report() {
  local id="$1" start="$2"
  curl -s -m 10 "$BASE/app/briefs/$id/state" | python3 -c '
import sys, json
from collections import Counter
d = json.load(sys.stdin)
r = d["run"]
print("run:", r.get("id"), "phase:", r.get("phase"), "exit:", r.get("exit_code"), "|", r.get("error") or "")
print("stages (stage, calls, ok, problems, seconds):")
for s in d["stages"]:
    print("  %-14s %3d %3d %3d %6d" % (s["stage"], s["calls"], s["ok"], s["problems"], s["duration_ms"] // 1000))
if d.get("stop_reason"):
    print("why it stopped:", d["stop_reason"][:400])
leaves = [n for n in d["nodes"] if n["kind"] == "function"]
print("leaves:", len(leaves), dict(Counter(n["status"] for n in leaves)))
a = d.get("acceptance")
if a:
    print("acceptance:", json.dumps(a)[:300])
errs = d["call_errors"]
print("failed calls:", len(errs))
for e in errs[:5]:
    print("  -", e["outcome"], "|", e["stage"], "|", e.get("provider", ""), "|", e["detail"][:140])
' 2>&1
  echo "wall time: $(( $(date +%s) - start ))s"
}

watch_run() {
  local id="$1" limit="$2" start="$3" ph
  while :; do
    ph=$(curl -s -m 10 "$BASE/app/briefs/$id/state" | python3 -c 'import sys,json; print(json.load(sys.stdin)["run"]["phase"])' 2>/dev/null)
    case "$ph" in
      planning|running|waiting|starting|"") ;;
      *) break ;;
    esac
    if [ $(( $(date +%s) - start )) -gt $(( limit * 60 )) ]; then
      report "$id" "$start"
      echo "STILL RUNNING after $limit minutes"
      return 2
    fi
    sleep 30
  done
  report "$id" "$start"
  [ "$ph" = "done" ]
}

if [ "${1:-}" = "--watch" ]; then
  watch_run "${2:?run id}" "${3:-50}" "$(date +%s)"
  exit $?
fi

brief="${1:?usage: run-example.sh <brief.md> [max-minutes]}"
limit="${2:-50}"
[ -f "$brief" ] || { echo "no such file: $brief" >&2; exit 3; }
start=$(date +%s)
reply=$(curl -s -m 30 -X POST --data-binary @"$brief" -H 'Content-Type: text/markdown' "$BASE/app/briefs?resubmit=1")
id=$(printf '%s' "$reply" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("run_id",""))' 2>/dev/null)
if [ -z "$id" ]; then
  echo "refused: $reply" >&2
  exit 3
fi
echo "submitted $brief as $id"
watch_run "$id" "$limit" "$start"
