# Hermes & OpenClaw — feature analysis and gophermind port plan

**Date:** 2026-07-27
**Revised:** 2026-07-29 — see *Revision log* below
**Scope:** feature-by-feature comparison of Hermes Agent (Nous Research, v0.19.0)
and OpenClaw (OpenClaw Foundation, v2026.7.2-beta.5) against gophermind, then a
phased plan to bring the worthwhile capabilities into gophermind.

## Revision log

The 2026-07-27 draft is materially updated. What changed:

- **OpenClaw moved.** The July beta line (`v2026.7.1-beta.2` → `v2026.7.2-beta.5`,
  5–28 July) added remote/cloud coding sessions, `openclaw attach`, a session-first
  Control UI, event-driven cron, an approval queue, and MCP isolation. None of this
  was in the first draft.
- **The `openclaw-code-agent` section was wrong.** Permission modes and worktree
  strategies were conflated. Corrected below; it changes Phase 3.
- **gophermind was undersold on session management.** `internal/session` already has
  fork-at-turn, merge, diff, tags, alias, search, replay, GC policy, encryption, and
  redacted export. The first draft credited none of it. This removes work from the
  plan.
- **Two categories were missing entirely:** performance/latency and context
  transparency. Hermes spent a whole release (v0.19.0) on them.

## Method and confidence

Both products post-date this analysis's training data, so everything here comes
from primary and secondary sources fetched on 2026-07-27 and re-verified
2026-07-29: the Hermes GitHub repo and release notes, the OpenClaw documentation
site, third-party changelog aggregators, and the `openclaw-code-agent` plugin repo.

Confidence is uneven and worth stating:

- **High** — Hermes release list and version dates; the `openclaw-code-agent`
  feature surface; everything on gophermind's side, which is read from this
  repository at `9726444`.
- **Medium** — OpenClaw's July feature detail. The official docs site carries no
  version number and `openclaw.com.au/updates` returned 403, so the July detail
  comes from changelog aggregators (`releases.sh`, `changelogs.info`,
  `gradually.ai`) rather than the vendor's own release notes. Direction is
  reliable; exact flag names are not.
- **Low** — the Hermes-vs-OpenClaw comparison article, which is published by a
  Hermes-friendly outlet. Its claims are flagged, not repeated as fact.

Where a number is quoted from vendor marketing (Hermes's "80% startup reduction",
"14× faster markdown"), it is reported as their claim.

## What each product actually is

**Hermes Agent** — Nous Research's open-source autonomous agent, first released
February 2026, now at v0.19.0 (20 July 2026, "The Quicksilver Release"). Its
thesis is *Harness Engineering*: the LLM is replaceable, and the value sits in the
instruction, constraint, feedback, memory, and orchestration layers around it.
That is precisely gophermind's thesis, which makes Hermes the closer competitor of
the two and the more relevant source of ideas.

**OpenClaw** — Peter Steinberger's self-hosted personal AI assistant, MIT licensed
under a non-profit foundation, reportedly the most-starred repo on GitHub within
five months. Its thesis is a *single persistent Gateway process* that owns every
messaging surface on a host and routes them to agents. It is a personal-assistant
platform first; coding is delivered through a plugin (`openclaw-code-agent`), not
through the core — though the July line pulls coding notably closer to the core
with `openclaw attach` and cloud-worker sessions.

**gophermind** — a minimal agentic coding harness: TUI plus CLI, spec-driven
project orchestration (PhaseFlow), RAG over the repo, MCP in both directions, an
audit-logged safety layer, a deep session store, and a webhook/SSE server with an
iOS client.

The strategic read is unchanged, with one amendment: **Hermes is converging on
gophermind's territory from the agent side; OpenClaw is adjacent but converging
faster than the first draft assumed.** Most of what is worth taking still comes
from Hermes, but OpenClaw's July work on session lifecycle and event-driven
scheduling is now directly relevant.

## Feature-by-feature matrix

Legend: ● full · ◐ partial · ○ absent

### Memory and learning

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Persistent cross-session memory | ● | ◐ | ◐ | gophermind has RAG + `internal/embed`; no durable user model |
| Bounded working memory (`MEMORY.md` ~2.2k chars, `USER.md` ~1.4k) | ● | ◐ | ○ | Hermes bounds deliberately to prevent context bloat |
| Session full-text search | ● | ● | ● | `internal/session/search.go`; OpenClaw added a searchable sidebar in July |
| LLM-summarized recall | ● | ○ | ○ | gophermind searches but does not summarize into budget |
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

### Session management *(new section — the first draft omitted this)*

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Fork / branch a session at a turn | ◐ | ● | ● | **parity or better** — `session branch <src> <new> --at N` |
| Merge / diff sessions | ○ | ○ | ● | **gophermind leads** — `merge.go`, `diff.go` |
| Rename / alias / tag | ◐ | ● | ● | `name.go`, `alias.go`, `tags.go` |
| Replay a session | ◐ | ● | ● | `replay.go`; shipped to iOS 2026-07 |
| Archive + restore, unread state, pin/group | ○ | ● | ○ | OpenClaw's July sidebar; gophermind has GC policy, not archive UX |
| At-rest encryption | ○ | ○ | ● | **gophermind leads** — `crypto.go`, `encrypted_store.go` |
| Redacted export | ○ | ○ | ● | **gophermind leads** — `redact_export.go` |
| Session sync across machines | ◐ | ● | ◐ | gophermind `remote.go` is push/pull, not live attach |
| Attach an external harness to a live session | ○ | ● | ○ | `openclaw attach` — temporary scoped creds, terminal resume |

### Orchestration and coding

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Spec-driven phase/plan workflow | ○ | ○ | ● | **gophermind leads** — PhaseFlow is unmatched by either |
| Per-task agent + model assignment | ○ | ◐ | ● | **gophermind leads** — `assignments.json` |
| Isolated subagents, parallel | ● | ◐ | ● | gophermind: `spawn`, `queue --fleet N`, `jobs.RunConcurrent` |
| Background fan-out delegation | ● | ● | ◐ | Hermes `delegate_task(background=true)` |
| Live subagent transcripts / watch windows | ● | ○ | ○ | v0.17.0/v0.19.0, durable delivery ledger |
| Git worktree coding sessions w/ lifecycle | ○ | ● | ○ | 7 states; see corrected detail below |
| Permission modes | ◐ | ● | ◐ | OpenClaw: `plan` \| `direct` \| `ask` |
| Worktree strategies | ○ | ● | ○ | `delegate` (default) \| `ask` \| `off` \| `manual` \| `auto-merge` \| `auto-pr` |
| PR/merge follow-through from chat | ○ | ● | ○ | two-step contract, routed summary back to thread |
| Goal loops with evidence verification | ● | ● | ◐ | Hermes *completion contracts*; OpenClaw verifier-driven/Ralph-style goal tasks |
| Mixture-of-Agents deliberation | ● | ○ | ◐ | v0.18.0 shows every reference model's full output before synthesis |
| Python/RPC scripting over tools | ● | ○ | ○ | collapses multi-step pipelines into one script |

**Corrected `openclaw-code-agent` detail.** The first draft got this wrong. The
actual surface:

- **Lifecycle states (7):** `active` → `pending_decision` → `pr_open` → `merged` |
  `released` | `dismissed` | `no_change`. `released` is the subtle one — content
  already landed on base via rebase/squash, so the branch is safe to delete even
  though ancestry says otherwise.
- **Permission modes (3):** `plan` (default — approval before execution), `direct`
  (execute immediately, report after), `ask` (user controls branch follow-through
  after completion).
- **Worktree strategies (6):** `delegate` (default — orchestrator handles merge),
  `ask`, `off`, `manual`, `auto-merge`, `auto-pr`. Orthogonal to permission mode.
- **Config keys:** `defaultWorkdir`, `defaultHarness` (`claude-code`/`codex`/
  `opencode`), `permissionMode`, `planApproval`, `defaultWorktreeStrategy`.
- **Two-step completion contract:** terse canonical status first, *then* the
  orchestrator wakes and sends a factual summary to the originating thread.
- **Adaptive action buttons:** new branch → Merge / PR / Later / Discard; existing
  PR → View / Sync.

### Execution environment

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Container-isolated shell | ● | ● | ◐ | gophermind: `GOPHERMIND_SHELL_CONTAINER` |
| Pluggable terminal backends | ● | ◐ | ○ | Hermes: local, Docker, SSH, Singularity, Modal, Daytona |
| Managed sandbox lifecycle | ◐ | ● | ○ | OpenClaw `sandbox` CLI |
| Remote / cloud-worker execution | ◐ | ● | ○ | July: Control UI sessions run on cloud workers |
| Smart approvals (LLM reviews the command) | ● | ● | ○ | Hermes v0.19.0; OpenClaw model-judged approvals tied to exact operations |
| Approval queue with explicit release | ○ | ● | ○ | Control UI queue + `/approve`; policy-blocked requests rejected before reaching the node |
| Secret manager integration | ● | ◐ | ○ | Hermes: 1Password + Bitwarden via pluggable `SecretSource` |
| MCP isolation | ◐ | ● | ◐ | OpenClaw hardened MCP sandboxing in July |
| Plugin/skill trust gating | ● | ● | ○ | OpenClaw requires `--force` for untrusted plugin sources |
| Browser control | ● | ● | ○ | Hermes cloud browser; OpenClaw dedicated Chrome |

### Interfaces and reach

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| TUI | ● | ◐ | ● | comparable |
| Messaging channels | ● 14+ | ● 11+ | ○ | Discord, Google Chat, iMessage, Matrix, Teams, Signal, Slack, Telegram, WhatsApp, Zalo, WebChat |
| Single gateway owning all surfaces | ● | ● | ◐ | gophermind `serve` is webhook/SSE, not multi-channel |
| Mobile client | ◐ | ● | ● | gophermind has a real iOS app + QR pairing |
| Device nodes (camera, canvas, location) | ○ | ● | ○ | OpenClaw-specific, personal-assistant territory |
| Web control dashboard | ● | ● | ○ | OpenClaw's is now the primary surface |
| Cron scheduling → any channel | ● | ● | ○ | |
| **Event-driven scheduling** | ◐ | ● | ○ | jobs watch a build/deploy/script and resume the workflow with its exit code |
| Condition-gated triggers | ○ | ● | ○ | fire only on state *change* — avoids waking the agent on unchanged polls |
| Declarative cron reapply (in-place, identity preserved) | ○ | ● | ○ | re-declaring updates the job rather than duplicating it |
| Automation Blueprints (no cron syntax) | ● | ○ | ○ | v0.17.0 |
| MCP client | ● | ● | ● | parity |
| MCP/tool server | ◐ | ◐ | ● | gophermind exposes itself as an MCP server |
| ACP / IDE bridge | ○ | ● | ◐ | gophermind has LSP + editor integrations |

### Models and operations

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Provider breadth | ● 400+ | ◐ | ● | gophermind: any OpenAI-compatible + profiles |
| Local backends (Ollama/vLLM/llama.cpp) | ● | ◐ | ● | parity — gophermind runs on local vLLM today |
| Reasoning effort tiers | ● | ● | ◐ | gophermind `-think low\|medium\|high`; both competitors expose a UI slider |
| Per-session model/mode picker | ◐ | ● | ● | gophermind shipped 2026-07-22; OpenClaw caught up in July |
| Cost tracking per session | ● | ● | ◐ | gophermind `usage report`; OpenClaw shows USD inline |
| **Inline context transparency ("context ring")** | ◐ | ● | ○ | exact context use + live input/output token counts + active model, without leaving the chat |
| Session export (MD/HTML/Quarto/HF traces) | ● | ○ | ◐ | gophermind exports JSONL + redaction |
| Trajectory generation for training | ● | ○ | ◐ | gophermind has `golden`, `benchmark`, `tuning` |
| Tamper-evident audit log | ○ | ○ | ● | **gophermind leads** — `audit verify` chain |
| Prompt-injection defense | ◐ | ◐ | ● | **gophermind leads** — `internal/safety/injection.go` |
| Scale-to-zero gateway | ● | ◐ | ○ | v0.18.0, drain coordination |

### Performance and responsiveness *(new section)*

Hermes made this an entire release. It is a real competitive axis and the first
draft ignored it.

| Capability | Hermes | OpenClaw | gophermind | Notes |
|---|:--:|:--:|:--:|---|
| Fast startup | ● | ◐ | ◐ | Hermes claims ~80% startup reduction in v0.19.0; gophermind bounds its startup model-probe with a deadline (`d07b0b1`) but still probes |
| Live reasoning-stream rendering | ● | ● | ○ | no `reasoning` handling in `internal/stream` or `internal/tui` |
| Virtualized diff viewing | ● | ◐ | ○ | matters once diffs get large |
| Fast markdown rendering | ● | ◐ | ◐ | Hermes claims 14×; gophermind uses lipgloss/glamour-class rendering, unmeasured |

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
6. **Session store depth** *(newly recognized)* — fork-at-turn, merge, diff,
   at-rest encryption, redacted export, and GC policy. OpenClaw's July session work
   is largely *UI* over a shallower store. gophermind's gap is presentation, not
   capability, which makes it cheap to close.

The port plan below is deliberately additive: nothing in it trades away these.

## Strategic filter — what NOT to build

The request was "all the features." Delivering that honestly means saying which
ones are wrong for this product:

- **WhatsApp / Signal / iMessage / Discord / Matrix / Teams / Zalo channels.**
  These serve OpenClaw's personal-assistant mission. gophermind is a coding
  harness; a coding agent reachable from Signal is a novelty, not a workflow.
  **Recommend: Telegram and Slack only** — the two where "the build finished,
  here's the PR" is genuinely useful. Skip the rest.
- **Device nodes (camera, canvas, location).** Entirely off-mission.
- **Honcho dialectic user modeling.** Deep personal user modeling is valuable for
  an assistant that knows *you*; a coding harness benefits far more from modeling
  *the repository*. gophermind already does that with RAG and `codeindex`.
- **Skill marketplace/hub.** The hub is a community-scale asset, not a feature you
  build; adopting the **agentskills.io open standard** captures most of the value
  at a fraction of the cost.
- **Web control dashboard.** Real cost, and gophermind already has TUI + CLI +
  iOS. Revisit only if multi-channel lands and needs a config surface.
- **Cloud-worker execution (OpenClaw's remote sessions).** SSH terminal backends
  (Phase 5) cover the "run it on the big box" case without standing up a worker
  fleet. Revisit only if SSH proves insufficient.

Everything below excludes these.

## The port plan

Eleven phases, ordered by value-per-unit-effort and by dependency. Phases 1–4 are
the ones that change what gophermind *is*; 5–11 broaden reach and polish.

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
- **1-7** *(new)* Trust gating on imported skills, mirroring OpenClaw's `--force`
  rule: a skill from outside the repo is inert until explicitly trusted. This is
  the import-side counterpart to 1-3's acceptance gate, and it matters more for
  gophermind than for either competitor because gophermind's differentiator is
  the safety layer.

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
  `internal/session/search.go` exists but scans; FTS is the scale fix.
- **2-4** LLM-summarized cross-session recall: retrieve, then summarize into the
  budget rather than pasting raw transcript.
- **2-5** Atomic batch memory operations.

### Phase 3 — Worktree coding sessions with lifecycle

This is the one genuinely excellent idea in OpenClaw, and it slots directly into
PhaseFlow: today `/project-execute` runs tasks in the working tree with no
isolation and no landing story.

**Scope reduced from the first draft** — gophermind's session fork/merge/diff
already exist, so this phase is about the *git* side and the *state machine*, not
about session manipulation.

- **3-1** Session state machine, all seven states: `active` → `pending_decision` →
  `pr_open` → `merged` | `released` | `dismissed` | `no_change`. Persist across
  restarts. Do not skip `released` — a branch whose content landed via rebase or
  squash is not an ancestor of base, and treating it as unmerged is how you either
  leak branches forever or delete unlanded work.
- **3-2** Git worktree per task, so parallel `--fleet` execution stops sharing one
  tree. This also removes the `isolation` gap in `/project-execute`.
- **3-3** Worktree strategies: `delegate` (default) | `ask` | `off` | `manual` |
  `auto-merge` | `auto-pr`.
- **3-4** Permission modes `plan` | `direct` | `ask`, orthogonal to strategy.
  Default `plan`, matching OpenClaw and matching gophermind's safety posture.
- **3-5** Two-step completion contract: emit a terse canonical status immediately,
  then wake the orchestrator to route a factual summary back to the originating
  surface (TUI, iOS, or channel). The two steps matter — a single fat message
  either blocks on summarization or ships before the work is verifiably done.
- **3-6** *(new)* Adaptive action tokens: new branch → Merge / PR / Later /
  Discard; existing PR → View / Sync. Designed here, rendered in Phase 7.

*Dependency:* 3-2 should land before any further autonomous multi-task execution;
the AI-Interview run would have been safer with it.

### Phase 4 — Execution budgets and smart approvals

Directly motivated by observed failures: task `01-01` failed at 25 iterations
because a session-level budget was applied to a repo-scaffolding task.

- **4-1** Per-task `max_iter` in `assignments.json`, defaulting to the session
  budget. Fixes the observed failure.
- **4-2** Budget exhaustion as a distinct outcome from failure, with resumability
  rather than a bare `failed`.
- **4-3** Smart approvals: LLM reviews the proposed command against policy and
  auto-approves the safe majority, escalating only the genuinely risky. Feeds
  `internal/safety`. **Must fail closed** — an approval reviewer that fails open
  is worse than no reviewer. Bind the judgment to the *exact* operation, as
  OpenClaw does; approving "a git command" rather than "this git command" is a
  confused-deputy hole.
- **4-4** Cancellation that interrupts in-flight LLM requests. Ctrl-C and Escape
  both failed to stop a running executor in testing; cancellation currently only
  lands between tasks.
- **4-5** *(new)* Approval queue with explicit release: pending approvals are
  inspectable and individually releasable (`/approve`), and policy-blocked
  requests are rejected at the gateway before they reach an executor. Without a
  queue, smart approvals just relocate the blocking prompt.

### Phase 5 — Pluggable terminal backends

- **5-1** `TerminalBackend` interface over the existing `GOPHERMIND_SHELL_CONTAINER`
  container-exec path.
- **5-2** Backends: local, Docker, SSH. (Skip Singularity/Modal/Daytona until asked
  — SSH covers the remote-box case that matters here.)
- **5-3** Per-task backend selection in `assignments.json`, so a deploy task can run
  on the target host while build tasks stay local.

### Phase 6 — Scheduling

OpenClaw's July work makes plain cron look primitive; build the better version
directly rather than shipping cron and retrofitting.

- **6-1** Cron scheduler as a first-class subsystem (`internal/jobs` is the
  natural home — it already has bounded-concurrency execution, and it has no
  scheduling today).
- **6-2** **Event-driven jobs:** a job watches a build, deploy, or script and
  resumes the originating workflow with that command's exit code. This is the
  feature that makes scheduling useful to a *coding* harness rather than a
  reporting one, and it composes with Phase 3's state machine — "PR checks went
  green" is exactly such an event.
- **6-3** **Condition-gated triggers:** fire only when watched state *changes*, so
  an unchanged poll costs a comparison rather than a full agent wakeup. On local
  models this is the difference between a viable and an unusable poll interval.
- **6-4** **Declarative reapply:** re-declaring a schedule updates the job in place,
  preserving its identity and run history instead of creating a duplicate.
- **6-5** Delivery routing: scheduled job output to TUI, iOS push, or channel.
- **6-6** Blueprints: named schedule templates ("nightly", "weekly audit") so users
  never write cron syntax.

### Phase 7 — Channels (Telegram + Slack only)

- **7-1** `Channel` interface (Send / Receive / Capabilities).
- **7-2** Telegram adapter rendering Phase 3-6's action tokens as inline buttons —
  Merge / Open PR / Later / Discard. This is OpenClaw's best interaction idea and
  pairs exactly with the state machine.
- **7-3** Slack adapter.
- **7-4** Route through existing `serve`, reusing its session model and auth rather
  than standing up a second gateway.

### Phase 8 — Subagent observability

- **8-1** Live subagent transcript streaming to the parent surface.
- **8-2** Durable delivery ledger so a subagent's result cannot be lost — Hermes's
  ledger survives a gateway crash, which is the actual bar.
- **8-3** Background fan-out: `delegate_task(background=true)`.
- **8-4** Per-session USD cost surfaced inline, extending `usagelog`.

### Phase 9 — Context transparency and responsiveness *(new phase)*

Cheap, highly visible, and currently absent. Bundled because they share the
render path.

- **9-1** **Context ring:** inline display of context used vs. available, live
  input/output token counts, and the active model. gophermind already accounts
  tokens in `internal/agent/usage.go` and `internal/usagelog` — this is a display
  over data that already exists, which makes it the best effort-to-value ratio in
  the plan.
- **9-2** Live reasoning-stream rendering. `internal/stream` has no `reasoning`
  handling today; on a local reasoning model, showing nothing during a long think
  reads as a hang.
- **9-3** Reasoning-effort control surfaced in TUI and iOS, not just the
  `-think` flag.
- **9-4** Startup latency: make the model probe lazy or cached rather than merely
  deadline-bounded (`d07b0b1` bounds it; it still runs).
- **9-5** Virtualized diff rendering for large diffs.

### Phase 10 — Reasoning and deliberation

- **10-1** Reasoning effort tiers extended to `max`/`ultra`, per-model.
- **10-2** Mixture-of-Agents: N models answer, **every reference model's full
  output is visible**, then results aggregate. gophermind's `internal/abtest` is
  the natural substrate; the visibility is the part that distinguishes it from
  ordinary ensembling.
- **10-3** Completion contracts for `/goal`: evidence-based verification that a
  goal is met, rather than model self-assertion. Pairs with
  `verification-before-completion`. OpenClaw's verifier-driven and Ralph-style
  goal loops are the two shapes worth supporting.

### Phase 11 — Export, secrets, and operations

- **11-1** Session export to Markdown, HTML, and Hugging Face trace format,
  extending existing JSONL + redaction.
- **11-2** Pluggable `SecretSource` with 1Password and Bitwarden backends.
- **11-3** Scale-to-zero for `serve` with drain coordination.
- **11-4** Python/RPC scripting over the tool surface.
- **11-5** Attach an external harness to a live `serve` session, with temporary
  scoped credentials — gophermind's `openclaw attach`. `internal/session/remote.go`
  is push/pull sync today, so this is new work, not an extension.

## Sequencing rationale

Phases 1–2 make gophermind *learn*, which is Hermes's actual moat and the thing no
amount of channel breadth substitutes for. Phase 3 makes autonomous execution
*safe to land*, which is the precondition for trusting `/project-execute` on real
work. Phase 4 fixes concrete defects observed in practice. Only then does reach
(5–7) and polish (8–11) pay off.

Two amendments to the first draft's ordering:

- **Phase 9-1 (context ring) is out of sequence on merit.** It is a display over
  data gophermind already collects — likely the smallest change in the entire plan
  with a visible payoff. Pull it forward opportunistically; it does not depend on
  anything.
- **Phase 6 got bigger but not slower.** Event-driven and condition-gated triggers
  (6-2, 6-3) are more valuable than the cron engine itself and should be designed
  in from the start rather than retrofitted onto a time-only scheduler.

A reasonable first cut, if the whole thing is too much: **Phases 1, 3, and 4, plus
9-1.** Those make gophermind a harness that learns from its work, isolates it,
lands it, and tells you what it is spending — a coherent product story on its own.

## Sources

- [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) — features and architecture
- [Hermes Agent releases](https://github.com/NousResearch/hermes-agent/releases) — v0.14.0–v0.19.0 changelogs
- [Hermes Atlas handbook](https://hermesatlas.com/guide/) — v0.19.0 skills, memory, toolsets
- [OpenClaw documentation](https://docs.openclaw.ai/) — gateway, channels, plugins
- [OpenClaw release notes (releases.sh)](https://releases.sh/openclaw/releases) — June–July 2026 version history
- [OpenClaw changelog (changelogs.info)](https://changelogs.info/openclaw/) — July session/cron/approval detail
- [OpenClaw changelog (gradually.ai)](https://www.gradually.ai/en/changelogs/openclaw/) — 2026.7.2 summary
- [goldmar/openclaw-code-agent](https://github.com/goldmar/openclaw-code-agent) — worktree lifecycle, permission modes
- [Hermes vs OpenClaw comparison](https://www.ai.cc/blogs/hermes-agent-2026-self-improving-open-source-ai-agent-vs-openclaw-guide/) — vendor-adjacent, treated as claims not facts
