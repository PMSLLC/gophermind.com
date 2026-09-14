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
Plan: 2 of 3 in current phase
Status: In progress
Last activity: 2026-09-14 — completed plan 01-03

01-03 findings: serve.Deps, session.Info, modelcat.Entry, skills.Skill, and
phaseflow.Task all already existed. Added the two genuinely missing pieces:
named SSE event structs with JSON tags (gophermind-lib/serve/events.go,
wired into sse.go/approval.go/pipeline.go, replacing inline anonymous
structs) and a full route contract as a doc comment on serve.NewMux. Note:
ModelSwitchedEvent's shape is defined but not yet emitted anywhere — no
model-fallback/cycling code path exists yet to wire it to.

Next task: 01-02 (Add userspace WireGuard client and server packages) — the
only remaining Phase 1 task, unblocks Phase 2's 02-03.

Progress: [██░░░░░░░░] 9%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
