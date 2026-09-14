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

Phase: 1 of 5 (gophermind-lib Foundation)
Plan: 1 of 3 in current phase
Status: In progress
Last activity: 2026-09-13 — completed plan 01-01

Next task: 01-03 (Define serve.Deps interface and SSE event types as API
contract) — recommended before 01-02, since it's the contract both Phase 2
and Phase 3 depend on and is otherwise unblocked. 01-02 (WireGuard packages)
has no dependency on 01-03 and can run in either order.

Progress: [░░░░░░░░░░] 4%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
