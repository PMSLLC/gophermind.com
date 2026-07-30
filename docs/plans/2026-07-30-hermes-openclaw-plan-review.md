# Review — Hermes/OpenClaw feature port plan

**Date:** 2026-07-30
**Reviewing:** `docs/plans/2026-07-27-hermes-openclaw-feature-port-plan.md` (as of `0d7b8fe`)
**Code baseline:** this repository at `0d7b8fe`
**Reviewer's note:** the plan under review was written by the same author as this
review. Every load-bearing claim about gophermind was therefore re-checked against
source rather than accepted, and each cell below carries a `file:line` citation so a
reader can re-verify independently.

## Verdict

**Directionally sound, factually unreliable — do not execute as written.**

The strategy holds: the competitive read is correct, the "what NOT to build" filter
is the document's strongest contribution, and self-improving skills is genuinely the
biggest capability gap. But the feature matrix is wrong in a *systematic* way, and
one of the errors conceals a live fail-open security defect.

## The systematic defect

Seven verified matrix errors. **All seven run in the same direction: gophermind is
scored as lacking something for which working code or a reusable seam already
exists.**

That one-directional pattern is the actual finding. A plan with random errors needs
proofreading; a plan whose errors all inflate the amount of new work needed has a
methodological bias. The cause is visible in the document's own *Method* section: it
grades confidence in the *competitors'* data carefully (high for Hermes releases,
medium for OpenClaw aggregator detail) while asserting gophermind's column is
"ground truth… read from this repository." It was not read from the repository — it
was recalled. The competitor research was more rigorous than the self-assessment.

Consequence: the plan systematically overstates greenfield work and understates
integration work. Those need different skills, different sequencing, and different
risk handling, so the error is not merely cosmetic.

### Confirmed errors

| # | Matrix claim | Reality | Correct |
|---|---|---|:--:|
| 1 | Smart approvals `○` | `internal/safety/judge.go` + wired `cmd/gophermind/main.go:810` | **◐** |
| 2 | Cron scheduling `○` | `--every` interval runner, `cmd/gophermind/main.go:113` | **◐** |
| 3 | Per-session USD cost `◐` (Phase 8-4) | `UsageSnapshot.String()` already renders it, `internal/agent/usage.go` | **●** |
| 4 | *(no row)* Existing reuse seams | 6 phases have substrate in-tree, none cited | — |
| 5 | Secret manager `○` | `ResolveSecret()`, `internal/safety/secrets.go:14` — file-based seam exists | **◐** |
| 6 | Python/RPC scripting `○` | full out-of-process plugin protocol, `internal/tools/plugin.go:68` | **◐** |
| 7 | Session archive/restore `○` | `Export`/`Import` (`manage.go:56,77`) + `GCProtecting` (`gc_policy.go:13`) | **◐** |

Errors 5–7 were found in a second pass *after* the first review, confirming the bias
rather than exhausting it. Assume more remain: **the whole gophermind column needs
re-baselining with citations before the plan is trusted.**

## Finding 1 — the fail-open judge (security, act independently of this plan)

The most serious item, and the reason error #1 matters beyond bookkeeping.

`internal/safety/judge.go` defines `JudgeApproval(judge JudgeFunc, fallback
ApprovalFunc)`, routing each gated tool call to a model with the tool name and raw
JSON args. The approval chain assembles in `cmd/gophermind/main.go:786-816`:

```go
approve := safety.ApprovalFunc(safety.Auto)          // :786 — Auto always returns true
if cfg.ApprovalMode == "ask" { approve = interactive } // :787
if *readOnlyFlag { approve = safety.ReadMode() }       // :790
else if pol := loadRepoPolicy(...); pol != nil {
    approve = safety.PolicyApproval(pol, approve)      // :795
}
if envTruthy("GOPHERMIND_JUDGE") {
    approve = safety.JudgeApproval(newJudge(client), approve)  // :810
}
if d := approvalTimeout(); d > 0 {
    approve = safety.ApprovalWithTimeout(approve, d)   // :815
}
```

`JudgeApproval` returns `fallback(tool, argsJSON)` on judge error. With default
config — approval mode not `ask`, no `.gophermind/policy` file — that fallback is
`safety.Auto`:

```go
func Auto(tool, argsJSON string) bool { return true }   // internal/safety/safety.go:111
```

**An unreachable or erroring judge therefore auto-approves every gated mutating tool
call.** The doc comment in `judge.go` asserts that "a judge outage never silently
blocks or opens the gate." That is true only when the fallback is not `Auto`. In the
default configuration it is.

`ApprovalWithTimeout` does not cover this: it wraps *outside* `JudgeApproval`, so a
fast judge **error** returns `true` immediately, before any timeout can fire. The
timeout guards latency, not failure.

Severity is bounded — the judge is opt-in via `GOPHERMIND_JUDGE`, and a repo policy
or `ask` mode changes the fallback. But the failure mode is exactly inverted from
the stated design intent, in the subsystem the plan itself calls gophermind's
differentiator, and `internal/safety/rbac.go:797-805` shows the codebase already
knows the right pattern (an unknown `GOPHERMIND_ROLE` **refuses the run**).

Phase 4-3 says smart approvals "must fail closed." Because the matrix scored the
feature absent, that reads as a constraint on future work. It is a **bug fix on
shipped code and should not be queued behind ten phases.**

## Finding 2 — no reconciliation with the existing 220-item backlog

`TODO-COMBINED.md` carries **220 unchecked items**; `todo-3.md` adds 31. The port
plan references none of them, leaving two uncoordinated roadmaps in one repo.
Overlaps found on a single pass:

| Backlog item | Port plan phase |
|---|---|
| #12 Token-aware request trimming | 2-1 bounded memory |
| #14 Per-turn tool-call budget | 4-1 per-task `max_iter` |
| #15 Sub-agent dispatch (`spawn_agent`) | 8-3 background fan-out |
| #19 Deterministic replay | 8-2 durable delivery ledger |
| #356 Scheduled runs — marked `[x]` | 6-1 cron subsystem |
| #392 Tool marketplace — marked `[x]` | skill-hub dismissal |

Items #356 and #392 are marked **done** in the backlog while the plan treats them as
absent. `#356` in particular is only the `--every` foreground loop
(`cmd/gophermind/main.go:113`), which dies with the process — so the checkbox
overstates reality and the plan understates it. Both documents are wrong, in
opposite directions, about the same feature.

## Finding 3 — reuse seams the plan should name

Phase-by-phase, work already in the tree that the plan proposes as new:

| Phase | Existing substrate |
|---|---|
| 1 skills | `internal/project/skills.go:15` — `.gophermind/skills/*.md` discovery *(plan's claim here was correct)* |
| 3 worktrees | `internal/tools/git.go` — `runGit()`, `gitStatus()`, `gitDiff()`, `gitLog()` |
| 4-5 approval queue | `internal/safety/policy.go`, `policy_approval.go`, `ApprovalWithTimeout`, `rbac.go` |
| 5-1 terminal backends | `internal/tools/shell_sandbox.go:12` — the `GOPHERMIND_SHELL_CONTAINER` path |
| 6-3 condition-gated triggers | `internal/watch` — `Changed(path, since)` *is* a change-gate |
| 9-1 context ring | `internal/agent/usage.go` + `llm.Capabilities.ContextWindow` (`capabilities.go:19`) |
| 11-2 `SecretSource` | `internal/safety/secrets.go:14` — `ResolveSecret()` is the seam to generalize |
| 11-4 tool scripting | `internal/tools/plugin.go:68` — `LoadPlugins()`, manifest + stdio JSON protocol |

Phase 9-1 deserves a specific correction. The plan calls it "the best
effort-to-value ratio," which is right, but for the wrong stated reason: the data
layer is *already complete*. `UsageSnapshot.String()` renders
`"tokens: 1200/340/1540 · ~$0.02"` today, and `Capabilities.ContextWindow` supplies
the denominator. The remaining work is wiring one existing string and one existing
int into the TUI status line. **Phase 8-4 is the same change and should be deleted.**

## Finding 4 — process gaps

- **No sizing or acceptance criteria.** Eleven phases × ~5 items ≈ 55 work items,
  none estimated, none with a definition of done, none with a test strategy. In a
  repo where every `internal/` package has adjacent `_test.go` files, that omission
  is conspicuous. It also makes the plan's own recommendation ("a reasonable first
  cut: Phases 1, 3, 4, plus 9-1") unfalsifiable.
- **Phase 3 has no risk or rollback story.** Worktree-per-task mutates the user's
  real git repository from an autonomous loop, yet the plan covers only the happy
  path. Unaddressed: orphaned worktrees when the process dies mid-task; disk growth
  under `--fleet N`; `auto-merge` hitting a conflict; recovering work from a
  `dismissed` session. Shipping the least safety-analyzed phase against real git
  history inverts the project's own priorities.
- **An unstated cross-phase dependency.** Phase 1-3 has the agent draft skill files;
  skills are injected into the system prompt (`internal/prompt/template.go:25`).
  That is agent-controlled input to the agent's own instructions — a self-poisoning
  path that must route through the Phase 4 approval gate. Phase 1-3's acceptance
  requirement is right in spirit but never links to 4-3/4-5, so the two phases could
  be built independently and leave the hole open.

## What holds up

Stated explicitly, because the rest of this review is critical:

- **"Strategic filter — what NOT to build" is the document's best section.**
  Declining WhatsApp/Signal/iMessage/device-nodes/dashboard with reasons tied to
  product mission is the rarest and most valuable content in the plan. Keep it
  verbatim.
- **The `released`-state insight (3-1)** — content landed via rebase or squash is
  not an ancestor of base, so ancestry-only checks either leak branches forever or
  delete unlanded work — is subtle, correct, and the kind of detail that prevents a
  data-loss bug.
- **Graded confidence in *Method*** honestly separates high-confidence Hermes
  release data from medium-confidence OpenClaw detail sourced from aggregators after
  the vendor page returned 403. That is the right way to handle uneven sourcing —
  it simply needs to be applied to gophermind's own column too.
- **Prioritizing self-improving skills over channel breadth** correctly identifies
  where Hermes's advantage actually lies.
- **The correction mechanism works.** The 2026-07-29 revision caught the prior
  draft's session-store undercount and its `openclaw-code-agent` mode/strategy
  confusion unprompted. It needs to run against code systematically, not once.

## Recommended actions

In priority order.

1. **Fix the fail-open judge — independently, before any phase work.**
   Add an on-error policy to `internal/safety/judge.go` (`deny` | `fallback`),
   defaulting to **deny**; correct the misleading doc comment; add a test asserting
   that a judge returning `error` with `safety.Auto` as fallback **denies**. Follow
   the `rbac.go` precedent, which already fails closed on an unknown role.
2. **Re-baseline the whole gophermind column** with `file:line` citations. Any cell
   that cannot be cited is marked *unverified* rather than scored. Seven errors
   surfaced across two passes; the bias is systematic, so assume incompleteness.
3. **Reconcile with `TODO-COMBINED.md`** — map each phase item to a backlog number
   or mark it genuinely new, and correct items #356 and #392.
4. **Add sizing and acceptance criteria**, plus the reuse table above, so
   implementers start from existing code rather than rebuilding seams.
5. **Write the Phase 3 risk section** — orphan cleanup, disk bounds, conflict
   handling, `dismissed` recovery.
6. **Delete Phase 8-4**; re-scope 9-1 to "wire existing `UsageSnapshot` +
   `Capabilities.ContextWindow` into the TUI status line."

## Verification

- **Fail-open reproduction (gates action 1):** with `GOPHERMIND_JUDGE=1`, no
  `.gophermind/policy`, and approval mode unset, point the judge at an unreachable
  endpoint and confirm a gated mutating tool is approved. That reproduction must
  fail after the fix.
- `go test ./internal/safety/...` for the new fail-closed test.
- `make test`, `go vet ./...`, `gofmt -l .` for the repo gate.
- Matrix re-baselining is verified by review, not tests: every gophermind cell
  should carry a citation a reader can `grep`.
