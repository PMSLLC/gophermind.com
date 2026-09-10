#!/usr/bin/env bash
# Emit docs/free-providers.md from the vendored registry (internal/freellm/data.json)
# and the compat table (internal/freellm/compat.go). Called by
# sync-free-providers.sh; the output is generated and must not be hand-edited.
#
# Every provider-specific fact in the output (which providers need no key,
# which are supported, which free-tier terms apply to whom) is read from
# `go run ./cmd/gophermind free list --json` or from data.json directly, never
# hand-typed here. That is the point: a change to compat.go (for example,
# marking a provider unsupported) shows up in this doc the next time it is
# regenerated, with no separate edit to keep in sync.
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA="$REPO_ROOT/internal/freellm/data.json"

COMPAT_JSON="$(cd "$REPO_ROOT" && go run ./cmd/gophermind free list --json)"

cat <<EOF
# Free LLM providers

Generated from \`internal/freellm/data.json\` and \`internal/freellm/compat.go\`.
Do not edit by hand; run \`scripts/sync-free-providers.sh\`.

Registry vendored from [mnfst/awesome-free-llm-apis](https://github.com/mnfst/awesome-free-llm-apis)
(CC0 1.0), upstream \`lastUpdated\`: $(jq -r .lastUpdated "$DATA").

Run one with \`gophermind --profile <profile> ask "hello"\`, or browse them with
\`gophermind free list\`.

| Provider | Models | Free tier | Link |
|---|---|---|---|
EOF

jq -r '.providers[] | "| \(.name) | \(.models | length) | \(.description | gsub("\\|"; "/")) | \(.url) |"' "$DATA"

cat <<'EOF'

## Providers that need no API key

These serve requests anonymously, which makes them the zero-signup way to try
gophermind. Each has been observed completing a real turn end to end, not just
returning a model list.
EOF

jq -nr --argjson compat "$COMPAT_JSON" --slurpfile providers "$DATA" '
  ($providers[0].providers | INDEX(.name)) as $byname |
  [ $compat[] | select(.no_key and .supported) ] as $entries |
  if ($entries | length) == 0 then
    "\n(No provider in the current registry is both no-key and supported.)"
  else
    "\n" + (
      $entries
      | map(
          . as $e
          | ($e.note // "") as $note
          | ($byname[$e.upstream].models[0].rateLimit // "") as $rl
          | "- `" + $e.profile + "` - " + $e.upstream +
            (if $note != "" then " (" + $note + ")"
             elif $rl != "" then " (" + $rl + ")"
             else "" end)
        )
      | join("\n")
    )
  end
'

cat <<'EOF'

## Profiles listed for discovery but not runnable

`gophermind free list` shows these too, so `gophermind free show <profile>`
always resolves, but `gophermind --profile <profile>` refuses to start until
the noted blocker is addressed.
EOF

jq -nr --argjson compat "$COMPAT_JSON" '
  [ $compat[] | select(.supported | not) ] as $entries |
  if ($entries | length) == 0 then
    "\n(Every profile in the current registry is supported.)"
  else
    "\n" + (
      $entries
      | map("- `" + .profile + "` - " + .upstream + ": " + (.note // ""))
      | join("\n")
    )
  end
'

cat <<'EOF'

## Free-tier terms worth reading

Some free tiers carry obligations beyond a rate limit. `gophermind free show
<profile>` prints them in full, and `gophermind free list` flags them.
EOF

jq -nr --argjson compat "$COMPAT_JSON" '
  ["non-commercial", "trains on prompts", "identity check"] as $labels |
  [
    $labels[] as $label
    | ([ $compat[] | select((.terms // []) | index($label)) | .upstream ]) as $names
    | select(($names | length) > 0)
    | "- " + $label + ": " + ($names | join(", "))
  ] as $lines |
  if ($lines | length) == 0 then
    "\n(No provider in the current registry flags a term.)"
  else
    "\n" + ($lines | join("\n"))
  end
'
