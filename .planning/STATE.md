# Project State

## Project Reference

See: .planning/ROADMAP.md for phase goals (there is no separate .planning/PROJECT.md).

**Core value:** Replace the Wails desktop app with gophermind-osx, a native
libui-ng macOS app talking to a standalone gophermind-server over userspace
WireGuard, both built on a shared gophermind-lib module.
**Current focus:** Phase 1

## Task detail

Full task spec (description, acceptance_criteria, depends_on) for each task
lives at `.planning/tasks/<task-id>.json` — e.g. `.planning/tasks/01-03.json`.
`assignments.json` only carries the lean summary (id/title/status/depends_on).
Read the task-id-matching file before starting work on it.

## Current Position

Phase: 1 of 5 (gophermind-lib Foundation) — COMPLETE, all 3 plans done
Plan: 3 of 3 in current phase
Status: Phase 1 done; ready for Phase 2
Last activity: 2026-09-14 — completed plan 01-02

01-03 findings: serve.Deps, session.Info, modelcat.Entry, skills.Skill, and
phaseflow.Task all already existed. Added the two genuinely missing pieces:
named SSE event structs with JSON tags (gophermind-lib/serve/events.go,
wired into sse.go/approval.go/pipeline.go, replacing inline anonymous
structs) and a full route contract as a doc comment on serve.NewMux. Note:
ModelSwitchedEvent's shape is defined but not yet emitted anywhere — no
model-fallback/cycling code path exists yet to wire it to.

01-02 findings: gophermind-lib/wireguard/ already had a substantial
implementation (netstack-based, no TUN/sudo, matching the real
golang.zx2c4.com/wireguard library) but go test panicked ("close of closed
channel"). Root cause: closeLocked() manually closed the raw TUN, but
device.NewDevice starts RoutineReadFromTUN immediately (before Up() is even
called) — that routine detects the closed TUN and calls device.Close() on
its own, which closes the same TUN a second time. Fixed by never touching
the TUN directly and calling device.Close() everywhere instead (it closes
the TUN exactly once, internally, and is itself idempotent) — in
closeLocked and in both setup-failure paths (IpcSet/Up failing), which had
the identical race. Also added Client.HTTPClient(), the "local HTTP proxy"
piece the task's acceptance criteria named but the code didn't have (only
raw DialTCP existed) — an *http.Client whose Transport dials through
netstack.Net.DialContext. All 7 tests pass with -race, including a new one
proving HTTPClient() actually round-trips through the tunnel.

Next task: Phase 2 — gophermind-server (02-01 first: entry point, config
loading, graceful shutdown).

Progress: [███░░░░░░░] 13%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
