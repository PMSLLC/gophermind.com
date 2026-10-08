# Goal: one planner, one executor, one run state, built end to end

Set by John on 2026-10-08. Delegated run: no review gates between tasks, one report at the end. Start it with the `goal` skill. The map of every document named below is `docs/unification-overview.md`.

Updated 2026-10-08 (later): John ordered the unification to run **before** the graded run ("run the unification first, skip the gate"). The gate of the first version of this document is removed, and the graded run of `docs/GOAL.md` is now the last big task of this goal (task 11).

## What finishing means

All of this is done, on the feature branch, tested, documented and reported:

- Sub-project A: run state as browsable files, no SQLite, scrubbed attempt artifacts.
- Sub-project B: every node carries every group, questions are asked as a tree in rounds, every question and answer is recorded in the node tree.
- Sub-project C: `/project` is the single entry point and drives the one system.
- Sub-project E: a planner revise stage rewrites a node after a failure.
- Sub-project F: the executor runs leaves in parallel in isolated trees.
- Sub-projects D and G: the `runview` library and the new pipeline contract; the server and the desktop app upgraded to it; v1 and `/phase` removed; the version names dropped.
- The graded run of `docs/GOAL.md`: AIVentureStudio built from its brief in one clean `/project` run on the unified system, and the perfect release tagged `v0.1.0` in the target repo by that document's own rule.

## Inputs

- The map: `docs/unification-overview.md`. The umbrella design: `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md`.
- Specs: `2026-10-07-node-widening-and-question-model-design.md`, `2026-10-08-retire-v1-and-upgrade-consumers-design.md` (in this repo's `docs/superpowers/specs/`), and on the feature branch `2026-09-29-v2-planner-core-design.md`, `2026-09-30-v2-executor-design.md`, `2026-09-30-project-single-entry-design.md`.
- Plans: `docs/superpowers/plans/2026-10-07-filesystem-run-state.md` (A, 9 tasks), `docs/superpowers/plans/2026-10-08-node-widening-and-question-model.md` (B, 13 tasks), and on the branch `2026-09-30-project-single-entry.md` (C, 14 tasks). D and G have a spec and no plan; E and F have neither a spec nor a plan. Writing them is part of the work (tasks 5, 6 and 7).
- Reviews: `docs/reviews/2026-10-07-*.md`, `docs/reviews/2026-10-08-v1-caller-inventory.md`.
- Code: the branch `feat/briefv2-planner-core` in the linked worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`. This checkout (`main`) holds docs only.
- Model for GopherMind's own work: the Mac mini (Ollama `qwen3.6:35b-a3b` at `192.168.1.35:11434`, VPN fallback `10.8.0.6`). Only the smoke and regression runs of task 11 call it.

## Rulings already taken (do not reopen)

- Sequencing (John, 2026-10-08): the unification runs first and the graded run after it. Decision 11 of the umbrella spec is reversed. `docs/GOAL.md` stays authoritative for the graded run itself.
- One planner, one executor, one state store; no v1 and v2; `/phase` and PhaseFlow are retired.
- One new node-based pipeline contract, `schema_version` 1; the server and the app are released together; no compatibility layer.
- The root package after the rename is `gophermind-lib/build`; docs move to `docs/build/`. The CLI names (`gophermind brief ...`, `/project`) do not change.
- `CREDITS.md` keeps a thank-you line for PhaseFlow's author, marked as no longer part of the code.
- Explicit, validated not-applicable answers are allowed where the node-group table says so; node stages follow each node's `model_tier`.
- Event streams are capped at 16; polling interval 250 ms; runs are not pruned.
- Where a spec is silent, choose what makes a dropped requirement or a silent failure impossible over what is faster, and record the ruling in the task's commit message.

## Execution rules

- Subagent-driven development, as John's standing rule says. Every subagent runs on Sonnet.
- All code work goes in the linked worktree, on `feat/briefv2-planner-core`, one writer at a time. Commit and push the feature branch after each task. Docs for this work are committed in this checkout.
- Never run `git worktree add`, `git checkout`, `git stash` or `--gw-force` in this checkout (the user's git wrapper blocks them). Use `git archive <sha> | tar -x -C <scratch>` for a scratch copy.
- Run tests with a throwaway home: `gt() { HOME="$(mktemp -d)" GOCACHE="$(go env GOCACHE)" GOMODCACHE="$(go env GOMODCACHE)" go test "$@"; }`. The executor package takes about 11 minutes: `-timeout 30m`. A test that shells out to `git` strips every `GIT_*` variable first.
- Tests run offline against canned or fake models. Only task 11 calls the mini. Check the mini's `memory_pressure` and swap before adding load; never load a second model.
- Do not merge into `main`, do not push `main`, do not tag a GopherMind release, do not deploy to the mini, do not install or notarise the desktop app. The one tag this goal makes is `v0.1.0` in the AIVentureStudio repo, by `docs/GOAL.md`'s perfect-release rule (task 11.2). Those are John's. The goal ends with a ready-to-merge branch and a written list of the steps (task 13).
- Never print a credential; never `rm -rf` a variable path without the guard (assign, test it is absolute and inside the intended folder, print it, then `rm -rf -- "${target:?}"`).
- No em dashes and no emojis in any text written.
- Commit messages end with the two attribution lines the harness gives.

## Claude token budget

Claude tokens are the cost to keep down.

- The orchestrator does not read a plan or spec in full. It finds a task's line range with `grep -n '^### Task'` and hands the subagent the path and the range.
- One subagent per plan task; its brief names paths and line ranges and never pastes file contents. One reviewer per task, reading the task's diff and its Review Focus lines only.
- A subagent's report is 150 words or fewer: the commit hash, the verify command and its pass or fail line, and any ruling. No diffs, no logs.
- Test and build output goes to a file under the scratchpad; read it with `grep` or `tail` for the failing lines.
- The orchestrator runs the task's one verify command and moves on; it does not repeat a check a subagent reported.
- Compact at task boundaries, after the commit. Write progress to `.superpowers/sdd/2026-10-08-unification/progress.md` (git-ignored) so a compaction loses nothing.

## Tasks

Run them in this order. A task is done when its plan's tasks are committed, its verify commands pass, and its documentation is updated.

0. **State of the graded run's prerequisites.** The gate of the first version is removed. Read `.superpowers/sdd/2026-09-30-v2-executor/HANDOFF.md` and the two progress ledgers in the worktree (`.superpowers/sdd/2026-09-30-v2-executor/progress.md`, `.superpowers/sdd/2026-09-29-v2-planner-core/progress.md`), and list which `docs/GOAL.md` tasks are done and which executor fix waves are still open (GOAL task 3). Write the list to `.superpowers/sdd/2026-10-08-unification/progress.md`. Task 1 finishes what is open.
1. **Baseline.** The worktree is clean; record the branch tip and run the whole suite once (`gt ./gophermind-lib/... ./cmd/... -count=1 -timeout 45m`, plus `gophermind-osx`'s own module) to a file; any failure that exists before this work is recorded, not fixed here, except that the executor fix waves still open from task 0 are finished first (one writer at a time, each with its tests), because the graded run needs them and A to C build on the executor. Spot-check the eight claims in `docs/reviews/2026-10-07-planner-executor-v1-v2-review.md` section 8 against the code; write the result as an addendum at the end of that file. If a claim is false and a plan's premise rests on it, correct that plan first.
2. **Sub-project A.** Execute the A plan (9 tasks). Verify: the plan's own final task (end-to-end on the greeter plan, the no-SQL grep gate, `go build ./...`).
3. **Sub-project B.** Execute the B plan (14 tasks; task 14 is the prompt-hygiene fix found by the task 1 claim check). Verify: its Task 13 (end-to-end tests, full suite) and Task 14.
4. **Sub-project C.** First amend the C plan for A and B exactly as the umbrella spec section 7 says (the preflight and clearing name the file layout; the unattended policy gains `ConfirmUnderstanding`; the report gains the node-class dimension), commit the amendment, then execute it (14 tasks). Verify: its final task, including a `/project` run on the csvstat brief in offline fake-provider mode.
5. **Sub-project E (planner revise stage).** Write the spec (`docs/superpowers/specs/<date>-planner-revise-stage-design.md`), then the plan, then build. Required behaviour: when a leaf has used its ladder, or reports a contract problem, the planner rewrites the node (description, contract text, tests, groups) within the approval rules (a changed signature or `depends_on` is `contract_flagged`, not a revision), bounded by `max_revisions`, recorded in the node's `revision` and in the questions-and-answers tree where a human decides; the executor consumes the rewritten node; the approval hash rule for revised node bodies is stated and tested. Acceptance test: a scripted fake model fails a leaf on every ladder entry, the revise stage rewrites it, the leaf then verifies; and a leaf that never verifies escalates after `max_revisions`. Use `docs/briefv2/handoff/prompts/06-revise.md` as the starting prompt.
6. **Sub-project F (parallel waves).** Write the spec, the plan, then build. Required behaviour: `executor.workers` above 1 runs leaves of a wave in parallel, each in an isolated working tree (so the stray-file check, the stub swap and the test run cannot race); landing order is deterministic and the work branch stays a linear history; a leaf's failure does not corrupt another's tree; the default stays 1 (the mini serves one request at a time). Acceptance test: a fixture of six independent leaves with `workers: 3` all verify, the commits are in a fixed order, no file outside a leaf's own paths changes, and `go test -race` passes.
7. **Sub-projects D and G: plan.** Write the plan for steps G1 to G7 of the G spec (`docs/superpowers/plans/<date>-run-view-and-retire-v1.md`), in the same format as the A and B plans, with real code in every step and tests first. Commit it.
8. **D and G, steps G1 to G4.** Execute the plan's tasks for `runview`, the server endpoints and dashboard, the desktop app, and the joint release build. Verify: the server tests, the app's client tests and `e2e_pipeline_test.go` against a real run folder; start the server locally on a loopback port and fetch every endpoint with a token; build the app with `gophermind-osx/build-app.sh` unsigned (do not install it).
9. **G5 to G7.** Remove v1 (TUI flow, `plantree`, `orchestrate`), retire `/phase` (the notice stub, delete `phaseflow`, the docs and the skill pack), then rename `briefv2` to `build` and `docs/briefv2` to `docs/build` in one mechanical commit. Verify: the removal gates of the G spec (no importer of the removed packages, no `briefv2` path, the stub prints its notice and exits 2), `go build ./...` and `go vet ./...` in the root module, `gophermind-lib` and `gophermind-osx`.
10. **Documentation sweep.** `README.md`, `docs/build/README.md`, `CHANGELOG.md` (the breaking changes: `/phase` removed, the pipeline contract, the file-based run state), `INDEX.md`, `docs/ARCHITECTURE-INDEX.md`, `docs/unification-overview.md` (every row's state, the paths after the rename, a list of renamed paths), `CONTEXT.md`. Run a search for stale statements: `briefv2`, `phaseflow`, `plantree`, `assignments.json`, `blackboard.db`, `.planning/` in live (non-historical) docs and code comments, and fix or justify each hit.
11. **Smoke and regression on the real system.**
    1. On the csvstat brief against the mini: `/project` unattended from a cleared state to a verified build. Check by file inspection: every function node has every group; the root's `decisions` equals the question store; `attempts/` holds scrubbed artifacts; `calls.jsonl` and `events.jsonl` exist; no `blackboard.db` anywhere; the pipeline endpoints show the run.
    2. **The graded run.** Execute `docs/GOAL.md`'s clean-run loop on the AIVS brief, to its perfect release, on the unified system. First check the human prerequisites of `2026-09-30-project-single-entry-design.md` section 16: the vault passphrase and a vault holding `DATABASE_URL` and `TEST_DATABASE_URL` that point at two empty PostgreSQL 15+ databases on loopback (for example `ssh -N -L 55432:127.0.0.1:5432 mini`), a `~/.gophermind/gophermind.yaml` with `privacy.mode: private_only` and the mini in every tier, and the installed `gophermind-dev` binary. This goal cannot create them: if one is missing, write the exact list to the report and stop here with everything before it done. Otherwise run the loop: clear the target repo, one `/project` run, no restart, no hand edit; a failed attempt is dead, recorded in `docs/goals/attempts/NN.md`, GopherMind is fixed (never the target repo), and the loop runs again until the perfect release of `docs/GOAL.md` or an escalation condition below. On a perfect release, tag that commit `v0.1.0` in the target repo; that tag is the only thing added there.
12. **Final verification matrix.** All of: the full suite in all three Go modules passes; `go vet ./...` clean; `gofmt -l` prints nothing; the grep gates pass; `gophermind brief validate` on both briefs prints no warning; the offline `brief plan --fake` and `brief run` on the greeter fixtures pass; the server's endpoints and the app's client tests pass.
13. **The report, then stop.** Write `docs/goals/unification/report.md` (150 lines at most): what was built per sub-project with commit ranges, the verification results, any ruling made and why, anything left undone, and the numbered steps John must take: review and merge the branch (the dry-run merge result), deploy the server to the mini (`git pull` there, `sudo launchctl kickstart -k system/com.home.<service>`, then check the endpoints), build, sign and notarise the app and release it in the same window, tag the release. Commit it here and stop.

## Stop and escalate

Stop and write the reason, the evidence and the next step to the report file when:

- A human prerequisite of the graded run is missing at task 11.2 (write the list; everything before it stays done).
- A task still fails its verify command after three fix rounds on the same failure (spec or plan wrong, or the code): record the failure in `docs/goals/unification/blocked.md` and stop; do not skip the task, because every later task depends on the earlier ones.
- The git wrapper blocks a command this goal needs and the workaround above does not apply.
- The mini runs out of memory or swap, or Ollama stops answering and a restart through launchd does not bring it back (task 11 only).
- The AIVS regression attempt fails for 20 revisions of the same cause (the same limit `docs/GOAL.md` uses).
- A step would need an action forbidden in "Execution rules" (merge, push `main`, deploy, install, tag a GopherMind release).
