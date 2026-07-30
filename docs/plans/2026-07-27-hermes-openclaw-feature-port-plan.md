# Hermes & OpenClaw — gophermind port backlog

**Created:** 2026-07-27 · **Rewritten:** 2026-07-30 as an execution backlog
**Competitors analysed:** Hermes Agent v0.19.0 (Nous Research) · OpenClaw v2026.7.2-beta.5
**Code baseline:** this repository at `e79ea06`
**Review that prompted the rewrite:** `docs/plans/2026-07-30-hermes-openclaw-plan-review.md`

## What this document is

An execution backlog: sized work items, each with an acceptance criterion, each
citing the code it builds on. The competitive analysis that produced it is
summarised below and preserved in full in the review document.

Sizes: **S** ≤1 day · **M** 2–4 days · **L** ≥1 week.

## Why it was rewritten

The 2026-07-27 draft was a strategy document formatted as a plan: 11 phases, ~55
unsized items, no acceptance criteria, and eight factual errors in its assessment of
gophermind. Seven undercounted gophermind — scoring a capability absent where
working code already existed — which inflated greenfield work and hid integration
work. The eighth ran the opposite way and mattered most: **MCP client was scored
"parity" when gophermind has no MCP client at all**, so a real gap received no phase
and left the roadmap silently.

Every gophermind claim below now carries a `file:line` citation. Anything that
could not be verified against source is marked *unverified* rather than scored.

## Competitive summary

**Hermes Agent** — thesis is *Harness Engineering*: the model is replaceable, value
lives in the instruction, memory, feedback, and orchestration layers. Same thesis as
gophermind, so it is the closer competitor and the source of most items here. Its
moat is a closed learning loop: skills that are created from experience and refined
in use.

**OpenClaw** — thesis is a single persistent Gateway process owning every messaging
surface on a host. A personal-assistant platform; coding arrives via the
`openclaw-code-agent` plugin. Its one excellent idea for a coding harness is the
**worktree session lifecycle**, and its July line added event-driven scheduling
worth copying.

**gophermind leads on** — PhaseFlow spec-driven orchestration (`internal/phaseflow/engine.go`),
tamper-evident audit (`VerifyAuditFile`, `internal/safety/audit.go:127`),
prompt-injection defence (`DetectInjection`, `internal/safety/injection.go:25`), MCP
*server* (`internal/mcp/server.go`), and session-store depth — fork-at-turn
(`branch.go`), merge/diff, at-rest encryption (`crypto.go`), redacted export
(`redact_export.go`). None of that is traded away below.

**Explicitly not building** — WhatsApp/Signal/iMessage/Discord/Matrix/Teams/Zalo
channels, device nodes (camera/canvas/location), Honcho dialectic user modelling, a
skill marketplace, a web dashboard, and cloud-worker fleets. All serve OpenClaw's
personal-assistant mission, not a coding harness. Telegram and Slack only, if
channels happen at all.

## Corrected assessment

The eight errors the rewrite fixes, each verified against source:

| # | Old claim | Verified reality | Now |
|---|---|---|:--:|
| 1 | Smart approvals absent | `internal/safety/judge.go`, wired `cmd/gophermind/main.go:810` | ◐ |
| 2 | Cron absent | `--every` interval runner, `cmd/gophermind/main.go:113` | ◐ |
| 3 | Per-session USD cost partial | `UsageSnapshot.String()`, `internal/agent/usage.go` | ● |
| 4 | Secret manager absent | `ResolveSecret()`, `internal/safety/secrets.go:14` | ◐ |
| 5 | Tool scripting absent | plugin protocol, `internal/tools/plugin.go:68` | ◐ |
| 6 | Session archive absent | `Export`/`Import` `manage.go:56,77`, `GCProtecting` `gc_policy.go:13` | ◐ |
| 7 | No reuse seams cited | eight phases had substrate in-tree | cited throughout |
| 8 | **MCP client at parity (●)** | `internal/mcp` is server-only; sole call `main.go:924` | **○** |

Re-verified as already correct, listed so they are not re-litigated: `/goal` ◐
(`internal/tui/commands.go:203`), plugin trust ○ (`LoadPlugins` performs no
gating), browser control ○, live subagent transcripts ○ (`internal/trace` is span
timing), worktree ○, scale-to-zero ○, effort tiers ◐ (`main.go:82`), LSP ◐
(`internal/lsp/client.go`), skills discovery ● (`internal/project/skills.go:15`).

---

## Phase 0 — Fail-open approval gate · **S** · security · do first

`cmd/gophermind/main.go:786` seeds `approve := safety.ApprovalFunc(safety.Auto)`,
and `Auto` is `return true` (`internal/safety/safety.go:111`). `JudgeApproval`
returns `fallback(...)` when the judge errors, so with approval mode unset and no
`.gophermind/policy` file, **an unreachable judge auto-approves every gated
mutating tool call.** `ApprovalWithTimeout` (`main.go:815`) wraps *outside* the
judge and never fires on a fast error — it guards latency, not failure.

The doc comment in `judge.go` asserts the opposite of the actual behaviour, which is
how this survived review.

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 0-1 | On-error policy in `internal/safety/judge.go` (`deny` \| `fallback`), default **deny** | Judge error denies under default config | S |
| 0-2 | Correct the misleading doc comment | Comment matches behaviour | S |
| 0-3 | Regression test | Judge returns `error` + `safety.Auto` fallback → **denied** | S |

Precedent: `main.go:797-805` already refuses the run on an unknown `GOPHERMIND_ROLE`.
Independent of every other phase; unblocks trusting Phases 1–3.

## Phase 1 — Self-improving skills · **L**

The genuine capability gap, and the one that compounds — every later phase generates
trajectories that improve it. Substrate exists (`internal/project/skills.go:15`
discovers `.gophermind/skills/*.md` and feeds the `skills` prompt section,
`internal/prompt/template.go:25`); the learning loop does not.

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 1-1 | Skill frontmatter: triggers, origin, usage count, last-refined | Existing plain-markdown packs still load unchanged | S |
| 1-2 | Pattern detection over repeated tool sequences and user corrections | Threshold configurable, not a hardcoded 5 | M |
| 1-3 | Draft skills to `.gophermind/skills/proposed/` | Nothing reaches the live prompt path without explicit acceptance | M |
| 1-4 | `/learn` command, registered in `internal/tui/commands_registry.go` | Distils the current session into a proposed skill | S |
| 1-5 | Failure-driven refinement | A skill cannot grow without bound across refinements | M |
| 1-6 | agentskills.io import/export | Round-trips a published skill without loss | M |
| 1-7 | Trust gating on import | Also closes the `LoadPlugins` hole below | S |

**1-7 is security work, not packaging.** `LoadPlugins`
(`internal/tools/plugin.go:68`) executes any `*.plugin.json` dropped into
`.gophermind/plugins/` with no signature, allowlist, or prompt. Combined with 1-3,
where the agent authors files that become its own instructions, both paths need the
same gate.

## Phase 2 — Worktree session lifecycle · **L**

OpenClaw's best idea, and the precondition for trusting `/project-execute` on real
work: today tasks run in the working tree with no isolation and no landing story.

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 2-1 | 7-state machine: `active`→`pending_decision`→`pr_open`→`merged`\|`released`\|`dismissed`\|`no_change` | Survives restart; `released` distinguished from `merged` | M |
| 2-2 | Git worktree per task — reuse `runGit()`, `internal/tools/git.go:132` | `--fleet N` tasks no longer share one tree | M |
| 2-3 | Strategies `delegate`(default)\|`ask`\|`off`\|`manual`\|`auto-merge`\|`auto-pr` | Each reaches a terminal state from every entry state | M |
| 2-4 | Permission modes `plan`(default)\|`direct`\|`ask` | Orthogonal to strategy; all combinations valid | S |
| 2-5 | Two-step completion contract | Terse status emits before summarisation begins | M |
| 2-6 | **Risk handling** | See below — the old plan had none | M |

**On 2-1:** `released` is the subtle state. Content landed via rebase or squash is
not an ancestor of base, so an ancestry-only check either leaks branches forever or
deletes unlanded work. Get this wrong and it is a data-loss bug.

**On 2-6:** this phase mutates the user's real git repository from an autonomous
loop, so it needs: orphaned-worktree cleanup when the process dies mid-task, a disk
bound under `--fleet N`, a defined `auto-merge` conflict path, and a documented
recovery route from `dismissed`. Shipping 2-1..2-5 without 2-6 inverts the
project's own safety priorities.

## Phase 3 — Budgets and approvals · **M**

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 3-1 | Per-task `max_iter` in `assignments.json`, defaulting to `cfg.MaxIter` (`internal/config/config.go:113`, default 25) | A scaffolding task can exceed the session default without editing global config | S |
| 3-2 | Budget exhaustion as an outcome distinct from failure | Exhausted task is resumable, not `failed` | S |
| 3-3 | Extend the **existing** `JudgeApproval` to full smart approvals | Judgment binds the exact operation, not the tool class | M |
| 3-4 | Cancellation that interrupts in-flight LLM requests | Ctrl-C and Escape stop a running executor mid-request | M |
| 3-5 | Approval queue with explicit release | Pending approvals inspectable and individually releasable | M |

3-3 is an extension, not new construction — see correction #1. Reuse
`policy.go`, `policy_approval.go`, `ApprovalWithTimeout`, and `rbac.go`; without
3-5 a smart-approval gate merely relocates the blocking prompt.

3-1 is motivated by an observed failure: task `01-01` died at 25 iterations because
the session-level default was applied to a repo-scaffolding task.

## Phase 4 — MCP client · **M** · restored by correction #8

gophermind exposes its tools over MCP but **cannot consume any**. Both competitors
can. This is how a harness acquires Playwright, Sentry, or database tooling without
hand-writing each integration, and it was invisible in the previous draft because
the row read "parity."

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 4-1 | stdio JSON-RPC client | `initialize`, `tools/list`, `tools/call` against a real server | M |
| 4-2 | Server config + process lifecycle (`.gophermind/mcp.json`) | Servers start, restart on crash, shut down cleanly | S |
| 4-3 | Register remote tools via `tools.NewRegistry` | Remote tools pass through the Phase 0/3 safety gate | M |

Two in-tree precedents for the framing: `internal/mcp/server.go` (same protocol,
opposite direction) and `internal/lsp/framing.go`.

**4-3 is the one to get right.** A remote MCP server is untrusted input; its tools
must not bypass the approval gate that local tools obey.

## Phase 5 — Wire-ups · **S** · best effort-to-value here

The data layer is already complete. This is display work.

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 5-1 | Context ring in the TUI status line | Shows context used/available, live token counts, active model | S |
| 5-2 | Live reasoning-stream rendering | A long think shows progress rather than appearing hung | M |
| 5-3 | Reasoning effort surfaced in TUI and iOS | Changeable without restarting | S |
| 5-4 | Lazy or cached startup model probe | Probe does not run on every start | S |

**5-1 needs almost no new logic.** `UsageSnapshot.String()`
(`internal/agent/usage.go`) already emits `"tokens: 1200/340/1540 · ~$0.02"`, and
`Capabilities.ContextWindow` (`internal/llm/capabilities.go:19`) supplies the
denominator. This item absorbs the old Phase 8-4, which was the same change listed
twice. `internal/stream` has no `reasoning` handling today, which is why 5-2 is M
and not S. `d07b0b1` bounded the startup probe with a deadline; it still runs.

## Phase 6 — Scheduling · **M**

Replaces `--every` (`cmd/gophermind/main.go:113`), a foreground interval runner that
dies with the process. Not greenfield — see correction #2.

| Item | Work | Acceptance | Size |
|---|---|---|:--:|
| 6-1 | Durable scheduler in `internal/jobs` | Survives restart; `--every` subsumed or deprecated | M |
| 6-2 | Event-driven jobs | Watches a build/deploy and resumes the workflow with its exit code | M |
| 6-3 | Condition-gated triggers | Unchanged poll costs a comparison, not an agent wakeup | S |
| 6-4 | Declarative reapply | Re-declaring updates in place, preserving identity and history | S |
| 6-5 | Delivery routing → TUI, iOS push, channel | Output reaches the originating surface | M |
| 6-6 | Named blueprints | Common schedules need no cron syntax | S |

`internal/jobs` already has bounded-concurrency execution and no scheduling, so it
is the natural home. **6-3 should reuse `internal/watch.Changed(path, since)`**,
which is already exactly a change-gate. 6-2 is what makes scheduling useful to a
*coding* harness rather than a reporting one, and it composes with Phase 2 — "PR
checks went green" is precisely such an event.

---

## Deferred

Not dropped. Each carries its reason and the seam it will build on.

- **Bounded memory and recall — the costliest deferral.** The 07-27 draft ranked
  this #2 and it was half the "make gophermind learn" thesis. Deferred because it
  needs Phase 1 to have produced something worth remembering, and `codeindex` + RAG
  already cover repository context. **Cheapest piece to pull forward: SQLite FTS5
  over session history**, since `internal/session/search.go:80` `bufio.Scanner`s
  every file on every search. Reconsider as soon as Phase 1 lands.
- **Pluggable terminal backends** (local/Docker/SSH) — seam:
  `internal/tools/shell_sandbox.go:12`.
- **Channels (Telegram, Slack)** — depends on Phase 2's state machine; the action
  tokens (Merge / PR / Later / Discard) are designed in 2-3 and rendered here.
- **Subagent observability** — `internal/trace` is span timing, not transcript
  streaming, so this is genuinely new work.
- **Reasoning tiers and Mixture-of-Agents** — `internal/abtest` is the substrate.
- **Export, secrets, scale-to-zero, tool scripting** — seams:
  `redact_export.go`, `secrets.go:14`, `internal/tools/plugin.go:68`.

## Backlog reconciliation

Mapping to `TODO-COMBINED.md`, which carries 220 open items and previously had no
relationship to this plan:

| Backlog item | Maps to |
|---|---|
| #12 Token-aware request trimming | deferred — memory |
| #14 Per-turn tool-call budget | 3-1 |
| #15 Sub-agent dispatch (`spawn_agent`) | deferred — subagent observability |
| #19 Deterministic replay | deferred — subagent observability |
| #356 Scheduled runs | 6-1 — *checkbox corrected 2026-07-30; only `--every` shipped* |
| #392 Tool marketplace/registry | 1-6, 1-7 — *checkbox corrected 2026-07-30* |

## Suggested order

**Phase 0 first and alone** — it is a security fix, it is size S, and every
autonomous phase after it is more trustworthy once it lands.

Then **1 → 2 → 3**: learn from the work, isolate the work, bound and gate the work.
**Phase 5 is out of sequence on merit** — it depends on nothing and 5-1 is the
smallest change in this document with a visible payoff; take it whenever there is a
gap. **Phase 4** whenever external tooling becomes the bottleneck. **Phase 6** last
of the committed phases.

Minimum coherent product story: **0, 1, 2, 3, plus 5-1.**

## Sources

- [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) · [releases](https://github.com/NousResearch/hermes-agent/releases) · [Hermes Atlas handbook](https://hermesatlas.com/guide/)
- [OpenClaw docs](https://docs.openclaw.ai/) · [releases.sh](https://releases.sh/openclaw/releases) · [changelogs.info](https://changelogs.info/openclaw/) · [gradually.ai](https://www.gradually.ai/en/changelogs/openclaw/)
- [goldmar/openclaw-code-agent](https://github.com/goldmar/openclaw-code-agent) — worktree lifecycle, permission modes
- Full feature matrix and sourcing confidence: `docs/plans/2026-07-30-hermes-openclaw-plan-review.md`
