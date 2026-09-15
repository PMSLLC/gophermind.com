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

Phase: 4 of 5 (gophermind-osx UI) — in progress
Plan: 1 of 8 in current phase
Status: In progress
Last activity: 2026-09-14 — completed plan 04-01

Phases 1-3 complete (12 tasks: gophermind-lib foundation, gophermind-server,
gophermind-osx core). What's live: gophermind-lib (shared packages +
userspace WireGuard), gophermind-server (full HTTP/SSE API, WG peer
registration), and gophermind-osx's non-UI core (app skeleton, HTTP/SSE
client, connection manager, OAuth2/Keychain auth).

Most recent (04-01): gophermind-osx split into `ui` (pure Go: Transcript
model, regex-based syntax highlighter, SSE-to-transcript async pump,
Cmd+Enter decision logic — 97.6% coverage, no cgo) and the actual
libui-ng widget wiring (chatview.go/chatinput.go: a custom uiArea with
real colored syntax highlighting via uiAttributedString, a Send button
since libui-ng has no key-event hook on uiMultilineEntry or key-equivalent
API on uiMenuItem — checked against ui.h, not assumed).

Found and fixed a systemic bug while testing 04-01, affecting ALL
libui-ng usage in this app (not just new code): AppKit requires every
call, process-wide, on one consistent OS thread. `go test` spawns each
test on a fresh goroutine with no guaranteed thread affinity — this went
unnoticed through 03-01's tests (lucky scheduling) until `-race` crashed
it reliably. Fixed with a dedicated, OS-thread-locked worker goroutine
(`uithread_test.go`) every libui-ng-touching test now routes through
(applied retroactively to `app_test.go` too). **This pattern must be
reused for every future gophermind-osx test that touches libui-ng.**

Completed tasks 01-01 through 04-01 (13 total). `git log --oneline` per
task, full detail in each commit message. One thing to remember without
re-reading commits: libui-ng is built from source, installed to
/opt/homebrew, NOT tracked by git — see gophermind-osx/README.md on a
fresh checkout.

Next task: 04-02 (Inline approval cards, Y/N keyboard shortcut, approval
bar, timeout) — another libui-ng widget task. The same Cmd+Enter-class
limitation may apply to "Y/N keyboard shortcut"; check ui.h before
assuming it's wireable, same as 04-01. Remember the uithread_test.go
pattern for any new test file touching libui-ng.

Progress: [██████████] 57%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
