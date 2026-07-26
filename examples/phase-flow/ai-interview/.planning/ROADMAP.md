# Roadmap: AI Interview

## Overview

The system is built from the inside out. First a tenant-safe foundation, then the
life-timeline store and the coverage grid that models what is unknown, then the
deterministic planner that reads holes in that grid and produces the next
question. Extraction closes the loop by turning answers back into events, and the
open-loop sweeper gives the system its memory for unfinished situations. Only
then do channels attach: email and SMS first, on a channel interface built
voice-ready from the start.

By the end of Phase 7 a subject can sign up, consent, receive real questions over
email and SMS, and browse their own life archive as a coverage heatmap — a
complete product on the deterministic engine alone. Phase 8 adds the real-time
voice agent and the bounded in-session follow-up loop, which is where approach C
finally supersedes A. Phase 9 makes the system predictive rather than merely
thorough. Phase 10 makes it safe to point at a real person's entire life, and
puts it on the box.

Each phase is independently shippable. A phase that cannot be demonstrated to the
subject as working behavior is scoped wrong.

## Phases

- [ ] **Phase 1: Tenant Foundation** - Go service skeleton, Postgres schema, row-level security, subject auth
- [ ] **Phase 2: Timeline and Coverage Grid** - the life-event store and the multi-resolution model of what is unknown
- [ ] **Phase 3: Deterministic Interview Planner** - scored cell selection and traceable question generation
- [ ] **Phase 4: Extraction and Open Loops** - answers become events; unresolved situations get tracked and re-probed
- [ ] **Phase 5: Email and SMS Channels** - two-way, consented, compliant contact on a voice-ready channel interface
- [ ] **Phase 6: Contact Scheduler** - when to reach out, on which channel, respecting quiet hours and fatigue
- [ ] **Phase 7: Web Application** - onboarding, consent capture, preferences, and the timeline browser
- [ ] **Phase 8: Real-Time Voice Agent** - live streaming conversation with bounded in-session follow-up
- [ ] **Phase 9: Prediction and Synthesis** - pattern mining into retrospective and forward-looking questions
- [ ] **Phase 10: Safety, Privacy, and Deployment** - crisis path, egress guard, data rights, and the box

## Phase Details

### Phase 1: Tenant Foundation

**Goal**: A running Go service with a multi-tenant Postgres schema where cross-tenant data access is impossible at the database level, and a subject can create an account.
**Depends on**: Nothing (first phase)
**Success Criteria** (what must be TRUE):
  1. `make run` starts the API and `/healthz` reports database connectivity.
  2. A test proves that a query issued in subject A's session cannot read subject B's rows, even with a deliberately unscoped query.
  3. A person can sign up with an email and password and receive a session.
  4. Migrations run forward and roll back cleanly from an empty database.

Plans:
- [ ] 01-01: Go module scaffold with chi router, layered config, structured logging, /healthz, Makefile, and CI
- [ ] 01-02: Postgres 16 schema v1 and a forward/rollback migrations runner: subjects, identities, consent_records, preferences
- [ ] 01-03: Row-level security policies plus a tenant-scoped repository layer, with cross-tenant isolation tests
- [ ] 01-04: Subject signup, login, and session management with argon2id password hashing

### Phase 2: Timeline and Coverage Grid

**Goal**: A life event can be stored at any temporal precision from decade to minute, linked to the people and places it involves, and the system can report exactly which regions of a life it knows nothing about.
**Depends on**: Phase 1
**Success Criteria** (what must be TRUE):
  1. An event recorded as "sometime in 1994" and one recorded as "1994-06-12 14:30" both persist without loss, each carrying its precision and confidence.
  2. Inserting an event raises the density of every covering grid cell, incrementally, without a full recompute.
  3. A coverage query returns the ten emptiest cells for a subject with their scores.
  4. Two mentions of the same person under different names can be merged, and the merge is reversible.

Plans:
- [ ] 02-01: timeline_events schema carrying temporal precision from decade to minute, plus confidence and provenance
- [ ] 02-02: entities for people, places, and organizations, with event links, deduplication, and reversible merge
- [ ] 02-03: coverage grid over time bucket by life domain, with incremental density recomputation on event insert
- [ ] 02-04: timeline query API over range, domain, entity, and full text, plus the coverage heatmap endpoint

### Phase 3: Deterministic Interview Planner

**Goal**: Given a subject, the system produces the next best question to ask, together with the specific gap in that subject's life that motivated it, using no model call to make the decision.
**Depends on**: Phase 2
**Success Criteria** (what must be TRUE):
  1. Cell scoring is a pure function with table-driven tests covering gap size, recoverability decay, salience adjacency, and fatigue.
  2. Every question in the queue names the cell it targets.
  3. Asking the same subject twice never produces a semantic near-duplicate of a prior question.
  4. The generated question text is schema-validated before it enters the queue, and a malformed generation is rejected rather than repaired.

Plans:
- [ ] 03-01: life-domain taxonomy with sensitivity tiers, seeded and subject-mutable
- [ ] 03-02: pure cell-scoring function over gap size, recoverability decay, salience adjacency, and per-domain fatigue
- [ ] 03-03: question generation on the local model from a target cell, schema-validated, with semantic deduplication
- [ ] 03-04: priority question queue plus the subject-visible view of everything queued for them

### Phase 4: Extraction and Open Loops

**Goal**: A free-text answer becomes structured timeline events without human help, and any unresolved situation it mentions is tracked until it is resolved or the subject closes it.
**Depends on**: Phase 3
**Success Criteria** (what must be TRUE):
  1. A paragraph mentioning three events at differing precisions yields three events with correct ranges and precisions.
  2. "The summer after I graduated" resolves to an absolute range when the graduation date is known, and is stored as low-confidence when it is not.
  3. An answer stating that a parent went into hospital opens an open loop in a non-terminal state.
  4. The sweeper re-probes that loop on a decaying schedule, and stops permanently the moment the subject mutes it.

Plans:
- [ ] 04-01: answer extraction into events, entities, and temporal expressions on the local model, schema-validated
- [ ] 04-02: temporal resolution of relative expressions into absolute ranges with explicit precision and confidence
- [ ] 04-03: open_loops with a resolution state machine, detection from extraction output, and related-event linking
- [ ] 04-04: open-loop sweeper on a decaying re-probe schedule, honoring per-loop and per-domain mutes at schedule time

### Phase 5: Email and SMS Channels

**Goal**: The system can hold a two-way conversation with a subject over email and SMS, and cannot send anything on a channel the subject has not consented to.
**Depends on**: Phase 4
**Success Criteria** (what must be TRUE):
  1. A question sent by email and answered by reply is correctly correlated back to its thread and question.
  2. An inbound SMS reply reaches the extraction pipeline and updates the timeline.
  3. Texting STOP halts all SMS immediately and is reflected in consent_records; START restores it.
  4. Attempting to send on a channel with no valid consent record fails closed, with a test proving it.

Plans:
- [ ] 05-01: the Channel interface over Send, Receive, and Capabilities, designed voice-ready from the start
- [ ] 05-02: email adapter with SMTP send, plus-addressed inbound routing, and thread correlation
- [ ] 05-03: SMS adapter on Twilio with A2P 10DLC registration and STOP, HELP, and START handling
- [ ] 05-04: consent gate that fails closed on every send path, with immediate revocation including queued sends

### Phase 6: Contact Scheduler

**Goal**: The system decides when to contact a subject and on which channel, respecting their cadence, timezone, quiet hours, and their demonstrated tolerance for being asked.
**Depends on**: Phase 5
**Success Criteria** (what must be TRUE):
  1. No contact is ever dispatched inside a subject's quiet hours, across timezone and daylight-saving boundaries.
  2. A high-sensitivity question is never routed to SMS unless the subject explicitly opted in.
  3. Sustained non-response decays contact cadence automatically rather than continuing at a fixed rate.
  4. A worker killed mid-job leaves no duplicate sends when it restarts.

Plans:
- [ ] 06-01: contact scheduler over subject cadence, timezone-aware quiet hours, and per-channel rate limits
- [ ] 06-02: channel selection over question sensitivity tier, channel fit, and subject preference
- [ ] 06-03: response-rate tracking with automatic cadence decay and per-domain fatigue feedback into scoring
- [ ] 06-04: worker runtime with an idempotent job queue, retry with jitter, and execution observability

### Phase 7: Web Application

**Goal**: A subject can sign up, verify their identity on each channel, grant consent, tune how the system talks to them, and explore and correct the archive of their own life.
**Depends on**: Phase 6
**Success Criteria** (what must be TRUE):
  1. Onboarding verifies both an email address and a phone number before any consent can be granted.
  2. The preferences screen can mute a life domain and an individual open loop, and the scheduler honors both.
  3. The timeline browser zooms from decade to hour and renders the coverage heatmap over the same axes.
  4. The subject can edit, redact, or delete any event, and redaction is visible as a gap rather than silently rewriting history.

Plans:
- [ ] 07-01: React and TypeScript scaffold on Vite with the authentication flow and session handling
- [ ] 07-02: onboarding wizard covering email and phone verification, per-channel consent capture, and profile seeding
- [ ] 07-03: preferences screens for channels, cadence, quiet hours, domain mutes, and per-loop mutes
- [ ] 07-04: timeline browser with decade-to-hour zoom, the coverage heatmap, and event edit, redact, and delete

### Phase 8: Real-Time Voice Agent

**Goal**: The system calls a subject and holds a genuine conversation, pursuing a planner-chosen target with real in-session follow-up, and it does so only where recording consent is lawfully established.
**Depends on**: Phase 7
**Success Criteria** (what must be TRUE):
  1. A call streams audio both ways, and the subject can interrupt mid-sentence with the agent yielding.
  2. Median end-of-speech to first audio frame stays within the 800 ms budget, and the figure is measured in CI rather than asserted.
  3. The in-session loop pursues its target across multiple turns and stops at its turn budget rather than running unbounded.
  4. No call is placed to a two-party-consent jurisdiction without stored consent and a recorded disclosure at call start.

Plans:
- [ ] 08-01: Twilio Voice and Media Streams WebSocket bridge over mu-law 8k with full call lifecycle handling
- [ ] 08-02: local streaming STT on faster-whisper and local TTS on Piper or Kokoro, with the latency budget instrumented
- [ ] 08-03: bounded in-session agentic loop with a turn budget and timeline retrieval, pursuing the planner target
- [ ] 08-04: recording consent with call-start disclosure, two-party-consent jurisdiction handling, and consent stored before dial

### Phase 9: Prediction and Synthesis

**Goal**: The system stops merely filling holes and starts anticipating, generating questions from the patterns of a life rather than from its gaps alone.
**Depends on**: Phase 8
**Success Criteria** (what must be TRUE):
  1. Pattern mining surfaces recurring cycles and life-change cadence from a timeline, with the supporting events cited.
  2. A retrospective analogy question references a real observed pattern and a real gap, both traceable.
  3. Forward-looking questions are grounded in observed periodicity and never invent an event that was not extrapolated.
  4. Marking a prediction wrong measurably changes subsequent cell scoring for that subject.

Plans:
- [ ] 09-01: pattern mining over the timeline for recurring cycles, arcs, and the cadence of life changes
- [ ] 09-02: retrospective analogy questions targeting gaps by observed pattern, with citation of supporting events
- [ ] 09-03: forward-looking and aspiration questions grounded in observed periodicity
- [ ] 09-04: prediction confidence scoring and the subject feedback loop back into cell scoring

### Phase 10: Safety, Privacy, and Deployment

**Goal**: The system is safe to point at a real person's entire life, provably keeps that life on the box, and runs in production alongside gophermind.
**Depends on**: Phase 9
**Success Criteria** (what must be TRUE):
  1. A simulated self-harm disclosure halts the session, surfaces resources on that channel, and writes an audit record, on every channel including mid-call.
  2. A build fails when subject-derived text is passed to any external client, demonstrated by a deliberately introduced violation.
  3. A subject can export everything, redact a single event, and hard-delete their account with full cascade, all audit-logged.
  4. A deploy to the target box is reversible, and a failed migration blocks the release rather than half-applying.

Plans:
- [ ] 10-01: crisis path with a disclosure classifier, per-channel response, session halt, resource surfacing, and audit
- [ ] 10-02: egress guard enforcing local-only inference, failing the build on subject-derived text reaching any external client
- [ ] 10-03: data rights covering full export, per-event redaction, and hard delete with cascade and audit logging
- [ ] 10-04: deployment to 10.0.0.5 with systemd units, a migration gate, healthchecks, rollback, and backups

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Tenant Foundation | 0/4 | Not started | - |
| 2. Timeline and Coverage Grid | 0/4 | Not started | - |
| 3. Deterministic Interview Planner | 0/4 | Not started | - |
| 4. Extraction and Open Loops | 0/4 | Not started | - |
| 5. Email and SMS Channels | 0/4 | Not started | - |
| 6. Contact Scheduler | 0/4 | Not started | - |
| 7. Web Application | 0/4 | Not started | - |
| 8. Real-Time Voice Agent | 0/4 | Not started | - |
| 9. Prediction and Synthesis | 0/4 | Not started | - |
| 10. Safety, Privacy, and Deployment | 0/4 | Not started | - |
