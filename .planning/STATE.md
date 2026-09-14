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

Phase: 2 of 5 (gophermind-server) — COMPLETE, all 4 plans done
Plan: 4 of 4 in current phase
Status: Phase 2 done; ready for Phase 3
Last activity: 2026-09-14 — completed plan 02-04

Most recent (02-04): 02-02/02-03's own tests already covered most of the
contract, as expected, but real gaps remained: POST /run, POST
/run/stream, GET /session/{id}/messages, PATCH+DELETE /session/{id}, GET
/models, GET /skills, and HMAC verification had zero coverage. Added 11
more tests closing those, plus refactored run() the same way runServer
and parseServerConfig already separate untestable OS-facing bits
(os.Args, a real port bind, a real signal handler) from testable logic:
new runWithConfig(cfg, ln, logger, ctx) takes the listener and context as
parameters, and a new end-to-end test drives the real orchestration
(buildDeps + NewMux + startWireGuard + pipeline watcher + runServer)
against a real listener and a cancelled-context shutdown. One real
lesson surfaced while adding session CRUD tests: POST /session alone
does not create the on-disk session file -- only session.Save after an
actual turn does -- so DELETE on a session with no turns yet correctly
404s; that's not a bug, my first test draft just assumed otherwise.
Coverage: 60.5% -> 77.4% -> 83.1%, clearing the >80% target. Full suite
(35 tests) passes with -race; go build/vet clean repo-wide.

Completed-task one-liners (full detail in each commit message):
- 01-01: gophermind-lib module created, internal/ packages moved.
- 01-02: userspace WireGuard (Server/Client), fixed a double-close panic,
  added Client.HTTPClient() (the missing "local HTTP proxy" piece).
- 01-03: named SSE event structs + route contract doc on serve.NewMux.
- 02-01: gophermind-server entry point, flags/env, graceful shutdown.
- 02-02: full serve.Deps wired in; found/fixed llm.ErrNoModel bug in the
  shared gophermind-lib/llm package (not gophermind-server-specific).
- 02-03: WireGuard server init, peer registration (pluggable/fail-closed
  gocloak validator -- no real IdP wired in yet, asked rather than
  guessed).

Next task: 03-01 (Create gophermind-osx/ module, main.go, app lifecycle,
window creation) -- first task of Phase 3. This is native libui-ng
(cgo-based) macOS GUI work: I can write and unit-test the Go side, but
cannot visually verify a GUI without a display. Flag this to John before
starting -- GUI correctness will need his eyes once it exists.

Progress: [███████░░░] 30%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
