# Project State

## Project Reference

See: .planning/ROADMAP.md for phase goals (there is no separate .planning/PROJECT.md).

**Core value:** Replace the Wails desktop app with gophermind-osx, a native
libui-ng macOS app talking to a standalone gophermind-server over userspace
WireGuard, both built on a shared gophermind-lib module.
**Current focus:** Phase 4 (gophermind-osx UI)

## Task detail

Full task spec (description, acceptance_criteria, depends_on) for each task
lives at `.planning/tasks/<task-id>.json` — e.g. `.planning/tasks/01-03.json`.
`assignments.json` only carries the lean summary (id/title/status/depends_on).
Read the task-id-matching file before starting work on it.

## Current Position

Phase: 5 of 5 (E2E tests and QA) — in progress
Plan: 2 of 7 in current phase
Status: In progress
Last activity: 2026-09-14 — 05-01 completed (local mode E2E: full flow, approval, session management)

Phases 1-4 complete (19 tasks: gophermind-lib foundation, gophermind-server,
gophermind-osx core + full UI). What's live: gophermind-lib (shared packages +
userspace WireGuard), gophermind-server (full HTTP/SSE API, WG peer
registration), and gophermind-osx (complete native macOS app: chat transcript
with syntax highlighting, approval cards, right panel with model picker /
sessions / pipeline, settings, native macOS integration).

Most recent (05-01, completed): E2E local mode tests in `e2e_local_test.go`
(build tag `e2e`): full flow (launch → create session → chat → stream →
done), approval flow (approval-needed event → tracker → resolve), session
management (create → rename → resume → delete). All pass with `-race`.

Completed tasks 01-01 through 05-01 (20 total). `git log --oneline` per
task, full detail in each commit message. One thing to remember without
re-reading commits: libui-ng is built from source, installed to
/opt/homebrew, NOT tracked by git — see gophermind-osx/README.md on a
fresh checkout.

Next: 05-02 (E2E remote mode: WG tunnel → chat → approve → disconnect),
05-03 (E2E pipeline: breakdown → execute → monitor), 05-04 (E2E model
switching: pin → cycle → switch), 05-05 (error scenario tests), 05-06
(performance benchmarks), 05-07 (UI refinement, accessibility, final QA).

Progress: [████████████] 77%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-14
Stopped at: 05-01 completed. Next: 05-02 (E2E remote mode: WG tunnel →
chat → approve → disconnect).
Resume file: None
