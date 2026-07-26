# AI Interview

A system that interviews a person — continuously, for years, across email, SMS,
and live phone calls — until their entire life is recorded at the finest
resolution they are willing to give. Birth to today to where they are going,
down to the hour.

Nothing is off the table by design. What makes that a life archive rather than
surveillance is structural: the subject opted in, sees every question before it
is asked, can permanently mute any topic or any line of follow-up, and can redact
or delete anything already recorded.

**Status:** planned, not yet implemented. The full spec and roadmap live in
[`.planning/`](.planning/).

## The idea

"Interview someone about every aspect of their life, hour by hour" is an infinite
ask. It becomes tractable only if the system holds an explicit model of what it
*does not yet know*.

That model is the **coverage grid** — a multi-resolution matrix of
`(time bucket × life domain)` cells, each with a density score, nesting
`decade → year → month → day → hour`. Hour-level detail is a depth the grid
reaches where it matters, not a quota to fill everywhere. A deterministic planner
reads the holes, scores them, and decides what to ask next. The local model
writes the question and extracts the answer; it never decides.

Answers that mention an unfinished situation open an **open loop**, re-probed on
a decaying schedule until it resolves — or until the subject mutes it, which the
scheduler honors before it ever queues a question.

## Architecture

| | |
|---|---|
| **Backend** | Go (chi) API + worker |
| **Web** | React + TypeScript (Vite) |
| **Database** | Postgres 16, multi-tenant with row-level security |
| **Inference** | Local `qwen3.6-35b` — no subject content ever leaves the box |
| **Voice** | Twilio Media Streams, local `faster-whisper` STT, local Piper/Kokoro TTS |
| **Deploy** | `10.0.0.5`, alongside `gophermind serve` |

Inference being local is a hard requirement, not a default. Hosted content
policies directly contradict the premise, and the data is the most sensitive a
person has. An automated egress guard fails the build if subject-derived text can
reach any external client.

## Roadmap

Ten independently shippable phases. Phases 1–7 deliver a complete working product
on the deterministic engine over email and SMS; Phase 8 adds the real-time voice
agent and in-session follow-up.

| Phase | Delivers |
|---|---|
| 1 | Tenant foundation — schema, row-level security, auth |
| 2 | Timeline store and the coverage grid |
| 3 | Deterministic interview planner |
| 4 | Extraction and open loops |
| 5 | Email and SMS channels |
| 6 | Contact scheduler |
| 7 | Web application and timeline browser |
| 8 | Real-time voice agent |
| 9 | Prediction and synthesis |
| 10 | Safety, privacy, and deployment |

Full detail, success criteria, and per-task acceptance criteria are in
[`.planning/ROADMAP.md`](.planning/ROADMAP.md) and
[`.planning/assignments.json`](.planning/assignments.json).

## Running the workflow

This project is driven by [PhaseFlow](https://github.com/jbrahy/gophermind),
built into gophermind. The `.planning/` tree is the workflow state — any session
resumes from where the last one stopped.

```sh
gophermind project            # review the plan, then approve
gophermind project-execute    # run every pending task in plan-id order
```

Each task runs in a fresh isolated agent with its assigned model and catalog
prompt, and verifies its own output against that task's acceptance criteria.

## Highest-risk items

Recorded in [`.planning/STATE.md`](.planning/STATE.md) and worth knowing up front:

- **Voice latency (Phase 8).** An 800 ms end-to-end turn budget with local STT, a
  35B model turn, and local TTS on a box already serving gophermind is tight. The
  decision point is task `08-02`; the fallback is scripted IVR with async
  transcription, which costs in-session follow-up and leaves every other phase
  intact.
- **A2P 10DLC registration (Phase 5).** Multi-week external lead time, outside our
  control. File during Phase 1 so it does not gate Phase 5.
- **The crisis path (task `10-01`).** Specified last but must be *decided* before
  any real subject is interviewed. If a live subject is onboarded ahead of
  Phase 10, pull `10-01` forward as an inserted phase.
