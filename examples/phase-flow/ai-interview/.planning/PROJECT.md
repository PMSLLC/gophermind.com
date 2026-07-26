# Project: AI Interview

## Core Value

A person is interviewed, continuously and for years, across email, SMS, and live
phone calls — until their entire life is recorded at the finest resolution they
are willing to give: birth to today to where they are going, down to the hour.

## The Framing Problem

"Interview someone about every aspect of their life, hour by hour" is an infinite
ask. It becomes tractable only if the system holds an explicit model of **what it
does not yet know**.

That model is the **coverage grid**: a multi-resolution matrix of
`(time bucket × life domain)` cells, each carrying a density score. Time buckets
nest `decade → year → month → day → hour`. Hour-level detail is therefore a
*depth the grid can reach where it matters*, not a quota to fill everywhere. A
Tuesday in 1994 and the Tuesday a marriage ended are the same cell type at
different depths, and the scheduler decides which is worth an hour of someone's
attention.

Everything else in this system exists to serve that grid: channels carry
questions to the subject, extraction turns answers into events, events raise cell
density, and the planner reads the remaining holes to decide what to ask next.

## Architecture Decision: C-built-as-A

Three approaches were considered for the interview engine:

| | Approach | Verdict |
|---|---|---|
| A | Deterministic Go planner; the model only generates questions and extracts answers, never decides | **Built first** |
| B | Agentic interviewer holding the plan, driving itself with tools | Rejected — non-deterministic, unprovable coverage, drifts and repeats, too costly per turn on a local 35B |
| C | Deterministic planner picks the target; a bounded agentic loop runs the session | **Target architecture** |

**Decision: build C, but ship A first.** The planner, the coverage grid, and the
extraction pipeline are the load-bearing parts and the parts that can be tested.
Phases 1–7 deliver a complete working system on pure A (email + SMS). The
in-session agentic loop arrives in Phase 8 alongside voice, where conversational
follow-up actually pays for its non-determinism.

The invariant that makes this testable: **the model never controls flow.** It
emits data — question text, extraction JSON — validated against a schema before
anything acts on it. Scheduling, channel selection, consent checks, and sweeps
are plain Go with table-driven tests.

## Requirements

### Interview engine

- [ ] REQ-01: A multi-resolution coverage grid over `(time bucket × life domain)`
      with a density score per cell, recomputed incrementally as events land.
- [ ] REQ-02: A pure, deterministic cell-scoring function combining gap size,
      memory-recoverability decay, salience adjacency to known high-emotion
      events, and per-domain subject fatigue.
- [ ] REQ-03: Every generated question records the target cell that motivated it.
      A question with no traceable target is a bug.
- [ ] REQ-04: Questions are deduplicated against everything previously asked of
      that subject, including semantic near-duplicates.
- [ ] REQ-05: Free-text answers are extracted into timeline events, entities, and
      temporal expressions, schema-validated before persistence.
- [ ] REQ-06: Relative temporal expressions ("last Tuesday", "the summer after
      graduation") resolve to absolute ranges carrying an explicit precision and
      confidence.

### Multiday events and follow-up

- [ ] REQ-07: Answers mentioning an unresolved situation open an **open loop**
      with a resolution state machine and links to related events.
- [ ] REQ-08: A sweeper re-probes open loops on a decaying schedule until the
      loop reaches a terminal state or the subject closes it.
- [ ] REQ-09: The sweeper checks per-loop and per-domain mutes **before**
      scheduling. See REQ-19.

### Prediction

- [ ] REQ-10: Pattern mining over the timeline surfaces recurring cycles, arcs,
      and the observed cadence of life changes.
- [ ] REQ-11: Retrospective analogy questions target gaps by pattern ("you did
      this every summer — what about the summer of 1998?").
- [ ] REQ-12: Forward-looking questions are grounded in observed periodicity, not
      invented.
- [ ] REQ-13: The subject can mark a prediction right or wrong; that signal feeds
      back into cell scoring.

### Channels

- [ ] REQ-14: One `Channel` interface (`Send` / `Receive` / `Capabilities`)
      covering email, SMS, and voice. The engine never branches on channel type.
- [ ] REQ-15: Channel selection combines question sensitivity tier, channel fit,
      and subject preference. High-sensitivity questions never route to SMS
      unless the subject explicitly opted in.
- [ ] REQ-16: Live phone calls run a real-time loop: streaming STT → target-driven
      follow-up → TTS, with barge-in and a turn budget.

### Consent, safety, and control

- [ ] REQ-17: No outbound contact on any channel without a current, valid
      `consent_record` for that subject **and** that channel. Revocation takes
      effect immediately, including for already-queued sends.
- [ ] REQ-18: The subject can see every question queued for them before it is
      asked.
- [ ] REQ-19: The subject can mute any open loop or any life domain permanently.
      A mute is honored at schedule time, not at send time.
- [ ] REQ-20: A defined crisis path exists: a disclosure classifier, a specified
      response per channel, session halt, resource surfacing, and an audit
      record. Behavior is decided now, not during the incident.
- [ ] REQ-21: Full data export, per-event redaction, and hard delete with
      cascade, all audit-logged.
- [ ] REQ-22: Call recording requires a recorded disclosure at call start, with
      two-party-consent jurisdictions handled explicitly, and consent stored
      before the first dial.

### Tenancy and inference

- [ ] REQ-23: Multi-tenant from day one. Every query is scoped by `subject_id`,
      enforced by Postgres row-level security, with tests proving cross-tenant
      isolation.
- [ ] REQ-24: **Local-only inference.** Question generation, extraction,
      prediction, STT, and TTS all run on the local box. No subject-derived text
      appears in any outbound request body.
- [ ] REQ-25: An automated egress guard fails the build if subject-derived text
      can reach an external client. External credentials exist only for message
      *transport* (Twilio, SMTP) and carry no life content beyond the message
      being delivered.

## Constraints

- **Deploy target:** `10.0.0.5`, the box already running `gophermind serve`
  on `:8090`. AI Interview adds `:8091` (API) and `:8092` (voice media
  WebSocket), its own Postgres 16, its own systemd units, and a reverse-proxy
  vhost for TLS and webhook ingress.
- **Inference:** the local `qwen3.6-35b` behind gophermind's OpenAI-compatible
  endpoint. STT is `faster-whisper`; TTS is Piper or Kokoro. All on-box.
- **Voice latency budget:** ~800 ms target turn latency, measured end to end from
  end-of-speech to first TTS audio frame. This is the hardest number in the
  project and it constrains model choice for in-session turns.
- **Stack:** Go (chi) API + worker, React + TypeScript (Vite) web, Postgres 16.
- **SMS:** A2P 10DLC brand and campaign registration required before production
  sending. `STOP` / `HELP` / `START` handling is table stakes, not a feature.
- **Quiet hours** are per-subject and timezone-aware. The scheduler treats them
  as hard bounds.

## Non-Goals

- No mobile app. The web app is responsive; contact happens through email, SMS,
  and phone, which every phone already has.
- No social features, sharing, or multi-subject cross-linking. One archive, one
  owner.
- No billing in this roadmap. Multi-tenancy is structural; monetization is a
  later, separate project.
- No frontier-model fallback. Local-only is a product commitment, not a
  cost optimization (REQ-24).

## On "nothing is taboo"

The system asks about sex, crime, substances, trauma, money, faith, and death,
because a life archive that omits them is not a life archive. That is a
deliberate content stance and it is the product.

It is not a consent stance. What separates this from surveillance is entirely
structural, and the structure is specified above: the subject opted in (REQ-17),
can see every question before it is asked (REQ-18), can permanently mute any
loop or domain (REQ-19), and can redact or delete anything already recorded
(REQ-21). "Follow up until resolved" is the right behavior for *did you get the
job?* and precisely the wrong behavior for *did your mother recover?* when she
did not. REQ-09 is what makes the difference, which is why it is a scheduler
invariant rather than a setting.

## Key Decisions

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-07-26 | Go API + worker, React TS web, Postgres 16 | Matches the language table for backend APIs; single static binary deploys cleanly next to gophermind |
| 2026-07-26 | Multi-tenant from day one | Productization is planned; retrofitting `subject_id` scoping and RLS across a life-timeline schema is far worse later |
| 2026-07-26 | Real-time AI voice agent, not IVR | Voice is the only channel that can chase a multiday event to resolution in one sitting; highest information yield per contact |
| 2026-07-26 | Local-only inference, hard requirement | The data is the most sensitive a person has, and hosted content policies directly contradict "nothing is taboo". Also a genuine differentiator |
| 2026-07-26 | Engine approach C, built as A first | Deterministic planner gives provable coverage and testability; the agentic layer is added where conversation pays for non-determinism |
| 2026-07-26 | Coverage grid as the core abstraction | Turns an infinite ask into a scored, finite, inspectable work queue — and renders directly as the timeline heatmap UI |
| 2026-07-26 | Mutes honored at schedule time, not send time | A muted loop should never occupy queue space or influence scoring; checking at send time leaves the question visible in the subject's queue |
