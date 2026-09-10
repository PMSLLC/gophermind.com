#!/usr/bin/env bash
# Refresh the vendored free-provider registry from upstream and regenerate the
# docs table. The drift tests in internal/freellm are the point of step 5: a
# sync that invalidates compat.go fails here instead of at runtime.
set -euo pipefail

UPSTREAM="https://raw.githubusercontent.com/mnfst/awesome-free-llm-apis/main/data.json"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$REPO_ROOT/internal/freellm/data.json"
DOCS="$REPO_ROOT/docs/free-providers.md"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Fetching $UPSTREAM"
curl -fsSL "$UPSTREAM" -o "$tmp"

echo "Validating"
jq -e '.providers | length > 0' "$tmp" >/dev/null || {
  echo "FAILED: upstream data.json has no providers; leaving the vendored copy alone" >&2
  exit 1
}

if cmp -s "$tmp" "$DEST"; then
  echo "Already up to date ($(jq -r .lastUpdated "$DEST"))"
else
  cp "$tmp" "$DEST"
  echo "Updated to $(jq -r .lastUpdated "$DEST")"
fi

echo "Regenerating $DOCS"
"$REPO_ROOT/scripts/gen-free-providers-doc.sh" > "$DOCS"

echo "Running drift tests"
cd "$REPO_ROOT"
go test ./internal/freellm/... ./cmd/gophermind/... || {
  echo "" >&2
  echo "FAILED: the vendored registry no longer matches compat.go." >&2
  echo "Fix internal/freellm/compat.go to match the new data.json." >&2
  echo "Do NOT edit data.json to match compat.go." >&2
  exit 1
}
echo "Sync complete."
