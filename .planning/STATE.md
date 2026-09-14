# Project State

## Project Reference

See: .planning/ROADMAP.md for phase goals (there is no separate .planning/PROJECT.md).

**Core value:** Replace the Wails desktop app with gophermind-osx, a native
libui-ng macOS app talking to a standalone gophermind-server over userspace
WireGuard, both built on a shared gophermind-lib module.
**Current focus:** Phase 2 (gophermind-server)

## Task detail

Full task spec (description, acceptance_criteria, depends_on) for each task
lives at `.planning/tasks/<task-id>.json` — e.g. `.planning/tasks/01-03.json`.
`assignments.json` only carries the lean summary (id/title/status/depends_on).
Read the task-id-matching file before starting work on it.

## Current Position

Phase: 2 of 5 (gophermind-server) — Phase 1 complete, Phase 2 in progress
Plan: 1 of 4 in current phase
Status: In progress
Last activity: 2026-09-14 — completed plan 02-01

Most recent (02-01): gophermind-server/main.go — flag/env parsing, refuses
to start without a token, graceful shutdown verified against the real
binary with an actual SIGINT. Serves only /healthz/readyz/metrics so far —
serve.NewMux + full Deps wiring is 02-02's job. WG interface close is a
wired-but-nil io.Closer hook for 02-03. 11 tests pass with -race. Full
detail: commit history (`git log --oneline -- gophermind-server/`).

Completed-task one-liners (full detail in each commit message):
- 01-01: gophermind-lib module created, internal/ packages moved.
- 01-02: userspace WireGuard (Server/Client), fixed a double-close panic,
  added Client.HTTPClient() (the missing "local HTTP proxy" piece).
- 01-03: named SSE event structs + route contract doc on serve.NewMux.

Next task: 02-02 (Wire serve.Deps, start HTTP listener, register all
routes) — depends only on 02-01, which is now done.

Progress: [████░░░░░░] 17%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
