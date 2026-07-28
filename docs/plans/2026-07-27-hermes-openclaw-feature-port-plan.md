# Hermes & OpenClaw — feature analysis and gophermind port plan

**Date:** 2026-07-27
**Scope:** feature-by-feature comparison of Hermes Agent (Nous Research, v0.19.0)
and OpenClaw (OpenClaw Foundation) against gophermind, then a phased plan to
bring the worthwhile capabilities into gophermind.

## Method and confidence

Both products post-date this analysis's training data, so everything here comes
from primary sources fetched on 2026-07-27: the Hermes GitHub repo and release
notes, the OpenClaw documentation site and CLI reference, the
`openclaw-code-agent` plugin repo, and one third-party comparison. Release-note
detail for Hermes v0.16.0 and v0.15.0 was truncated in the fetched excerpt and is
marked where it matters. gophermind's side is ground truth — read from this
repository at commit `1c7a07d`+merge.

Where a claim comes from a vendor's own marketing (notably the Hermes-vs-OpenClaw
comparison, which is published by a Hermes-friendly outlet), it is flagged rather
than repeated as fact.

## What each product actually is

**Hermes Agent** — Nous Research's open-source autonomous agent, first released
February 2026, now at v0.19.0 (20 July 2026). Its thesis is *Harness
Engineering*: the LLM is replaceable, and the value sits in the instruction,
constraint, feedback, memory, and orchestration layers around it. That is
precisely gophermind's thesis, which makes Hermes the closer competitor of the
two and the more relevant source of ideas.

**OpenClaw** — Peter Steinberger's self-hosted personal AI assistant, MIT
licensed under a non-profit foundation, reportedly the most-starred repo on
GitHub within five months. Its thesis is a *single persistent Gateway process*
that owns every messaging surface on a host and routes them to agents. It is a
personal-assistant platform first; coding is delivered through a plugin
(`openclaw-code-agent`), not through the core.

**gophermind** — a minimal agentic coding harness: TUI plus CLI, spec-driven
project orchestration (PhaseFlow), RAG over the repo, MCP both directions, an
audit-logged safety layer, and a webhook/SSE server with an iOS client.

The strategic read: **Hermes is converging on gophermind's territory from the
agent side; OpenClaw is adjacent and mostly off-mission.** Most of what is worth
taking comes from Hermes.

## Feature-by-feature matrix

Legend: ● full · ◐ partial · ○ absent

### Memory and learning

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Persistent cross-session memory | ● | ◐ | ◐ | gophermind has RAG + `internal/embed`; no durable user model |
| Bounded working memory (`MEMORY.md` ~2.2k chars, `USER.md` ~1.4k) | ● | ◐ | ○ | Hermes bounds deliberately to prevent context bloat |
| Session full-text search + LLM summarization recall | ● | ○ | ◐ | gophermind has `sessions search`, no summarizing recall |
| Dialectic user modeling (Honcho) | ● | ○ | ○ | builds a deepening model of the user across sessions |
| Memory graph visualization / `/journey` timeline | ● | ○ | ○ | desktop radial timeline of memories and skills |
| Atomic batch memory ops | ● | ○ | ○ | v0.17.0 `operations` array |

### Skills and self-improvement

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Human-authored skill packs | ● | ● | ● | gophermind: `.gophermind/skills/*.md` → system prompt |
| **Autonomous skill creation from experience** | ● | ○ | ○ | the "5-tool-call rule"; auto-generates after repeated patterns |
| Skills self-improve during use | ● | ○ | ○ | closed learning loop, post-task refinement |
| `/learn` — distill a workflow into a skill | ● | ○ | ○ | v0.18.0 |
| Skill hub / marketplace | ● 643+ | ● | ○ | Hermes has 4 trust tiers + security scanning |
| Open skill standard (agentskills.io) | ● | ◐ | ○ | interop matters more than the hub itself |

### Orchestration and coding

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Spec-driven phase/plan workflow | ○ | ○ | ● | **gophermind leads** — PhaseFlow is unmatched by either |
| Per-task agent + model assignment | ○ | ◐ | ● | **gophermind leads** — `assignments.json` |
| Isolated subagents, parallel | ● | ◐ | ● | gophermind: `spawn`, `queue --fleet N`, `jobs.RunConcurrent` |
| Background fan-out delegation | ● | ● | ◐ | Hermes `delegate_task(background=true)` |
| Live subagent transcripts / watch windows | ● | ○ | ○ | v0.17.0/v0.19.0, durable delivery ledger |
| Git worktree coding sessions w/ lifecycle | ○ | ● | ○ | OpenClaw: active→pending→pr_open→merged→released→dismissed |
| Plan→review→execute permission modes | ◐ | ● | ◐ | OpenClaw: `plan`/`ask`/`delegate` × `off`/`manual`/`auto-merge`/`auto-pr` |
| PR/merge follow-through from chat | ○ | ● | ○ | two-step contract, routed summary back to thread |
| Goal loops with evidence verification | ● | ● | ◐ | gophermind has `/goal`; Hermes adds *completion contracts* |
| Mixture-of-Agents deliberation | ● | ○ | ◐ | gophermind has `ab` testing, not live multi-model deliberation |
| Python/RPC scripting over tools | ● | ○ | ○ | collapses multi-step pipelines into one script |

### Execution environment

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Container-isolated shell | ● | ● | ◐ | gophermind: `GOPHERMIND_SHELL_CONTAINER` |
| Pluggable terminal backends | ● | ◐ | ○ | Hermes: local, Docker, SSH, Singularity, Modal, Daytona |
| Managed sandbox lifecycle | ◐ | ● | ○ | OpenClaw `sandbox` CLI |
| Smart approvals (LLM reviews the command) | ● | ○ | ○ | v0.19.0 — replaces manual approval burden |
| Secret manager integration | ● | ○ | ○ | 1Password + Bitwarden via pluggable `SecretSource` |
| Browser control | ● | ● | ○ | Hermes cloud browser; OpenClaw dedicated Chrome |

### Interfaces and reach

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| TUI | ● | ◐ | ● | comparable |
| Messaging channels | ● 14+ | ● 8+ | ○ | Telegram, Discord, Slack, WhatsApp, Signal, iMessage… |
| Single gateway owning all surfaces | ● | ● | ◐ | gophermind `serve` is webhook/SSE, not multi-channel |
| Mobile client | ◐ | ● | ● | gophermind has a real iOS app + QR pairing |
| Device nodes (camera, canvas, location) | ○ | ● | ○ | OpenClaw-specific, personal-assistant territory |
| Web control dashboard | ● | ● | ○ | |
| Cron scheduling → any channel | ● | ● | ○ | daily reports, nightly audits |
| Automation Blueprints (no cron syntax) | ● | ○ | ○ | v0.17.0 |
| MCP client | ● | ● | ● | parity |
| MCP/tool server | ◐ | ◐ | ● | gophermind exposes itself as an MCP server |
| ACP / IDE bridge | ○ | ● | ◐ | gophermind has LSP + editor integrations |

### Models and operations

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Provider breadth | ● 400+ | ◐ | ● | gophermind: any OpenAI-compatible + profiles |
| Local backends (Ollama/vLLM/llama.cpp) | ● | ◐ | ● | parity — gophermind runs on local vLLM today |
| Reasoning effort tiers | ● | ◐ | ◐ | gophermind `-think low\|medium\|high`; Hermes adds max/ultra |
| Per-session model/mode picker | ◐ | ◐ | ● | **gophermind leads** — shipped 2026-07-22 |
| Cost tracking per session | ● | ● | ◐ | gophermind `usage report`; OpenClaw shows USD inline |
| Session export (MD/HTML/Quarto/HF traces) | ● | ○ | ◐ | gophermind exports JSONL + redaction |
| Trajectory generation for training | ● | ○ | ◐ | gophermind has `golden`, `benchmark`, `tuning` |
| Tamper-evident audit log | ○ | ○ | ● | **gophermind leads** — `audit verify` chain |
| Prompt-injection defense | ◐ | ◐ | ● | **gophermind leads** — `internal/safety/injection.go` |
| Scale-to-zero gateway | ● | ◐ | ○ | v0.18.0, drain coordination |

## Where gophermind already wins

Worth stating plainly, because it should shape what gets ported and what gets
left alone:

1. **PhaseFlow.** Neither competitor has a spec-driven roadmap→phase→plan→execute
   →verify→milestone loop with per-task agent/model assignment and machine-checked
   plan validation. This is gophermind's genuine differentiator.
2. **Tamper-evident audit log** with chain verification — neither has it.
3. **Prompt-injection defense** as a first-class subsystem.
4. **MCP server** direction — gophermind exposes itself, not just consumes.
5. **Per-session model/mode picker**, shipped and deployed to iOS.

The port plan below is deliberately additive: nothing in it trades away these.

## Strategic filter — what NOT to build

The request was "all the features." Delivering that honestly means saying which
ones are wrong for this product:

- **WhatsApp / Signal / iMessage / Discord channels.** These serve OpenClaw's
  personal-assistant mission. gophermind is a coding harness; a coding agent
  reachable from Signal is a novelty, not a workflow. **Recommend: Telegram and
  Slack only** — the two where "the build finished, here's the PR" is genuinely
  useful. Skip the rest.
- **Device nodes (camera, canvas, location).** Entirely off-mission.
- **Honcho dialectic user modeling.** Deep personal user modeling is valuable for
  an assistant that knows *you*; a coding harness benefits far more from modeling
  *the repository*. gophermind already does that with RAG and `codeindex`.
- **Skill marketplace/hub.** The hub is a community-scale asset, not a feature you
  build; adopting the **agentskills.io open standard** captures most of the value
  at a fraction of the cost.
- **Web control dashboard.** Real cost, and gophermind already has TUI + CLI +
  iOS. Revisit only if multi-channel lands and needs a config surface.

Everything below excludes these.

## The port plan

Ten phases, ordered by value-per-unit-effort and by dependency. Phases 1–4 are
the ones that change what gophermind *is*; 5–10 broaden reach and polish.

### Phase 1 — Self-improving skills (the highest-value gap)

gophermind already loads `.gophermind/skills/*.md` into the system prompt. It has
the *substrate* and none of the *loop*. Hermes's learning loop is the single
biggest capability difference between the two products.

- **1-1** Skill provenance and metadata: frontmatter (triggers, required context,
  origin, usage count, last-refined) on existing skill files, backward compatible
  with today's plain-markdown packs.
- **1-2** Pattern detection: instrument the agent loop to spot repeated tool-call
  sequences and user corrections. Hermes's heuristic is the "5-tool-call rule" —
  implement it as a tunable threshold, not a constant.
- **1-3** Autonomous skill drafting: on threshold, draft a skill from the session
  trajectory. **Write to `.gophermind/skills/proposed/` and require acceptance** —
  auto-writing into the live prompt path is how a harness silently poisons itself.
- **1-4** `/learn` command: distill the current session into a skill on demand.
- **1-5** Skill refinement: when a skill is used and the task fails, record the
  failure against it; refine on repeat. Bound refinement so a skill cannot grow
  without limit.
- **1-6** agentskills.io standard compliance for import/export.

*Why first:* it compounds. Every subsequent phase generates trajectories that make
the skills better.

### Phase 2 — Bounded memory and recall

gophermind's RAG retrieves from the repo; it has no durable model of the work.

- **2-1** `MEMORY.md` bounded working context with an explicit character budget and
  a documented eviction policy. Hermes bounds at ~2,200 chars deliberately —
  adopt the *principle*, tune the number against gophermind's context budget.
- **2-2** `PROJECT-FACTS.md` — gophermind's analogue of `USER.md`, but modeling the
  repository and its conventions rather than the person.
- **2-3** FTS index over session history (SQLite FTS5, as Hermes uses).
- **2-4** LLM-summarized cross-session recall: retrieve, then summarize into the
  budget rather than pasting raw transcript.
- **2-5** Atomic batch memory operations.

### Phase 3 — Worktree coding sessions with lifecycle

This is the one genuinely excellent idea in OpenClaw, and it slots directly into
PhaseFlow: today `/project-execute` runs tasks in the working tree with no
isolation and no landing story.

- **3-1** Session state machine: `active` → `pending_decision` → `pr_open` →
  `merged` | `released` | `dismissed` | `no_change`. Persist across restarts.
- **3-2** Git worktree per task, so parallel `--fleet` execution stops sharing one
  tree. This also removes the `isolation` gap in `/project-execute`.
- **3-3** Worktree strategies: `off` | `manual` | `auto-merge` | `auto-pr`.
- **3-4** Permission modes: `plan` | `ask` | `delegate`, orthogonal to strategy.
- **3-5** PR/merge follow-through with a routed summary back to the originating
  surface (TUI, iOS, or channel).

*Dependency:* 3-2 should land before any further autonomous multi-task execution;
today's AI-Interview run would have been safer with it.

### Phase 4 — Execution budgets and smart approvals

Directly motivated by this session: task `01-01` failed at 25 iterations because a
session-level budget was applied to a repo-scaffolding task.

- **4-1** Per-task `max_iter` in `assignments.json`, defaulting to the session
  budget. Fixes the observed failure.
- **4-2** Budget exhaustion as a distinct outcome from failure, with resumability
  rather than a bare `failed`.
- **4-3** Smart approvals: LLM reviews the proposed command against policy and
  auto-approves the safe majority, escalating only the genuinely risky. Feeds
  `internal/safety`. **Must fail closed** — an approval reviewer that fails open
  is worse than no reviewer.
- **4-4** Cancellation that interrupts in-flight LLM requests. Ctrl-C and Escape
  both failed to stop a running executor in testing; cancellation currently only
  lands between tasks.

### Phase 5 — Pluggable terminal backends

- **5-1** `TerminalBackend` interface over the existing `GOPHERMIND_SHELL_CONTAINER`
  container-exec path.
- **5-2** Backends: local, Docker, SSH. (Skip Singularity/Modal/Daytona until asked
  — SSH covers the remote-box case that matters here.)
- **5-3** Per-task backend selection in `assignments.json`, so a deploy task can run
  on the target host while build tasks stay local.

### Phase 6 — Scheduling

- **6-1** Cron scheduler as a first-class subsystem (`internal/jobs` is the
  natural home — it already has bounded-concurrency execution).
- **6-2** Delivery routing: scheduled job output to TUI, iOS push, or channel.
- **6-3** Blueprints: named schedule templates ("nightly", "weekly audit") so users
  never write cron syntax.

### Phase 7 — Channels (Telegram + Slack only)

- **7-1** `Channel` interface (Send / Receive / Capabilities).
- **7-2** Telegram adapter with action-token UX — inline buttons for Merge / Open
  PR / Later / Discard, which is OpenClaw's best interaction idea and pairs
  exactly with Phase 3's state machine.
- **7-3** Slack adapter.
- **7-4** Route through existing `serve`, reusing its session model and auth rather
  than standing up a second gateway.

### Phase 8 — Subagent observability

- **8-1** Live subagent transcript streaming to the parent surface.
- **8-2** Durable delivery ledger so a subagent's result cannot be lost.
- **8-3** Background fan-out: `delegate_task(background=true)`.
- **8-4** Per-session USD cost surfaced inline, extending `usagelog`.

### Phase 9 — Reasoning and deliberation

- **9-1** Reasoning effort tiers extended to `max`/`ultra`, per-model.
- **9-2** Mixture-of-Agents: N models answer, deliberation is visible, results
  aggregate. gophermind's `internal/abtest` is the natural substrate.
- **9-3** Completion contracts for `/goal`: evidence-based verification that a goal
  is met, rather than model self-assertion. Pairs with `verification-before-completion`.

### Phase 10 — Export, secrets, and operations

- **10-1** Session export to Markdown, HTML, and Hugging Face trace format,
  extending existing JSONL + redaction.
- **10-2** Pluggable `SecretSource` with 1Password and Bitwarden backends.
- **10-3** Scale-to-zero for `serve` with drain coordination.
- **10-4** Python/RPC scripting over the tool surface.

## Sequencing rationale

Phases 1–2 make gophermind *learn*, which is Hermes's actual moat and the thing no
amount of channel breadth substitutes for. Phase 3 makes autonomous execution
*safe to land*, which is the precondition for trusting `/project-execute` on real
work. Phase 4 fixes concrete defects observed in this session. Only then does
reach (5–7) and polish (8–10) pay off.

A reasonable first cut, if the whole thing is too much: **Phases 1, 3, and 4.**
Those three make gophermind a harness that learns from its work, isolates it, and
lands it — which is a coherent product story on its own.

## Sources

- [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) — features and architecture
- [Hermes Agent releases](https://github.com/NousResearch/hermes-agent/releases) — v0.15.0–v0.19.0 changelogs
- [Hermes Atlas handbook](https://hermesatlas.com/guide/) — v0.19.0 skills, memory, toolsets
- [OpenClaw documentation](https://docs.openclaw.ai/) — gateway, channels, plugins
- [OpenClaw gateway architecture](https://docs.openclaw.ai/concepts/architecture) — WS protocol, device trust
- [OpenClaw CLI reference](https://openclawlab.com/en/docs/cli/) — command surface
- [goldmar/openclaw-code-agent](https://github.com/goldmar/openclaw-code-agent) — worktree lifecycle, permission modes
- [Hermes vs OpenClaw comparison](https://www.ai.cc/blogs/hermes-agent-2026-self-improving-open-source-ai-agent-vs-openclaw-guide/) — vendor-adjacent, treated as claims not facts
