# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-26)

**Core value:** Interview a person continuously across email, SMS, and live phone calls until their whole life is recorded at the finest resolution they will give.
**Current focus:** Phase 1 — Tenant Foundation

## Current Position

Phase: 1 of 10 (Tenant Foundation)
Plan: 0 of 4 in current phase
Status: Planned, awaiting approval
Last activity: 2026-07-26 — roadmap and assignments authored from the design spec

Progress: [░░░░░░░░░░] 0%

## Accumulated Context

### Decisions

- Engine approach **C built as A**: the deterministic planner ships first
  (Phases 1–7), the bounded in-session agentic loop arrives with voice in
  Phase 8. Rationale and rejected alternatives in PROJECT.md.
- The **coverage grid** is the core abstraction. It converts "interview about
  everything" into a scored, finite, inspectable work queue, and it renders
  directly as the timeline heatmap in Phase 7.
- **Local-only inference** is a hard requirement, not a default. Enforced by an
  automated egress guard in 10-02.
- **Mutes are honored at schedule time**, never at send time, so a muted loop
  never occupies queue space or influences scoring.
- Deploy is a **second service on the existing gophermind box** (10.0.0.5),
  reusing the local qwen3.6-35b rather than provisioning new inference hardware.

### Blockers/Concerns

- **Voice latency (Phase 8) is the highest-risk item in the roadmap.** An 800 ms
  end-to-end budget with local STT, a 35B model turn, and local TTS on a box
  already serving gophermind is tight. If it proves unreachable, the fallback is
  scripted IVR with async transcription, which costs in-session follow-up but
  keeps every other phase intact. Decide by 08-02, not later.
- **A2P 10DLC registration (05-03) has external lead time** measured in weeks and
  is not under our control. Start the brand and campaign filing during Phase 1 so
  it does not gate Phase 5.
- The crisis path (10-01) is specified last but **its behavior must be decided
  before any real subject is interviewed**, not before the code ships. If a live
  subject is onboarded ahead of Phase 10, pull 10-01 forward as an inserted phase.

## Session Continuity

Last session: 2026-07-26
Stopped at: spec, roadmap, and assignments authored; plan not yet approved
Resume file: None
