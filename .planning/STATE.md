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
Plan: 3 of 4 in current phase
Status: In progress
Last activity: 2026-09-14 — completed plan 02-03

Most recent (02-03): gophermind-server/wireguard.go. Real design decision
surfaced and asked rather than guessed: the task wanted "valid gocloak
token" validation, but no gocloak/JWT dependency or Keycloak instance
exists anywhere in this environment. Went with a pluggable TokenValidator
type; the wired-in default (stubTokenValidator) fails closed -- rejects
every token with ErrValidatorUnconfigured -- rather than an insecure
always-allow stub. Swap in a real gocloak-backed validator once a realm
exists to test against. Added peerTracker (register/renew/remove/TTL
sweep) on top of the existing wireguard.Server, plus two small additions
to gophermind-lib/wireguard itself (ErrPeerNotFound sentinel,
PeerConfig() getter) needed to support renewal without a second IPC
round trip. WG interface starts at server startup when --wg-interface is
set (empty = deliberately disabled, not an error) and closes through
runServer's existing shutdown hook -- given its own context deliberately,
not the shutdown ctx, since wireguard.Server self-closes on ctx.Done()
and that would race the ordered HTTP-drains-then-WG-closes guarantee.
7 new tests including a real end-to-end integration test (register via
the actual HTTP handler, build a wireguard.Client from the returned
config, prove an HTTP request round-trips through the tunnel). Full
gophermind-server suite (24 tests) passes with -race; verified against
the real binary too.

Completed-task one-liners (full detail in each commit message):
- 01-01: gophermind-lib module created, internal/ packages moved.
- 01-02: userspace WireGuard (Server/Client), fixed a double-close panic,
  added Client.HTTPClient() (the missing "local HTTP proxy" piece).
- 01-03: named SSE event structs + route contract doc on serve.NewMux.
- 02-01: gophermind-server entry point, flags/env, graceful shutdown.
- 02-02: full serve.Deps wired in; found/fixed llm.ErrNoModel bug in the
  shared gophermind-lib/llm package (not gophermind-server-specific).

Next task: 02-04 (integration tests for all HTTP endpoints and WG
registration) — depends on 02-03, which is now done. Given how much of
02-04's ground 02-02/02-03's own tests already cover, worth checking
what's genuinely still missing before adding more.

Progress: [██████░░░░] 26%

## Accumulated Context

### Decisions

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-09-13
Stopped at: project initialized
Resume file: None
