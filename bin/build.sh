#!/usr/bin/env bash
# Build the latest gophermind binary from source and launch it.
# Any arguments are passed straight through, e.g.:
#   bin/build.sh                 # interactive session
#   bin/build.sh run "do X"      # one-shot
#   bin/build.sh ask "where?"    # one-shot, read-only
set -euo pipefail

# Resolve the repo root (this script lives in bin/).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"
cd "$ROOT"

echo "› building gophermind…" >&2
go build -o gophermind ./cmd/gophermind

echo "› launching" >&2
exec ./gophermind "$@"
