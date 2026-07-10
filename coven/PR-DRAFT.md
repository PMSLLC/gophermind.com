<!-- PR body for OpenCoven/coven-runtimes — submit with:
     gh pr create --repo OpenCoven/coven-runtimes --title "Add GopherMind runtime adapter (0.1.0)" --body-file coven/PR-DRAFT.md
     See the submission steps in coven/README.md. -->

## What & why

Adds **GopherMind** as a new streaming runtime adapter.

[GopherMind](https://github.com/jbrahy/gophermind.com) is a minimal, MIT-licensed
agentic coding harness for OpenAI-compatible LLMs — a single Go binary,
installable via Homebrew. It implements a Claude-Code-compatible **stream-json**
protocol (`--print --input-format stream-json --output-format stream-json`) with
**pre-assigned / resumable sessions** (`--session-id` / `--resume`) and a
**sandbox permission mapping** (`--permission-mode auto|plan`), so Coven can drive
it as a streaming runtime.

## Type

- [x] New runtime adapter (manifest)
- [ ] Update to an existing adapter
- [ ] SDK change (`coven-runtime-*` crate)
- [ ] Docs / tooling / CI

---

## If this adds or changes a runtime adapter

- [x] `conjure validate <manifest> --verbose` passes with zero problems
- [x] Every declared capability is real:
  - `stream` — working `stream_args`; the binary emits init/assistant/`tool_use`/`tool_result`/`result` stream-json
  - `preassigned_session_id` — `session_id_flag: --session-id` (+ `--resume`); sessions persist and reload
  - `sandbox` — `--permission-mode` maps `auto` (full) / `plan` (read-only: denies edits & shell)
  - `think` / `speed` — declared **`true`**: `--think low|medium|high` sends a reasoning-effort hint; `--speed` swaps in a faster model (GOPHERMIND_SPEED_MODEL or first fallback)
- [x] `id` is `[a-z0-9._-]+` (`gophermind`) and doesn't collide with a built-in
- [x] `install_hint` tells a user how to obtain the binary (`brew install jbrahy/tap/gophermind`)
- [x] Source is at `registry/runtimes/gophermind/0.1.0.json` (one adapter, `version` = filename)
- [x] Ran `conjure registry add` (rebuilds the index); `conjure registry check` is green
- [x] Not editing a released version in place — brand-new adapter
- [x] Ran `conjure test <manifest>` against the real binary — static validation + `--version` probe pass (two benign warnings: the `--model` / `--permission-mode` flags aren't in the `--version` probe output; both are verified working)

## Notes for reviewers

- **coven-core coordination:** core doesn't read manifest `capabilities` yet (still
  `harness_id == "claude"` per `docs/integration.md`), so this adapter is
  schema-valid and stream-ready but inert until the planned core PRs land. Happy
  to land it ahead of that or wait — your call.
- `think` / `speed` are now backed by real flags (`--think`, `--speed`) and declared `true`.
- GopherMind's stream-json is a documented *subset* of Claude Code's schema
  (complete messages rather than token deltas) — sufficient for Coven to parse
  turns, tool use, and results.
