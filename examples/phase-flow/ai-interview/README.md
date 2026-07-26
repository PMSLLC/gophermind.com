# Example: a complete PhaseFlow project spec

This is a worked example of a fully-specified PhaseFlow project — the state
[`internal/phaseflow`](../../../internal/phaseflow) expects a project to be in
*before* `gophermind project-execute` will run it.

The subject is **AI Interview**: a system that interviews a person across email,
SMS, and live phone calls until their whole life is recorded. It was chosen as
the example because it is genuinely hard to scope — "interview someone about
every aspect of their life, hour by hour" is an infinite ask — and so it exercises
the parts of PhaseFlow that a toy project never touches.

The live project lives at `~/OtherProjects/AI-Interview`. This copy is
documentation and does not execute.

## What's here

| File | Role |
|---|---|
| [`.planning/PROJECT.md`](.planning/PROJECT.md) | Core value, 25 numbered requirements, constraints, non-goals, decision log |
| [`.planning/ROADMAP.md`](.planning/ROADMAP.md) | 10 phases × 4 plans, each phase with goal, dependencies, and success criteria |
| [`.planning/assignments.json`](.planning/assignments.json) | The machine-readable plan — 40 tasks with acceptance criteria, agent, model tier, and per-task addendum |
| [`.planning/STATE.md`](.planning/STATE.md) | Living memory: position, accumulated decisions, live blockers |
| [`.planning/config.json`](.planning/config.json) | Workflow gates and granularity |
| [`PROJECT-README.md`](PROJECT-README.md) | The project's own README, for context on what is being built |

Not included: `.planning/agents/`. The 32-agent catalog is seeded from the
embedded upstream prompts by `SeedCatalog`, so it is generated, not authored.
Run `gophermind project` in a real project and it appears.

## What this example demonstrates

**It validates.** `Engine.ValidatePlan()` returns `Complete: true` — 10 phases,
40 tasks, zero issues. Every roadmap plan id has a matching assignment; every
assignment carries acceptance criteria, a catalog agent that actually exists, and
a model tier. No template placeholder or `TBD` survives anywhere, which is what
the validator's placeholder scan enforces.

**Success criteria are observable, not aspirational.** PhaseFlow asks for "what
must be TRUE". Compare:

> A query issued in subject A's session returns zero rows from subject B,
> including when the `WHERE` clause omits `subject_id` entirely.

against the same idea written badly ("multi-tenancy works"). The first is a test
somebody can write without asking a follow-up question. Every criterion in this
roadmap is meant to pass that bar.

**Acceptance criteria carry the design decisions.** Task `02-03` does not just
say "build the coverage grid" — its fourth criterion requires that cells only
subdivide once their parent justifies it, and the addendum explains why:

> Uniform hour-level cells across eighty years is roughly 700,000 cells per
> subject, nearly all permanently empty, and the density signal drowns.

An executing agent reading only that task still gets the reasoning that would
otherwise live in someone's head.

**`agent_addendum` is where the judgment goes.** The field exists so a task can
carry the one thing the generic catalog prompt cannot know. Some of the load-bearing
uses here:

- `04-04` — why "follow up until resolved" must be able to give up, and why
  *closed-by-subject* is a different terminal state from *resolved*
- `06-02` — why the sensitivity floor is not a user preference (SMS is readable on
  a lock screen and on shared family devices)
- `10-02` — why crash reporters are the leak that defeats a local-only guarantee
  while every deliberate code path stays clean
- `08-02` — explicit permission to report failure and trigger the documented
  fallback rather than letting the riskiest phase slip indefinitely

**Phases are ordered by dependency and by risk.** Each is independently
shippable: phases 1–7 are a complete product on the deterministic engine before
the high-risk real-time voice work in phase 8 begins. A phase that cannot be
demonstrated as working behavior is scoped wrong.

**`STATE.md` names blockers with decision points.** The voice latency risk names
task `08-02` as the moment to decide, and names the fallback. The A2P 10DLC lead
time is flagged in Phase 1 even though it gates Phase 5, because that is when
filing has to start.

## Using it as a template

```sh
mkdir my-project && cd my-project
cp -r path/to/examples/phase-flow/ai-interview/.planning .
# rewrite PROJECT.md, ROADMAP.md, and assignments.json for your project
gophermind project          # validates, then approve
gophermind project-execute  # runs every pending task in plan-id order
```

Keep the structure and the standard of specificity; replace all of the content.
The validator will catch leftover placeholders, missing acceptance criteria,
unknown agent names, and plans with no assignment — but it cannot tell you that a
success criterion is vague. That part is on you.
