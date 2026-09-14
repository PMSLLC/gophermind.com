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

Phase: 3 of 5 (gophermind-osx Core) — Phase 2 complete, Phase 3 started
Plan: 1 of 4 in current phase
Status: In progress
Last activity: 2026-09-14 — completed plan 03-01

Most recent (03-01): real environment blocker hit and resolved, not
guessed around: libui-ng (the GUI toolkit Phase 3+ depends on) was not
installed anywhere -- no pkg-config entry, no Homebrew formula. Asked
before acting since installing a new system library is a real action;
built it from source (meson+ninja, ~65 targets, universal x86_64+arm64
dylib) and installed to /opt/homebrew/{lib,include}. This is NOT tracked
by git -- gophermind-osx/README.md documents the install steps, since a
fresh checkout on another machine needs it done again. Ruled out
github.com/andlabs/ui (the obvious pre-built Go binding): its bundled
darwin static lib is amd64-only from 2020, broken on this arm64 Mac.
app.go binds directly to libui-ng's C API via cgo instead, covering only
what 03-01 needs (window creation, title/size, OnClosing/ShouldQuit
lifecycle callbacks, a minimal menu with the platform Quit item).
gophermind-osx is its own Go module (module gophermind/gophermind-osx,
per the task spec) -- added to the repo's existing go.work alongside
gophermind-lib. 5 tests verify window creation/title/size/sequential
init-uninit/Show() without panicking; App.Run() (the blocking uiMain()
event loop) is deliberately NOT unit tested -- it needs a real display
session, which this environment doesn't have. That is the one piece of
03-01 genuinely unverified here: launching the real binary and seeing an
actual window, and confirming Cmd+Q/window-close actually quit, needs
your eyes. Everything else (build succeeds, no panics on the testable
lifecycle, correct title/size) is verified.

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
- 02-04: integration test coverage 60.5% -> 83.1%, run()/runWithConfig
  split for testability.

Most recent (03-02): gophermind-osx/client/ -- one method per contract
route, hand-rolled SSE parser (typed decoders reuse gophermind-lib/serve's
event structs), backoff retry (5xx/network, not 4xx). Skipped POST
/devices and "backends"/"backend-status" -- not real routes, documented
not faked. 19 tests, -race clean, 83.2% coverage.

Completed tasks 01-01 through 03-02 (9 total) -- one-liners in
`git log --oneline` per task, full detail in each commit message.
Notable cross-cutting things worth remembering without re-reading commits:
- libui-ng (Phase 3's GUI toolkit) is built from source and installed to
  /opt/homebrew, NOT tracked by git -- see gophermind-osx/README.md on a
  fresh checkout.
- Fixed a real bug in shared gophermind-lib/llm (ErrNoModel) while
  building 02-02, unrelated to gophermind-server specifically.
- Two "asked, didn't guess" design points on record: 02-03's gocloak
  token validation (pluggable, fails closed, no real IdP wired in) and
  the libui-ng install itself (confirmed before building/installing).

Next task: 03-03 (Connection manager: local/remote mode, WG tunnel
lifecycle, health checks).

Progress: [████████░░] 39%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
