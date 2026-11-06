# Resume from step: close-out report (2026-11-06)

Design: `docs/superpowers/specs/2026-11-05-resume-from-step-design.md`. Plan: `docs/superpowers/plans/2026-11-05-resume-from-step.md`. Range: c8268970..16e55c82 on `main` (not pushed). Installed in the local service at 16e55c82 on 2026-11-06 01:34 MST.

## What a person can do now

- `gophermind brief resume <run-id> --from <step> [--reset-repo]` re-enters a run at a planner stage (`clarify`, `confirm`, `contract`, `decompose`, `enrich`, `coverage`, `approve`, `testwriter`) or at one executor node (`node:<id>`).
- The run page of the local service shows a "Resume from this step" control on a finished run, backed by `POST /app/briefs/{id}/resume?from=<step>`; `GET /app/briefs/{id}/state` carries `resume_points` (always an array).
- A stage resume moves that stage's state files and everything downstream into `_state/discarded/<timestamp>/` (moves only, never deletes) and, for the service's own clones, resets the clone to the base branch. For a person's repository, `--reset-repo` restores only the plan's files, deletes only a run-owned branch with no foreign commits, and refuses by name any file holding the person's uncommitted change.
- A node resume returns the row to `ready` with copies of its attempts, old status and leaf result in the discard dir; `brief run` picks it up again.
- Every resume refuses while a run is active (a claimed or in-progress row), and refuses a run that already landed on its base branch.

## Commits

| Task | Commit | Content |
|---|---|---|
| 1 | 4995579b, c034931c, d0131462 | `build/resume`: steps, discard tables, planner-stage move-aside; the enrich stage entered without `enriched.json` enriches every node |
| 2 | 86a81b7f, 5f47e54c | blackboard `Reset`; `FromNode`, `Points`; attempts and leaf-result copies written before the reset |
| 3 | 00d51207, f548bd4c, 9e0995d3 | repository reset (`RepoKeep`, `RepoOwnedClone`, `RepoRestoreFiles`); gitland `ForeignCommitsBetween` and exported hardened prefix; repo-root and git-dir checks; landed-run and uncommitted-change refusals |
| 6 | 906d8de9, b9058627 | hello r003 shape test |
| 5 | 1a4ef130, df3e907c | service endpoint, record fields, `resume_points`, run-page control, runview `reset` event |
| 4 | ce2f6778, 564112f0 | CLI flags; gate validated before the reset; stage path and usage pinned |
| final | 16e55c82 | stage resume refuses a live run; service logs the planned clone reset, the discarded set and the repo actions; owned clone drops its work branch; shared-root node resume runs in the root; runview attempt counter reset |

## Tests run

- Per task: `build/resume`, `build/blackboard`, `build/gitland`, `serve`, `runview`, and `cmd/gophermind -run 'BriefResume|BriefUsage'`, red then green.
- Full `gophermind-lib` at df3e907c: build, vet, gofmt clean; 84 packages ok, exit 0 (executor 3526 s under load, planner 252 s, serve 51 s).
- Root module (`cmd/gophermind`, whole package) at 564112f0 and again at 16e55c82: ok, exit 0, gofmt clean.
- Installed service smoke check: `resume_points` lists the eight stages on a finished run and adds `node:fn-greeting` on the failed run hello-v1.1-20261106-r009.
- Load-timing flakes in `serve` seen once each under parallel load and passing alone: TestBriefEventsEndsWhenRunIsFinal, TestRunPreviewTokenMatchStartsRun, TestBriefResubmitStartsNewRun.

## Parked findings (rulings in the execution ledger, kept here because the ledger is scratch)

- A live planner process (a `brief resume` or `brief plan` without a blackboard claim) is not detected by a stage resume; a run-level lock file held by `brief plan`, `brief resume` and `brief run` would close the gap.
- Off the work branch, `--reset-repo` refuses once wave 0 has committed the test files (the strict plan load wants them in the tree); the refusal should say "check out <work branch> and pass --reset-repo again".
- `Points` offers steps the server will refuse (a landed run, a shared-root run with executor state) and treats stale claims after a hard kill as live; `Points` should take the repo mode and compare heartbeats with the stale-claim window.
- A resume at coverage keeps coverage's own additions to `contracts.json` and `decomposed.json`; only a resume at `contract` undoes them (a warm restart, by the letter of the design).
- `repoPath` accepts paths under `.git/` and `.gophermind/`, and the run-dir `inside` check is lexical; both inputs are harness-written; route them through `pathsafe` and refuse a symlinked `_state`.
- Node resume points are labelled by status, not by the node title; node-resume tests use the flat board layout production never uses.
- A stage resume through the service always passes `--gate file`, so a run uploaded without `ask` that meets a question is recorded failed; `resumed_at` and `resumed_from` are stored but not yet shown on the page or list.
- The landed-run check walks the base branch's whole history for the run trailer; `merge-base --is-ancestor <State.Tip>` bounds it when the executor recorded a tip.
- The serve test for the logged clone reset matches the `planned:` line before the action line; the `planned:` lines use the unresolved repo path while the action lines use the resolved one.
- A stage marker means the stage began, not completed, so the "completed" wording in a refusal can overstate; "restored f" is logged even when f already matched base.
