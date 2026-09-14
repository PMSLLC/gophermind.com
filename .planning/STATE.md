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
Plan: 2 of 4 in current phase
Status: In progress
Last activity: 2026-09-14 — completed plan 02-02

Most recent (02-02): gophermind-server/server.go's buildDeps wires a real
serve.Deps -- Run/Stream/SessionTurn build one agent.Agent per turn the
same way the CLI's own "serve" command does, SessionTurn always through
serve.RemoteApprovalGate (headless server, no terminal to prompt). Full
mux now live via serve.NewMux (auth/rate-limiting/HMAC all came for free
from that). Along the way, found and fixed a real bug in the SHARED
gophermind-lib/llm package: a Client with no Model set made
connectStreamChain return (nil, nil), which Stream then panicked on
(nil pointer) and Complete silently "succeeded" with an empty message --
neither was gophermind-server-specific, any caller with an unset model
hit this. Fixed via a new llm.ErrNoModel sentinel; buildDeps also now
calls DiscoverModel at startup (matching the CLI), so this only matters
as a defense-in-depth backstop. 17 gophermind-server tests + 2 new
llm-package regression tests, all pass with -race. Verified against the
real binary too (not just httptest): auth, session list, graceful
shutdown all confirmed live.

Completed-task one-liners (full detail in each commit message):
- 01-01: gophermind-lib module created, internal/ packages moved.
- 01-02: userspace WireGuard (Server/Client), fixed a double-close panic,
  added Client.HTTPClient() (the missing "local HTTP proxy" piece).
- 01-03: named SSE event structs + route contract doc on serve.NewMux.
- 02-01: gophermind-server entry point, flags/env, graceful shutdown.

Next task: 02-03 (WireGuard server init, peer registration endpoint,
per-backend tunnel management) — depends on 02-02 (done) and 01-02 (done).

Progress: [█████░░░░░] 22%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
