# Parallel Waves in the Executor Implementation Plan (sub-project F)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** With `executor.workers` above 1, the leaves of one level run at once, each in a private copy of the repository with its own throwaway git repository, and land one at a time in id order by replaying the saved reply in the main tree with the full leaf check, so the work branch stays linear and its commits are in the serial order; with the default of 1 nothing changes.

**Architecture:** A level that may run in parallel is copied once (`<scratch>/base`) in a serial phase; each leaf copies the base into `<scratch>/trees/<id>`, made a repository by `gitland.NewScratch`, with its own HOME (`<scratch>/w/<id>`) and sandbox profile, carried by a `workTree` value that the leaf loop reads instead of `rc.o.Repo`, `rc.git` and `rc.chk`. A pass in a private tree saves `_state/candidate-<id>` and waits on a `turn` (a chain of `done` channels in id order that also takes the scheduler's body lock); the turn holder moves the leaf to the main tree, replays the candidate through the reply gate and the leaf check, and commits through the unchanged `finishPass`. A landing failure is a charged attempt and the leaf continues its ladder in the main tree; the revise rung always runs in the main tree. Resume lands saved candidates with no model call.

**Tech Stack:** Go (module `gophermind`, go 1.25, packages under `gophermind-lib/briefv2/`), standard library only, git through `gitland`'s fixed-argument runner; offline scripted providers and the fake `Checker` in tests, one real-runner acceptance test.

**Spec:** `docs/superpowers/specs/2026-10-08-parallel-waves-design.md` (binding; this checkout). Scope: `docs/GOAL-UNIFICATION.md` task 6. Context: `docs/unification-overview.md`.

## Global Constraints

- **Preconditions.** Sub-projects A and E are built on the branch `feat/briefv2-planner-core` at `de24662` (E's final review fix wave). Task 1 step 1 checks the tip and a clean tree.
- All code work happens in the linked worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core` on branch `feat/briefv2-planner-core`, **one writer at a time**. This plan lives in the docs-only checkout (`/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com`). Never run `git worktree add`, `git checkout`, `git stash` or `--gw-force`. **The branch is never merged into `main` and never pushed in this goal.** Paths below are relative to the worktree root unless they start with `docs/` in the main checkout (said explicitly). `B2` abbreviates `gophermind-lib/briefv2`.
- Run tests with a throwaway home that is deleted even when the tests fail (the disk is nearly full). Define once per shell, run from the worktree root:

```bash
gt() { local gc gm h rc; gc="$(go env GOCACHE)"; gm="$(go env GOMODCACHE)"; h="$(mktemp -d)"; HOME="$h" GOCACHE="$gc" GOMODCACHE="$gm" go test "$@"; rc=$?; rm -rf -- "${h:?}"; return $rc; }
TRAILERS=$'Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>\nClaude-Session: https://claude.ai/code/session_01JAphVLY8SVrEJ6E5YXqCXB'
```

(An executor in another session puts its own session URL in the second trailer line.)

- **The executor package takes 20 to 25 minutes.** Always pass `-timeout 45m -parallel 2` to it. Iterate with a narrow `-run`. The whole package runs three times in this plan: end of Task 7, and twice in Task 12 (plain, then `-race`). Other packages are fast: run them whole.
- **Known flake.** `TestAcceptanceInterruptedIsNotAPass` (`B2/executor/finish_test.go:234`) fails under load. When it is the only failure of a package run, rerun it alone: `gt ./gophermind-lib/briefv2/executor/ -run '^TestAcceptanceInterruptedIsNotAPass$' -count=3 -timeout 45m`. Three passes mean it was the flake; say so in the commit body. Any other failure is real.
- **osx tests** always need `-skip TestKeychain_RealStoreRoundTrip` (`gophermind-osx/auth/manager_test.go`).
- **Race.** Every task that adds or changes concurrent code (Tasks 1, 4, 5, 6, 7, 8, 9, 10, 11) runs its tests with `-race -count=3` before it commits. A data race is a failure, never a flake.
- **Offline only.** Every model call in a test goes to the scripted provider of `B2/executor/harness_test.go` (`Script`, keyed by stage). A stage with no reply left fails the test with "no reply left for stage". Nothing calls the mini.
- `gofmt -w` every Go file you touch before its tests; `gofmt -l gophermind-lib cmd` prints nothing at each commit; `go vet` of every touched package is clean.
- A test that shells out to `git` uses `g.gitCmd` (the executor's `TestMain` strips `GIT_*`) or the gitland helpers.
- **Privacy.** No new prompt is added. Implement and revise prompts stay node scope, built by the existing packer; the landing makes no model call; a landing failure reaches the next prompt only through `failureOf` (scrubbed) or a fixed note, exactly as a serial failure does.
- **Secrets.** No secret value in any new file, event, warning, error, report field or commit. A candidate file holds source that passed the reply gate (which refuses a secret) and is deleted at landing; landing runs the gate again. Events carry ids, counts, fixed class words, a short hash or a scratch path relative to the scratch directory. pathsafe errors name a path by its length only. Canary test in Task 6.
- **Files.** Candidate files are written by `writeStateBytes` (mode `0600`, atomic). Scratch directories are made by `ensureDir` (mode `0700`, a link in their place refused). Copies inside a tree are `0644`, or `0755` when the source had an executable bit.
- **Recursive deletes.** Only `rc.removeScratch` (Task 4) deletes recursively in production code, and only after building the target, checking it is absolute, inside `rc.scratch`, one of `trees`, `w`, `base` (optionally `/<leaf-id>`), and emitting it. Tests use `t.TempDir()`. Task 12 has a static gate.
- **Explicit paths in every `git add`** (never `-A`, never `.`). Commit with `git commit -m "<subject>" -m "<rulings applied>" -m "$TRAILERS"`. The controller commits this plan file and the main-checkout doc edits of Task 12.
- No em dashes and no emojis in new text, code comments, messages or docs.
- Where the spec is silent or two statements cannot both hold, the plan chose what keeps the main tree single-writer and keeps a silent failure impossible over what is faster (`docs/GOAL-UNIFICATION.md`). Each ruling below is repeated in the commit body of the task that applies it.

## Rulings taken while grounding the plan in the code

Each ruling: the decision; why; the cost if it is wrong.

1. **The chain re-proof takes its read lock before it reads the memory (Task 1).** Fact from the re-review of E: `Plan.chainsIntact` (`B2/executor/plan.go:627-668`) copies `p.Revised` under `revMu` and only then takes `chainMu.RLock()`, so a whole transaction step that runs in between makes the files lead the memory and reads as a spurious `plan_changed`. Moving the `RLock` first is enough: writers write files under the write lock and move the memory after, so under the read lock the memory can lag the files and never lead them. `setTip`, `openTip` and `closeTip` stay outside `chainWrite`. Cost if wrong: a spurious `plan_changed` under `workers > 1`; F also confines every revision and every `planIntact` to the turn holder (ruling 6), so the window would need both defects.
2. **A level is copied once into `<scratch>/base` and every private tree is copied from the base, never from the live main tree (Task 4).** The spec copies "the main tree"; with fewer workers than leaves, later trees would be copied while an earlier leaf lands, which can catch a half-swapped stub. The base is made in the serial phase before the level's first job. Cost: one extra copy per parallel level and its disk space; removed when the level ends.
3. **`gitland.NewScratch(dir, runID)` takes no file list and stages with `git add -A` (Task 3).** The spec's signature has `files`; `add -A` over a directory that holds exactly the copy needs none, and staging a named tracked-but-ignored file would fail because the guard refuses `--force`. Cost: a file the main repo tracks although `.gitignore` matches it is untracked-and-ignored in the tree; a check that changes it is still reverted as stray in the tree, and the landing check sees the main tree.
4. **A symbolic link is copied only when its target is relative and stays inside the tree; any copy failure (such a link, a submodule, a special file, a full disk) or a level over 256 MiB makes the level serial with a `warning` (Tasks 3, 4, 7).** The spec says links are copied as links and is silent on their targets; a link out of the tree would let a leaf's check reach files outside its copy. Cost: a repository with an absolute link never runs a level in parallel.
5. **Main-tree jobs.** In a parallel level, a leaf that is `escalated` or `needs_revision`, is not stub-only in the main tree, has a reopen marker, or has a saved candidate runs in the main tree when its turn comes (`needsMain`, Task 7). The spec describes only the candidate case (section 11); adoption of a file on disk and the gate question both read and write the main tree. Cost: such leaves (resume only) gain no parallelism.
6. **The revise rung always runs in the main tree.** A leaf in a private tree that reaches the revise rung (notes or rewrite) first waits for its turn and moves to the main tree; the rest of its ladder runs there (Task 6). The spec (written before E built `rewrite`) is silent; a rewrite writes the test file, the node file, `leaf_tests.json` and a commit in the main tree. Cost: a leaf that needs a revision holds back the higher ids of its level until it ends.
7. **The gate may be asked from a private tree.** `toHuman` runs in the private tree under the gate mutex (decision F11); a retry continues the ladder there. Cost: none beyond F11.
8. **After a landing conflict the leaf restarts its current revision's ladder in the main tree** (first entry, fresh fix count), with the conflict as its previous failure. The spec says "continues its ladder"; the private ladder's position is not kept once the leaf leaves its tree. In the main tree a pass commits directly, so a leaf has at most one conflict. Cost: up to one extra ladder of model calls for a conflicted leaf.
9. **Landing runs the reply gate again before the swap (Task 6).** The candidate may come from `_state` after a kill or be edited while it waits; the gate also catches a helper name that a sibling landed first, with the declared names re-read from the main tree. Cost: the conflict class of that case is `malformed` (the gate's), not the spec example's `build`.
10. **The candidate file is JSON (revision, entry, tier, position, reply hash, source).** The spec says "candidate source"; the pass attempt recorded at landing needs the provider and model, and a candidate of an earlier revision must be recognisable. A candidate whose revision is not the row's is dropped with a `warning`; one that does not parse is removed and treated as absent. Cost: a few bytes more per candidate; no secret (gate-passed source).
11. **`finishPass` removes the candidate together with the reopen marker,** so a kill between the commit and the removal leaves nothing stale for resume to replay. Cost: none.
12. **Overlapping paths are split into sub-levels only when `executor.workers > 1`,** first fit in id order (`splitOverlaps`, Task 7). A serial run keeps today's exact order. Cost: for a defective plan (two leaves sharing a file) the commit order differs between `workers: 1` and more; a valid plan has no overlap.
13. **Report `schema_version` stays 3.** The spec says "stays 2": E raised it to 3 (`B2/report/report.go:34`) after the spec was written. `parallel` is additive and omitted when no level ran in parallel. Cost: none for readers of 3.
14. **Tree removal emits a `debug` event** with the path relative to the scratch directory (`trees/fn-greet`), only when something exists to remove, so a serial run emits nothing new. Cost: one event per removed tree in parallel runs; the CLI's sink ignores the kind.
15. **A turn takes `rc.sched.body`.** The turn holder of a parallel level holds the same mutex a serial leaf body holds, so the main tree has one writer by two mechanisms. Cost: none (the chain already orders them).
16. **The resume refusal names every live leaf.** One live leaf keeps today's text (`node <id> is held by a live worker`, pinned by `resume_test.go:172`); several read `nodes a, b are held by live workers`, ids sorted. Cost: none.
17. **Model concurrency for the preflight notice** is the largest `max_concurrent` among providers named in any chain of `models`. Cost: a chain the executor never uses could raise the figure and hide the notice.
18. **The six-leaf fixture is generated at test time from the greeter fixture** (`sixpackPlanner`, Task 10): four functions (`fn-cheer`, `fn-shout`, `fn-thank`, `fn-welcome`) cloned from `fn-greet` into `internal/greet`, so six independent leaves in one package. The spec asks for `executor/testdata/sixpack` "across two packages"; same-package is the stricter case (all six compile together at every landing), and the second package is exercised in the same run by `fn-bye` and `fn-hello` (`cmd/greeter`), a parallel level of wave 2. Generating keeps the fixture in step with the greeter. Cost: no six-wide level spans two packages.
19. **The six-leaf acceptance test runs the real runner** (sandbox off, so `checkerFor` returns the run's checker) wrapped by a recorder, and proves overlap with a two-party barrier at the first two private checks. Cost: a slower test (real `go test` per check).
20. **The goroutine check is its own non-parallel test (Task 9)**, not part of the determinism test: `runtime.NumGoroutine` counts the whole process, and parallel tests make it meaningless; Go pauses parallel tests while a sequential one runs. Cost: one more rig.
21. **The whole-package `-race` run uses `-timeout 90m -parallel 2` (Task 12).** The race detector slows the package several times; the concurrency subsets run at `-timeout 45m`. Cost: a hang in that one run takes longer to show.
22. **`--workers 0` (the flag's new default) means "use the setting"**; any other value outside 1 to 8 is refused before anything runs. Cost: none.
23. **Report counters are per invocation.** `parallel_levels` and `landing_conflicts` count what this process did; a resumed run reports its own levels, and `_state/executor.json` records the last `workers` used. Cost: a run killed and resumed reports the second half only.
24. **`workers` in the report is `executor.workers` as configured for the run**, not the per-level minimum (the `level_parallel` event carries that). Cost: none.

## Review Focus

The five input classes the spec implies but its test list does not exercise, most likely first. Each has its test in the owning task.

1. A repository holding a symbolic link whose target is absolute or leaves the repository: the level runs one leaf at a time with a `warning`, nothing is copied through the link, the run still verifies (Task 3 `TestCopyFileCopiesALinkInsideTheTreeAndNeverFollowsOne`, Task 4 `TestSnapshotLevelRefusesALinkOutOfTheTreeAndAnOversizedTree`, Task 7 `TestALinkOutOfTheTreeRunsTheLevelSerially`).
2. A saved candidate that is stale (written for an earlier revision) or tampered (edited to import `os/exec` and to hold a secret) by the time it lands: it is refused or dropped, nothing of it is committed or stored, and the ladder makes a fresh attempt (Task 6 `TestATamperedCandidateIsRefusedAtLanding`, `TestAStaleCandidateIsDroppedAndTheLadderRuns`).
3. What a killed run leaves under the scratch directory: a read-only directory inside an old tree and a symbolic link planted in place of `w/`: the next start removes them without following the link and without failing (Task 4 `TestLeftoverTreesOfAKilledRunAreRemovedWithoutFollowingALink`).
4. A level with more leaves than workers: never more than `workers` trees on disk or checks in flight, and no tree or base left after the level (Task 10 `TestMoreLeavesThanWorkersNeverHoldMoreTrees`).
5. Two leaves of one level sharing a path (a plan defect): the later id runs in a following sub-level after the earlier one landed, never in parallel with it (Task 7 `TestSplitOverlapsPutsASharedPathInALaterSubLevel`, `TestOverlappingLeavesOfALevelLandOneAfterTheOther`).

## File Structure

```text
B2/executor/
  plan.go                                 EDIT chainsIntact takes the read lock first (Task 1)
  chain_lock_test.go                      NEW  (Task 1)
  options.go, options_test.go             EDIT CheckWorkers range (Task 2)
  worktree.go, worktree_test.go           NEW  workTree, base copy, makeTree, dropTree, removeScratch, profile (Task 4)
  env.go                                  EDIT envAt, leafCheckIn; rc.checkLeaf removed (Tasks 4, 5)
  start.go                                EDIT runCtx fields, profile kept, cleanTrees, preflight notice, gate mutex (Tasks 4, 6, 7, 8)
  turn.go, turn_test.go                   NEW  the landing order (Task 5)
  candidate.go, candidate_test.go         NEW  _state/candidate-<id> (Task 5)
  leaf.go                                 EDIT leafRun on a workTree, private pass, landing, hand-off (Tasks 5, 6)
  leaftree_test.go                        NEW  (Task 5)
  land.go, land_test.go                   NEW  privatePass, toMain, land, resumeCandidate (Task 6)
  schedule.go, schedule_test.go           EDIT workerCount, splitOverlaps, needsMain, levelWorkers, runJobs (Task 7)
  parallel_test.go                        NEW  scheduler tests and shared helpers (Task 7)
  escalate.go, resume.go, run.go, state.go EDIT gate mutex, live list, report, state workers (Task 8)
  parallel_gate_test.go                   NEW  (Task 8)
  parallel_race_test.go                   NEW  goroutine check (Task 9)
  rig_test.go                             EDIT newRigIn passes PlannerDirs (Task 10)
  sixpack_test.go, parallel_accept_test.go NEW (Task 10)
  parallel_kill_test.go                   NEW  (Task 11)
B2/settings/executor.go, executor_test.go EDIT MaxWorkers, range 1 to 8 (Task 2)
B2/gitland/gitland.go, cli.go, scratch_test.go  EDIT/NEW Files, NewScratch (Task 3)
B2/pathsafe/pathsafe.go, copy_test.go     EDIT/NEW CopyFile (Task 3)
B2/report/report.go, summary.go, parallel_test.go  EDIT/NEW Parallel (Task 8)
B2/ledger/fs_test.go                      EDIT eight-writer test (Task 9)
cmd/gophermind/brief_run.go, brief_run_test.go  EDIT --workers (Task 2)
docs (worktree): docs/briefv2/README.md, docs/superpowers/specs/2026-09-30-v2-executor-design.md (Task 12)
docs (main checkout): docs/unification-overview.md, docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md, docs/superpowers/specs/2026-10-08-parallel-waves-design.md (Task 12)
```

---

## Task 1: The chain re-proof reads the memory under the chain lock

**Files:**
- Modify: `B2/executor/plan.go:621-639` (`chainsIntact`, its comment and the lock order)
- Create: `B2/executor/chain_lock_test.go`

**Interfaces:**
- Consumes: `Plan.chainMu`, `Plan.revMu`, `Plan.setTip`, `rc.applyRevision`, `rc.afterRevStep`, `errRevisionKilled` (`B2/executor/revision.go:20`), the test helpers `g.proposal` and `greetTest` (`B2/executor/revision_test.go:18,66`).
- Produces: no new name. Lock order from here on: `chainMu` before `revMu`; nothing takes `revMu` and then `chainMu`.

- [ ] **Step 1: Confirm the base.** From the worktree root:

```bash
git rev-parse --short HEAD && git status --short | head
```
Expected: `de24662` (or a later commit of this branch) and no output from `status`. Stop and report if the tree is dirty.

- [ ] **Step 2: Write the failing tests** in `gophermind-lib/briefv2/executor/chain_lock_test.go`:

```go
package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/revlog"
	"gophermind/gophermind-lib/briefv2/runner"
)

// A revision step that runs entirely between chainsIntact's copy of the
// in-memory tips and its read lock makes the files lead the memory: a spurious
// plan_changed (re-review of sub-project E). The reader must take the lock
// before it reads the memory.
func TestChainsIntactReadsTheMemoryUnderTheChainLock(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")} // the red check of the new test
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	rc.afterRevStep = func(n int) error {
		if n == 6 {
			return errRevisionKilled
		}
		return nil
	}
	if _, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted); err != errRevisionKilled {
		t.Fatalf("applyRevision = %v", err)
	}
	rc.afterRevStep = nil
	dir, _ := revlog.NodeDir(g.runDir, leafID)
	recs, _ := revlog.Read(dir)
	commit := recs[0]
	commit.Kind = revlog.KindCommit
	// The memory as a reader saw it before the intent: no tip for the node.
	rc.plan.revMu.Lock()
	delete(rc.plan.Revised, leafID)
	rc.plan.revMu.Unlock()

	// A writer holds the chain lock, as a transaction step does, and finishes
	// the commit (record and tip) before it lets go.
	rc.plan.chainMu.Lock()
	got := make(chan bool, 1)
	go func() { got <- rc.plan.chainsIntact() }()
	time.Sleep(200 * time.Millisecond) // the reader is now blocked on the lock
	if err := revlog.Append(dir, commit); err != nil {
		rc.plan.chainMu.Unlock()
		t.Fatal(err)
	}
	rc.plan.setTip(leafID, RevisedTip{Key: rc.plan.nodeKey(leafID), N: commit.N, BodySHA256: commit.BodySHA256, TestSHA256: commit.TestSHA256After})
	rc.plan.chainMu.Unlock()
	if !<-got {
		t.Fatal("chainsIntact compared a memory read before the lock with files written under it")
	}
}

// planIntact on one goroutine never fails while a whole revision transaction
// runs on another (the race detector watches every shared field).
func TestPlanIntactNeverFailsWhileARevisionRunsOnAnotherGoroutine(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	l := rc.plan.Leaf(leafID)
	fc.LeafScript[leafID] = []runner.Verdict{failVerdict("test_fail", "x", "TestGreet")}
	prop := g.proposal(t, leafID, "REVISED-SENTENCE for the greeting.", greetTest)
	stop := make(chan struct{})
	bad := make(chan string, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if se := rc.planIntact(); se != nil {
				bad <- se.Reason
				return
			}
		}
	}()
	_, err := rc.applyRevision(context.Background(), l, prop, planner.TriggerLadderExhausted)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-bad:
		t.Fatalf("planIntact failed (%s) while a revision ran", r)
	default:
	}
}
```

- [ ] **Step 3: Run the first test and see it fail.**

```bash
gofmt -w gophermind-lib/briefv2/executor && gt ./gophermind-lib/briefv2/executor/ -run '^TestChainsIntactReadsTheMemoryUnderTheChainLock$' -count=1 -timeout 45m
```
Expected: FAIL with `chainsIntact compared a memory read before the lock with files written under it`.

- [ ] **Step 4: Implement.** In `gophermind-lib/briefv2/executor/plan.go` replace the comment and the first lines of `chainsIntact` (lines 621-639) so the read lock is taken first:

```go
// chainsIntact proves every revision chain again with verifyRevisions, as
// LoadPlan does (hashes and the frozen-field check, no test run): body-0 must
// still be the revision 0 the plan was loaded with, and each chain's committed
// revision must be the one this run holds, or the open intent's (a commit
// record written, the tip not yet moved). The read lock is taken before the
// memory is copied: a writer writes the files under the write lock and moves
// the memory after, so under the read lock the memory can lag the files and
// never lead them. Lock order: chainMu, then revMu.
func (p *Plan) chainsIntact() bool {
	p.chainMu.RLock()
	defer p.chainMu.RUnlock()
	p.revMu.Lock()
	mem := make(map[string]RevisedTip, len(p.Revised))
	for id, t := range p.Revised {
		mem[id] = t
	}
	p.revMu.Unlock()
	paths := make(map[string]string, len(p.keys))
	for id, key := range p.keys {
		paths[id] = strings.TrimPrefix(key, "tree/")
	}
	tests, err := planner.ReadLeafTests(p.RunDir)
```

The rest of the function (from `if err != nil { return false }` after `ReadLeafTests`) is unchanged; the old `p.chainMu.RLock()` and `defer p.chainMu.RUnlock()` after the `paths` loop are deleted.

- [ ] **Step 5: Prove the lock order.** Nothing may take `revMu` and then `chainMu`:

```bash
grep -n 'chainWrite(func' gophermind-lib/briefv2/executor/*.go | grep -v _test.go
```
Expected: every closure listed calls only `revlog.Stage`, `revlog.Append`, `revlog.RemoveStaged`, `pathsafe.ReplaceTest`, `planner.SetLeafTestSHA` or `tree.NewStore(...).Write`, none of which takes `revMu` (`revision.go:90,97,106,191,211,231,247,278,283,287`, `revise_rewrite.go:56`). Report and stop if another closure appears.

- [ ] **Step 6: Run both tests and the revision tests with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/ && gt ./gophermind-lib/briefv2/executor/ -run 'TestChainsIntact|TestPlanIntactNeverFails|TestApplyRevision|TestFinishCommitted|TestAbortRevision' -race -count=3 -timeout 45m -parallel 2
```
Expected: `ok`, no `WARNING: DATA RACE`.

- [ ] **Step 7: Commit.**

```bash
git add gophermind-lib/briefv2/executor/plan.go gophermind-lib/briefv2/executor/chain_lock_test.go
git commit -m "fix(executor): chainsIntact reads the tip memory under the chain lock (sub-project F task 1)" -m "Ruling 1: the read lock is taken before the memory is copied; setTip, openTip and closeTip stay outside chainWrite; lock order chainMu then revMu." -m "$TRAILERS"
```

## Task 2: `executor.workers` 1 to 8, `CheckWorkers` and `--workers`

**Files:**
- Modify: `B2/settings/executor.go:92-120`, `B2/settings/executor_test.go:274-289`
- Modify: `B2/executor/options.go:87-95`, `B2/executor/options_test.go:109-115,167-185`
- Modify: `cmd/gophermind/brief_run.go:93-108,200-201`, `cmd/gophermind/brief_run_test.go:435-444`

**Interfaces:**
- Produces: `const settings.MaxWorkers = 8`; `func executor.CheckWorkers(n int) error` returns `--workers must be between 1 and 8` outside the range; the CLI flag `--workers` defaults to 0, meaning the setting.

- [ ] **Step 1: Write the failing tests.** In `gophermind-lib/briefv2/settings/executor_test.go` replace `TestExecutorWorkersMustBeOne` (lines 274-289) with:

```go
// executor.workers is 1 to MaxWorkers (8). A bad value names the key and the
// range, never the value; the default stays 1.
func TestExecutorWorkersRange(t *testing.T) {
	if settings.MaxWorkers != 8 {
		t.Fatalf("MaxWorkers = %d, want 8", settings.MaxWorkers)
	}
	for _, n := range []int{1, 2, 8} {
		c := settings.Default()
		c.Executor.Workers = n
		if err := c.Validate(); err != nil {
			t.Errorf("workers %d refused: %v", n, err)
		}
	}
	for _, n := range []int{0, 9, 64, -1} {
		c := settings.Default()
		c.Executor.Workers = n
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "executor.workers must be between 1 and 8") {
			t.Errorf("workers %d: %v, want the key and the range", n, err)
		}
	}
	if c := settings.Default(); c.Executor.Workers != 1 {
		t.Errorf("the default is %d, want 1", c.Executor.Workers)
	}
}
```

In `gophermind-lib/briefv2/executor/options_test.go` replace the subtest `more than one worker is refused` (lines 109-115) with:

```go
	t.Run("workers above 8 are refused, 3 is accepted", func(t *testing.T) {
		o := validOptions(t)
		o.Settings.Executor.Workers = 9
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "executor.workers must be between 1 and 8") {
			t.Errorf("workers 9: %v", err)
		}
		o = validOptions(t)
		o.Settings.Executor.Workers = 3
		if err := o.validate(); err != nil {
			t.Errorf("workers 3: %v", err)
		}
	})
```

and replace `TestCheckWorkers` (lines 167-185) with:

```go
// The CLI checks a --workers value it was given against the setting's range.
func TestCheckWorkers(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, -1, 9, 1000} {
		if err := CheckWorkers(n); err == nil || err.Error() != "--workers must be between 1 and 8" {
			t.Errorf("CheckWorkers(%d) = %v, want the range refusal", n, err)
		}
	}
	for _, n := range []int{1, 3, 8} {
		if err := CheckWorkers(n); err != nil {
			t.Errorf("CheckWorkers(%d) = %v, want nil", n, err)
		}
	}
}
```

In `cmd/gophermind/brief_run_test.go` replace the loop at lines 435-444 with:

```go
	for _, n := range []string{"9", "-1"} {
		t.Run("--workers "+n+" is 1 with no model call", func(t *testing.T) {
			id, _ := plannedGreeter(t)
			sp := useRun(t, goodReplies(t, ""))
			code, _, errs := runBriefCmd(t, "", "run", id, "--workers", n)
			if code != 1 || !strings.Contains(errs, "error: --workers must be between 1 and 8") || sp.Calls() != 0 {
				t.Errorf("code=%d err=%q calls=%d", code, errs, sp.Calls())
			}
		})
	}
```

and add after `TestBriefRunRepoOverride`:

```go
// --workers n sets executor.workers for this run only; 0 and no flag keep the setting.
func TestBriefRunWorkersFlagOverridesTheSettingForThisRun(t *testing.T) {
	id, _ := plannedGreeter(t)
	useRun(t, goodReplies(t, ""))
	var got executor.Options
	stubExecutor(t, func(_ context.Context, o executor.Options) (executor.Report, error) {
		got = o
		return summaryReport(id, "verified", ""), nil
	})
	if code, _, errs := runBriefCmd(t, "", "run", id, "--workers", "3"); code != 0 || got.Settings.Executor.Workers != 3 {
		t.Fatalf("--workers 3: code=%d err=%q workers=%d", code, errs, got.Settings.Executor.Workers)
	}
	if code, _, errs := runBriefCmd(t, "", "run", id, "--workers", "0"); code != 0 || got.Settings.Executor.Workers != 1 {
		t.Fatalf("--workers 0: code=%d err=%q workers=%d, want the setting (1)", code, errs, got.Settings.Executor.Workers)
	}
	if code, _, errs := runBriefCmd(t, "", "run", id); code != 0 || got.Settings.Executor.Workers != 1 {
		t.Fatalf("no flag: code=%d err=%q workers=%d, want the setting (1)", code, errs, got.Settings.Executor.Workers)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/settings/ -run TestExecutorWorkersRange -count=1
gt ./gophermind-lib/briefv2/executor/ -run 'TestCheckWorkers|TestOptionsValidate' -count=1 -timeout 45m
gt ./cmd/gophermind/ -run 'TestBriefRunExitCodes|TestBriefRunWorkersFlag' -count=1 -timeout 15m
```
Expected: compile failure `undefined: settings.MaxWorkers`; then the old refusal text in the executor and CLI tests.

- [ ] **Step 3: Implement.** In `gophermind-lib/briefv2/settings/executor.go` add above `validate`:

```go
// MaxWorkers is the largest executor.workers: leaves of one level run at once,
// each in a private copy of the repository (sub-project F).
const MaxWorkers = 8
```

change the table entry `{"workers", e.Workers, 64},` to `{"workers", e.Workers, MaxWorkers},` and delete lines 115-120 (the comment and the `if e.Workers != 1 { ... }` block).

In `gophermind-lib/briefv2/executor/options.go` replace `CheckWorkers` (lines 87-95):

```go
// CheckWorkers is the check the CLI makes on a --workers value it was given
// (the flag's 0 means "use executor.workers" and is not passed here). The
// range is the setting's.
func CheckWorkers(n int) error {
	if n < 1 || n > settings.MaxWorkers {
		return fmt.Errorf("--workers must be between 1 and %d", settings.MaxWorkers)
	}
	return nil
}
```

(`errors` stays imported: `validate` uses it.)

In `cmd/gophermind/brief_run.go` change line 93 to `workers := 0`, line 98 to

```go
		fs.IntVar(&workers, "workers", 0, "leaves run at once (default from settings, 1)")
```

replace lines 105-108 with

```go
	if workers != 0 {
		if err := executor.CheckWorkers(workers); err != nil {
			fmt.Fprintf(errw, "error: %v\n", err)
			return exitError
		}
	}
```

and replace line 201 (`runCfg.Executor.Workers = workers`) with

```go
	if workers != 0 {
		runCfg.Executor.Workers = workers // this run only: the settings file is not touched
	}
```

(`runCfg := *cfg` on line 200 is a copy, so the override never reaches the loaded config.)

- [ ] **Step 4: Run.**

```bash
gofmt -w gophermind-lib/briefv2/settings gophermind-lib/briefv2/executor cmd/gophermind && go vet ./gophermind-lib/briefv2/settings/ ./gophermind-lib/briefv2/executor/ ./cmd/gophermind/
gt ./gophermind-lib/briefv2/settings/ -count=1
gt ./gophermind-lib/briefv2/executor/ -run 'TestCheckWorkers|TestOptionsValidate|TestWorkerCountIsAlwaysOne|TestNeverTwoLeafBodiesAtOnce' -count=1 -timeout 45m
gt ./cmd/gophermind/ -run 'TestBriefRun' -count=1 -timeout 15m
grep -rn 'until leaf-isolated trees exist' gophermind-lib cmd
```
Expected: `ok` for each; the grep prints nothing. (`TestWorkerCountIsAlwaysOne` and `TestNeverTwoLeafBodiesAtOnce` still pass: the scheduler ignores the setting until Task 7.)

- [ ] **Step 5: Commit.**

```bash
git add gophermind-lib/briefv2/settings/executor.go gophermind-lib/briefv2/settings/executor_test.go gophermind-lib/briefv2/executor/options.go gophermind-lib/briefv2/executor/options_test.go cmd/gophermind/brief_run.go cmd/gophermind/brief_run_test.go
git commit -m "feat(settings,executor,cmd): executor.workers 1 to 8 and --workers overrides it for one run (sub-project F task 2)" -m "Ruling 22: --workers 0 (the default) means the setting; any other value outside 1 to 8 is refused before anything runs." -m "$TRAILERS"
```

## Task 3: `gitland.Files`, `gitland.NewScratch` and `pathsafe.CopyFile`

**Files:**
- Modify: `B2/gitland/gitland.go:11-49` (the `Repo` interface), `B2/gitland/cli.go` (after `Ignored`, line 462; after `NewCLI`, line 102)
- Create: `B2/gitland/scratch_test.go`
- Modify: `B2/pathsafe/pathsafe.go` (new function after `Remove`, line 302; import `io`)
- Create: `B2/pathsafe/copy_test.go`

**Interfaces:**
- Produces: `Repo.Files() ([]string, error)` (sorted, tracked files still on disk plus untracked unignored, never `.gophermind/`); `func gitland.NewScratch(dir, runID string) (*CLI, error)`; `func pathsafe.CopyFile(srcRoot, dstRoot, rel string) error`.
- Every type that implements `gitland.Repo` outside `gitland` embeds it (`B2/executor/resume_test.go:248,678`, `finish_test.go:44`), so the new method needs no other edit.

- [ ] **Step 1: Write the failing gitland tests** in `gophermind-lib/briefv2/gitland/scratch_test.go`:

```go
package gitland

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFilesListsTrackedAndUntrackedButNotIgnored(t *testing.T) {
	c := newRepo(t) // README.md and go.mod committed
	put(t, c, ".gitignore", "*.log\n")
	put(t, c, "pkg/new.go", "package pkg\n")
	put(t, c, "run.log", "ignored\n")
	put(t, c, ".gophermind/r/_state/x.json", "{}\n")
	if err := os.Remove(filepath.Join(c.dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Files()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".gitignore", "go.mod", "pkg/new.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Files = %q, want %q", got, want)
	}
}

func TestNewScratchMakesABaselineWithTheGuard(t *testing.T) {
	useTestGit(t)
	dir := t.TempDir()
	for rel, body := range map[string]string{"go.mod": "module x\n", "a/a.go": "package a\n", ".gitignore": "*.log\n"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewScratch(dir, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	home := s.home
	if d, err := s.Dirty(); err != nil || len(d) != 0 {
		t.Fatalf("Dirty after the baseline = %q, %v", d, err)
	}
	if subj := gx(t, s, "log", "-1", "--format=%s"); subj != "gm(baseline)" {
		t.Fatalf("baseline subject = %q", subj)
	}
	put(t, s, "a/stray.go", "package a\n")
	put(t, s, "x.log", "ignored\n")
	if d, _ := s.Dirty(); !reflect.DeepEqual(d, []string{"a/stray.go"}) {
		t.Fatalf("Dirty = %q", d)
	}
	if ig, _ := s.Ignored(); !reflect.DeepEqual(ig, []string{"x.log"}) {
		t.Fatalf("Ignored = %q", ig)
	}
	if err := s.Restore([]string{"a/stray.go", "x.log"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "stray.go")); !os.IsNotExist(err) {
		t.Fatal("Restore left the untracked file")
	}
	for _, args := range [][]string{{"push"}, {"reset", "--hard"}, {"checkout", "main"}, {"clean", "-f"}, {"gc"}} {
		if _, _, err := s.runCode(args...); !errors.Is(err, ErrForbiddenGitArgs) {
			t.Errorf("git %v in a scratch tree: %v, want the guard", args, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("Close left the private home")
	}
	if _, err := NewScratch(dir, "run-1"); err == nil || !strings.Contains(err.Error(), "already holds a repository") {
		t.Fatalf("a second NewScratch over the same directory: %v", err)
	}
	if _, err := NewScratch(t.TempDir(), "bad id!"); err == nil {
		t.Fatal("an invalid run id was accepted")
	}
}

// A scratch tree under the repository's .gophermind is a repository of its
// own: the outer repository neither sees its files nor gains a commit.
func TestNewScratchInsideARepositoryIsItsOwnRepository(t *testing.T) {
	outer := newRepo(t)
	if err := os.WriteFile(filepath.Join(outer.dir, ".git", "info", "exclude"), []byte(".gophermind/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(outer.dir, ".gophermind", "r-scratch", "trees", "fn-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewScratch(dir, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	put(t, s, "b.go", "package x\n")
	if d, _ := s.Dirty(); !reflect.DeepEqual(d, []string{"b.go"}) {
		t.Fatalf("the tree's Dirty = %q", d)
	}
	if d, _ := outer.Dirty(); len(d) != 0 {
		t.Fatalf("the outer repository sees the tree: %q", d)
	}
	if n := gx(t, outer, "rev-list", "--count", "HEAD"); n != "1" {
		t.Fatalf("the outer repository has %s commits, want 1", n)
	}
}
```

- [ ] **Step 2: Write the failing pathsafe tests** in `gophermind-lib/briefv2/pathsafe/copy_test.go`:

```go
package pathsafe_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/pathsafe"
)

func writeFileMode(t *testing.T, p, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFileKeepsBytesAndTheExecutableBit(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFileMode(t, filepath.Join(src, "a", "x.go"), "package a\n", 0o644)
	writeFileMode(t, filepath.Join(src, "run.sh"), "#!/bin/sh\n", 0o755)
	for _, rel := range []string{"a/x.go", "run.sh"} {
		if err := pathsafe.CopyFile(src, dst, rel); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a", "x.go")); string(b) != "package a\n" {
		t.Fatalf("content = %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(dst, "run.sh")); fi.Mode().Perm()&0o111 == 0 {
		t.Fatal("the executable bit was lost")
	}
	if fi, _ := os.Stat(filepath.Join(dst, "a", "x.go")); fi.Mode().Perm()&0o111 != 0 {
		t.Fatal("a plain file became executable")
	}
	if err := pathsafe.CopyFile(src, dst, "a/x.go"); err == nil {
		t.Fatal("an existing destination was overwritten")
	}
}

func TestCopyFileCopiesALinkInsideTheTreeAndNeverFollowsOne(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFileMode(t, filepath.Join(src, "a", "x.go"), "package a\n", 0o644)
	if err := os.Symlink("x.go", filepath.Join(src, "a", "y.go")); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.CopyFile(src, dst, "a/y.go"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(filepath.Join(dst, "a", "y.go"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was not copied as a link")
	}
	if target, _ := os.Readlink(filepath.Join(dst, "a", "y.go")); target != "x.go" {
		t.Fatalf("target = %q", target)
	}
	outside := t.TempDir()
	writeFileMode(t, filepath.Join(outside, "secret.txt"), "outside\n", 0o644)
	for name, target := range map[string]string{
		"abs.go": filepath.Join(outside, "secret.txt"),
		"up.go":  "../../" + filepath.Base(outside) + "/secret.txt",
		"git.go": "../.git/config",
	} {
		if err := os.Symlink(target, filepath.Join(src, "a", name)); err != nil {
			t.Fatal(err)
		}
		err := pathsafe.CopyFile(src, dst, "a/"+name)
		if err == nil {
			t.Errorf("a link to %s was copied", target)
			continue
		}
		if strings.Contains(err.Error(), outside) || strings.Contains(err.Error(), "secret") {
			t.Errorf("the error names the path: %v", err)
		}
	}
	// A parent directory that is a link is refused in the source and in the destination.
	if err := os.Symlink(outside, filepath.Join(src, "lnk")); err != nil {
		t.Fatal(err)
	}
	if err := pathsafe.CopyFile(src, dst, "lnk/secret.txt"); err == nil {
		t.Error("a file under a linked source directory was copied")
	}
	if err := os.Symlink(outside, filepath.Join(dst, "out")); err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, filepath.Join(src, "out", "f.go"), "package out\n", 0o644)
	if err := pathsafe.CopyFile(src, dst, "out/f.go"); err == nil {
		t.Error("a file was written through a linked destination directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "f.go")); err == nil {
		t.Error("the copy landed outside the tree")
	}
}

func TestCopyFileRefusesUnsafeNamesAndDirectories(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFileMode(t, filepath.Join(src, ".git", "config"), "x", 0o644)
	writeFileMode(t, filepath.Join(src, ".gophermind", "r", "x"), "x", 0o644)
	if err := os.MkdirAll(filepath.Join(src, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".git/config", ".gophermind/r/x", "../x", "/etc/hosts", "a/../b", "", "d"} {
		if err := pathsafe.CopyFile(src, dst, rel); err == nil {
			t.Errorf("CopyFile(%q) was accepted", rel)
		}
	}
}
```

- [ ] **Step 3: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/gitland/ ./gophermind-lib/briefv2/pathsafe/ -count=1
```
Expected: compile failures `c.Files undefined`, `undefined: NewScratch`, `undefined: pathsafe.CopyFile`.

- [ ] **Step 4: Implement gitland.** In `gophermind-lib/briefv2/gitland/gitland.go`, inside `Repo`, after `Ignored()`:

```go
	// Files lists sorted repo-relative paths of the working tree's files that git
	// knows or would add: tracked files still on disk and untracked files that are
	// not ignored. .gophermind/ is never listed. It changes nothing.
	Files() ([]string, error)
```

In `gophermind-lib/briefv2/gitland/cli.go`, after `Ignored` (line 462):

```go
// Files is the file list a private tree copies (sub-project F): ls-files of the
// index and of the untracked files that are not ignored, NUL-separated so odd
// names are exact, minus .gophermind/ and minus tracked files deleted from disk.
func (c *CLI) Files() ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out, err := c.run("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []string
	for _, f := range splitNul(out) {
		if f == ".gophermind" || strings.HasPrefix(f, ".gophermind/") || seen[f] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(c.dir, filepath.FromSlash(f))); err != nil {
			continue // deleted in the working tree: nothing to copy
		}
		seen[f] = true
		res = append(res, f)
	}
	sort.Strings(res)
	return res, nil
}
```

After `NewCLI` (line 102):

```go
// NewScratch makes dir a throwaway repository for one leaf's private tree
// (sub-project F): git init, git add -A, one commit "gm(baseline)". dir must be
// a directory that holds no .git; the files in it are the copy the executor
// made. The CLI has NewCLI's guard, environment, identity and private HOME, so
// Dirty, Ignored and Restore work in the tree as in the repository. Nothing in
// a scratch repository is ever pushed or merged: the executor deletes dir and
// calls Close.
func NewScratch(dir, runID string) (*CLI, error) {
	if !idRe.MatchString(runID) {
		return nil, errors.New("gitland: invalid run id")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.New("gitland: invalid scratch directory")
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if st, err := os.Lstat(abs); err != nil || !st.IsDir() {
		return nil, errors.New("gitland: scratch directory is not a directory")
	}
	if _, err := os.Lstat(filepath.Join(abs, ".git")); err == nil {
		return nil, errors.New("gitland: the scratch directory already holds a repository")
	}
	bin := testGit
	if bin == "" {
		if bin, err = exec.LookPath("git"); err != nil {
			return nil, errors.New("gitland: git binary not found")
		}
	}
	home, err := os.MkdirTemp("", homePrefix)
	if err != nil {
		return nil, errors.New("gitland: cannot make private home")
	}
	c := &CLI{dir: abs, git: bin, runID: runID, home: home}
	fail := func(err error) (*CLI, error) {
		_ = c.Close()
		return nil, err
	}
	if _, err := c.run("init", "-q"); err != nil {
		return fail(err)
	}
	if _, err := c.run("add", "-A"); err != nil {
		return fail(err)
	}
	if _, err := c.run("commit", "--no-verify", "--allow-empty", "-q", "-m", "gm(baseline)", "-m", c.runTrailer()); err != nil {
		return fail(err)
	}
	out, err := c.run("rev-parse", "--show-toplevel")
	if err != nil {
		return fail(ErrNotRepoRoot)
	}
	if top, err := filepath.EvalSymlinks(strings.TrimRight(string(out), "\n")); err != nil || top != abs {
		return fail(ErrNotRepoRoot)
	}
	return c, nil
}
```

(`init`, `add` and `commit` pass `guard`; `GIT_CEILING_DIRECTORIES` in `env()` stops discovery at the parent, so the outer repository is never found.)

- [ ] **Step 5: Implement pathsafe.** In `gophermind-lib/briefv2/pathsafe/pathsafe.go` add `"io"` to the imports and, after `Remove`:

```go
// CopyFile copies rel from the directory srcRoot to the same place under
// dstRoot, for a private tree (sub-project F). rel is a clean relative slash
// path with no .git or .gophermind component. A regular file is copied with
// its executable bits (mode 0644 or 0755), opened without following a link. A
// symbolic link is copied as a link with the same target text, only when the
// target is relative and stays inside the tree; it is never followed. Anything
// else, a parent directory that is a link in either tree, or an existing
// destination is refused. Errors name the path by its length only.
func CopyFile(srcRoot, dstRoot, rel string) error {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, `\`) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("copied path (%d bytes) is not a clean path inside the tree", len(rel))
	}
	if hasGitName(strings.Split(rel, "/")) {
		return fmt.Errorf("copied path (%d bytes) is under .git or .gophermind", len(rel))
	}
	if err := noLinkedParent(srcRoot, rel); err != nil {
		return err
	}
	src := filepath.Join(srcRoot, filepath.FromSlash(rel))
	dst := filepath.Join(dstRoot, filepath.FromSlash(rel))
	fi, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
	}
	if err := noLinkedParent(dstRoot, rel); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
	}
	if err := noLinkedParent(dstRoot, rel); err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		joined := path.Clean(path.Join(path.Dir(rel), filepath.ToSlash(target)))
		if filepath.IsAbs(target) || joined == ".." || strings.HasPrefix(joined, "../") || hasGitName(strings.Split(joined, "/")) {
			return fmt.Errorf("a symbolic link (%d bytes of path) points outside the tree", len(rel))
		}
		if err := os.Symlink(target, dst); err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		return nil
	case fi.Mode().IsRegular():
		perm := os.FileMode(0o644)
		if fi.Mode().Perm()&0o111 != 0 {
			perm = 0o755
		}
		in, err := os.OpenFile(src, os.O_RDONLY|NoFollow, 0)
		if err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|NoFollow, perm)
		if err != nil {
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		if err := out.Chmod(perm); err != nil {
			out.Close()
			return fmt.Errorf("copying %d bytes of path: %s", len(rel), osReason(err))
		}
		return out.Close()
	}
	return fmt.Errorf("copied path (%d bytes) is not a regular file or a link", len(rel))
}

// noLinkedParent refuses rel when an existing directory on its way under root
// is a symbolic link.
func noLinkedParent(root, rel string) error {
	cur := root
	for _, part := range strings.Split(path.Dir(rel), "/") {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil // not there yet
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("copied path (%d bytes) passes through a symbolic link", len(rel))
		}
	}
	return nil
}
```

- [ ] **Step 6: Run, with the race detector too.**

```bash
gofmt -w gophermind-lib/briefv2/gitland gophermind-lib/briefv2/pathsafe && go vet ./gophermind-lib/briefv2/gitland/ ./gophermind-lib/briefv2/pathsafe/ ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/gitland/ ./gophermind-lib/briefv2/pathsafe/ -count=1
gt ./gophermind-lib/briefv2/gitland/ ./gophermind-lib/briefv2/pathsafe/ -race -count=1
```
Expected: `ok` for both packages each time; `go vet` of the executor proves every `gitland.Repo` value still compiles.

- [ ] **Step 7: Commit.**

```bash
git add gophermind-lib/briefv2/gitland/gitland.go gophermind-lib/briefv2/gitland/cli.go gophermind-lib/briefv2/gitland/scratch_test.go gophermind-lib/briefv2/pathsafe/pathsafe.go gophermind-lib/briefv2/pathsafe/copy_test.go
git commit -m "feat(gitland,pathsafe): Files, NewScratch and CopyFile for private trees (sub-project F task 3)" -m "Ruling 3: NewScratch stages with add -A and takes no file list. Ruling 4: a link is copied only when its target is relative and stays inside the tree." -m "$TRAILERS"
```

## Task 4: Private work trees (base copy, tree, environment, profile, removal)

**Files:**
- Create: `B2/executor/worktree.go`, `B2/executor/worktree_test.go`
- Modify: `B2/executor/start.go:48-80` (runCtx fields), `:357-361` (cleanTrees after the directories), `:380` (keep the profile)
- Modify: `B2/executor/env.go:34-47,111-119` (envAt, leafCheckIn)

**Interfaces:**
- Consumes: `gitland.Repo.Files`, `gitland.NewScratch`, `pathsafe.CopyFile` (Task 3); `ensureDir` (`start.go`), `runner.New`, `sandbox.Profile`.
- Produces:

```go
type workTree struct {
	ID      string       // the leaf of a private tree; "" for the main tree
	Repo    string       // absolute root: rc.o.Repo or <scratch>/trees/<id>
	Git     gitland.Repo // rc.git or the tree's own (gitland.NewScratch)
	Chk     Checker      // rc.chk or a runner with the tree's profile
	Scratch string       // HOME and TMPDIR: rc.scratch or <scratch>/w/<id>
	Private bool
	cli     *gitland.CLI // closed by dropTree
}
const maxTreeBytes = 256 << 20
var treeIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
func (rc *runCtx) mainTree() *workTree
func (rc *runCtx) snapshotLevel() error
func (rc *runCtx) endLevel()
func (rc *runCtx) makeTree(id string) (*workTree, error)
func (rc *runCtx) dropTree(wt *workTree)
func (rc *runCtx) removeScratch(kind, id string) error // kind trees|w|base
func (rc *runCtx) cleanTrees() error
func (rc *runCtx) treeProfile(repo, scratch string) *sandbox.Profile
func (rc *runCtx) checkerFor(repo, scratch string) Checker
func (rc *runCtx) envAt(scratch, nodeID string, mode envMode) ([]string, error)
func (rc *runCtx) leafCheckIn(ctx context.Context, wt *workTree, l *Leaf, files []string) runner.Verdict
// runCtx gains: profile *sandbox.Profile; baseFiles []string
```

- [ ] **Step 1: Write the failing tests** in `gophermind-lib/briefv2/executor/worktree_test.go`:

```go
package executor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/sandbox"
)

func TestSnapshotAndMakeTreeCopyTheLevel(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	write(t, filepath.Join(g.repo, "notes.txt"), "untracked, not ignored\n")
	write(t, filepath.Join(g.repo, ".git", "info", "exclude"), "*.log\n")
	write(t, filepath.Join(g.repo, "build.log"), "ignored\n")
	before, err := g.git.Dirty()
	if err != nil {
		t.Fatal(err)
	}
	commits := g.gitCmd("rev-list", "--count", "HEAD")
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	wt, err := rc.makeTree(leafID)
	if err != nil {
		t.Fatal(err)
	}
	l := rc.plan.Leaf(leafID)
	if wt.Repo != filepath.Join(rc.scratch, "trees", leafID) || wt.Scratch != filepath.Join(rc.scratch, "w", leafID) || !wt.Private || wt.ID != leafID {
		t.Fatalf("tree = %+v", wt)
	}
	for _, rel := range []string{"go.mod", "notes.txt", l.StubFile, l.TestFile, "cmd/greeter/main.go", ".git"} {
		if !fileExists(filepath.Join(wt.Repo, filepath.FromSlash(rel))) {
			t.Errorf("%s is missing from the tree", rel)
		}
	}
	for _, rel := range []string{"build.log", ".gophermind"} {
		if fileExists(filepath.Join(wt.Repo, rel)) {
			t.Errorf("%s was copied", rel)
		}
	}
	if d, err := wt.Git.Dirty(); err != nil || len(d) != 0 {
		t.Fatalf("the tree is dirty after its baseline: %q %v", d, err)
	}
	after, _ := g.git.Dirty()
	if !reflect.DeepEqual(before, after) || g.gitCmd("rev-list", "--count", "HEAD") != commits {
		t.Fatalf("the main tree changed: %q -> %q", before, after)
	}
	rc.dropTree(wt)
	rc.endLevel()
	for _, d := range []string{wt.Repo, wt.Scratch, filepath.Join(rc.scratch, "base")} {
		if fileExists(d) {
			t.Errorf("%s was left behind", d)
		}
	}
}

func TestAPrivateTreeHasItsOwnHomeAndProfile(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	wt, err := rc.makeTree(leafID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.dropTree(wt)
	env, err := rc.envAt(wt.Scratch, leafID, envLeaf)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for k, want := range map[string]string{"HOME": wt.Scratch, "TMPDIR": wt.Scratch, "GOPATH": filepath.Join(wt.Scratch, "gopath"), "GOCACHE": rc.goCache, "GOMODCACHE": rc.modCache} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if wt.Chk != Checker(fc) {
		t.Fatal("with the sandbox off a tree must use the run's checker")
	}
	rc.profile = &sandbox.Profile{Repo: g.repo, RunDir: g.runDir, Scratch: rc.scratch, GoCache: rc.goCache, ReadOnly: []string{"/usr/bin"}}
	defer func() { rc.profile = nil }()
	p := rc.treeProfile(wt.Repo, wt.Scratch)
	if p.Repo != wt.Repo || p.Scratch != wt.Scratch || p.RunDir != g.runDir || p.GoCache != rc.goCache || !reflect.DeepEqual(p.ReadOnly, []string{"/usr/bin"}) {
		t.Fatalf("profile = %+v", p)
	}
	p.ReadOnly[0] = "/changed"
	if rc.profile.ReadOnly[0] != "/usr/bin" {
		t.Fatal("the tree profile shares the run's ReadOnly list")
	}
	if _, ok := rc.checkerFor(wt.Repo, wt.Scratch).(*runner.Runner); !ok {
		t.Fatal("with a profile a tree gets its own runner")
	}
}

// Review Focus 1: a link out of the tree, and a tree over the limit, are
// refused before a private tree exists; the errors name no path.
func TestSnapshotLevelRefusesALinkOutOfTheTreeAndAnOversizedTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	link := filepath.Join(g.repo, "hosts-link")
	if err := os.Symlink("/etc/hosts", link); err != nil {
		t.Fatal(err)
	}
	err := rc.snapshotLevel()
	if err == nil || err.Error() != "executor: a file of the repository cannot be copied into a private tree" {
		t.Fatalf("a link out of the tree: %v", err)
	}
	if fileExists(filepath.Join(rc.scratch, "base")) {
		t.Fatal("a refused base was left behind")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(g.repo, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTreeBytes + 1); err != nil { // sparse: no disk is used
		t.Fatal(err)
	}
	f.Close()
	if err := rc.snapshotLevel(); err == nil || err.Error() != "executor: the tree copy is over 256 MiB" {
		t.Fatalf("an oversized tree: %v", err)
	}
	if err := os.Remove(big); err != nil {
		t.Fatal(err)
	}
	if err := rc.snapshotLevel(); err != nil {
		t.Fatalf("a clean tree: %v", err)
	}
	rc.endLevel()
}

// Review Focus 3: what a killed run leaves (a read-only directory in an old
// tree, a link planted in place of w/) is removed at the next start without
// following the link; the guard refuses anything else.
func TestLeftoverTreesOfAKilledRunAreRemovedWithoutFollowingALink(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	scratch := filepath.Join(g.repo, ".gophermind", g.id+"-scratch")
	ro := filepath.Join(scratch, "trees", "fn-old", "pkg")
	write(t, filepath.Join(ro, "a.go"), "package pkg\n")
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "keep.txt"), "keep\n")
	if err := os.Symlink(outside, filepath.Join(scratch, "w")); err != nil {
		t.Fatal(err)
	}
	rc, _ := g.leafRC(t, Script{}, true) // startRun's preflight cleans the scratch directory
	for _, d := range []string{"trees", "w", "base"} {
		if _, err := os.Lstat(filepath.Join(rc.scratch, d)); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", d, err)
		}
	}
	if !fileExists(filepath.Join(outside, "keep.txt")) {
		t.Fatal("the removal followed the link")
	}
	for _, c := range [][2]string{{"trees", "../x"}, {"bin", ""}, {"trees", "Fn"}, {"w", "a/b"}, {"", ""}} {
		if err := rc.removeScratch(c[0], c[1]); err == nil {
			t.Errorf("removeScratch(%q, %q) was allowed", c[0], c[1])
		}
	}
	saved := rc.scratch
	rc.scratch = t.TempDir()
	if err := rc.removeScratch("trees", ""); err == nil {
		t.Error("a scratch directory that is not the run's was accepted")
	}
	rc.scratch = saved
}

func TestTwoTreesAreMadeAtOnce(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.leafRC(t, Script{}, true)
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	ids := []string{"fn-farewell", "fn-greet"}
	trees := make([]*workTree, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trees[i], errs[i] = rc.makeTree(id)
		}()
	}
	wg.Wait()
	for i := range trees {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		defer rc.dropTree(trees[i])
	}
	if trees[0].Repo == trees[1].Repo || trees[0].Scratch == trees[1].Scratch {
		t.Fatal("two leaves share a tree")
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/executor/ -run 'TestSnapshotAndMakeTree|TestAPrivateTreeHasItsOwn|TestSnapshotLevelRefuses|TestLeftoverTrees|TestTwoTreesAreMade' -count=1 -timeout 45m
```
Expected: compile failure (`rc.snapshotLevel undefined`).

- [ ] **Step 3: Implement** `gophermind-lib/briefv2/executor/worktree.go`:

```go
package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/gitland"
	"gophermind/gophermind-lib/briefv2/pathsafe"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/sandbox"
)

// workTree is where one leaf reads and writes the repository: the main tree,
// or a private copy of it (sub-project F, spec section 5). The leaf loop reaches
// the repository only through it.
type workTree struct {
	ID      string       // the leaf of a private tree; "" for the main tree
	Repo    string       // absolute root: rc.o.Repo or <scratch>/trees/<id>
	Git     gitland.Repo // rc.git or the tree's own (gitland.NewScratch)
	Chk     Checker      // rc.chk or a runner with the tree's sandbox profile
	Scratch string       // HOME and TMPDIR of the tree's commands: rc.scratch or <scratch>/w/<id>
	Private bool
	cli     *gitland.CLI // the private tree's CLI, closed by dropTree
}

// maxTreeBytes is the copy limit of decision F12: a level whose files are
// larger runs one leaf at a time.
const maxTreeBytes = 256 << 20

// treeIDRe is the shape of a leaf id that may name a scratch directory.
var treeIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// mainTree is the repository itself. It reads rc.chk when called, so a test
// that replaced the checker after startRun is honoured.
func (rc *runCtx) mainTree() *workTree {
	return &workTree{Repo: rc.o.Repo, Git: rc.git, Chk: rc.chk, Scratch: rc.scratch}
}

// snapshotLevel copies the main tree once, before a parallel level starts, into
// <scratch>/base, and keeps the file list for makeTree (ruling 2). It runs in a
// serial phase, so no leaf is landing while it reads. Every error is fixed text
// with no path; the caller then runs the level one leaf at a time.
func (rc *runCtx) snapshotLevel() error {
	rc.endLevel()
	files, err := rc.git.Files()
	if err != nil {
		return errors.New("executor: the repository's file list could not be read")
	}
	var total int64
	for _, f := range files {
		fi, err := os.Lstat(filepath.Join(rc.o.Repo, filepath.FromSlash(f)))
		if err != nil {
			return errors.New("executor: a file of the repository could not be read")
		}
		if total += fi.Size(); total > maxTreeBytes {
			return errors.New("executor: the tree copy is over 256 MiB")
		}
	}
	base := filepath.Join(rc.scratch, "base")
	if err := ensureDir(base); err != nil {
		return err
	}
	for _, f := range files {
		if err := pathsafe.CopyFile(rc.o.Repo, base, f); err != nil {
			rc.endLevel()
			return errors.New("executor: a file of the repository cannot be copied into a private tree")
		}
	}
	rc.baseFiles = files
	return nil
}

// endLevel removes the level's base copy. Safe to call when there is none.
func (rc *runCtx) endLevel() {
	rc.baseFiles = nil
	if err := rc.removeScratch("base", ""); err != nil {
		rc.emit("warning", "", "the base copy of a level could not be removed")
	}
}

// makeTree makes the private tree of one leaf from the level's base copy: the
// files, a throwaway repository with one baseline commit, the leaf's HOME and
// TMPDIR with a GOPATH, and the checker of the tree. A failure leaves nothing.
func (rc *runCtx) makeTree(id string) (*workTree, error) {
	if !treeIDRe.MatchString(id) {
		return nil, errors.New("executor: a leaf id is not a safe directory name")
	}
	fail := func() (*workTree, error) {
		_ = rc.removeScratch("trees", id)
		_ = rc.removeScratch("w", id)
		return nil, fmt.Errorf("executor: the private tree of %s could not be made", id)
	}
	if rc.removeScratch("trees", id) != nil || rc.removeScratch("w", id) != nil {
		return fail()
	}
	repo := filepath.Join(rc.scratch, "trees", id)
	home := filepath.Join(rc.scratch, "w", id)
	for _, d := range []string{filepath.Join(rc.scratch, "trees"), repo, filepath.Join(rc.scratch, "w"), home, filepath.Join(home, "gopath")} {
		if err := ensureDir(d); err != nil {
			return fail()
		}
	}
	base := filepath.Join(rc.scratch, "base")
	for _, f := range rc.baseFiles {
		if err := pathsafe.CopyFile(base, repo, f); err != nil {
			return fail()
		}
	}
	cli, err := gitland.NewScratch(repo, rc.plan.RunID)
	if err != nil {
		return fail()
	}
	return &workTree{ID: id, Repo: repo, Git: cli, cli: cli, Chk: rc.checkerFor(repo, home), Scratch: home, Private: true}, nil
}

// dropTree closes a private tree's git layer and removes the tree and its HOME.
func (rc *runCtx) dropTree(wt *workTree) {
	if wt == nil || !wt.Private {
		return
	}
	if wt.cli != nil {
		_ = wt.cli.Close()
	}
	for _, kind := range []string{"trees", "w"} {
		if err := rc.removeScratch(kind, wt.ID); err != nil {
			rc.emit("warning", wt.ID, "a private tree could not be removed")
		}
	}
}

// cleanTrees removes what a killed run left of its private trees and base copy:
// derived data only (a candidate lives in _state, decision F10).
func (rc *runCtx) cleanTrees() error {
	for _, kind := range []string{"trees", "w", "base"} {
		if err := rc.removeScratch(kind, ""); err != nil {
			return err
		}
	}
	return nil
}

// removeScratch deletes <scratch>/<kind> or <scratch>/<kind>/<id>, the only
// directories parallel levels make. The target is built and checked before
// anything is deleted: rc.scratch must be absolute, end in -scratch and sit in
// a .gophermind directory; kind must be trees, w or base; id empty or a leaf
// id; the target strictly inside rc.scratch. It is emitted (relative path) and
// only then removed. A link in its place is removed itself, never followed.
func (rc *runCtx) removeScratch(kind, id string) error {
	if !filepath.IsAbs(rc.scratch) || !strings.HasSuffix(rc.scratch, "-scratch") || filepath.Base(filepath.Dir(rc.scratch)) != ".gophermind" {
		return errors.New("executor: refusing to delete under a directory that is not the run's scratch directory")
	}
	if kind != "trees" && kind != "w" && kind != "base" {
		return errors.New("executor: refusing to delete an unknown scratch directory")
	}
	if id != "" && !treeIDRe.MatchString(id) {
		return errors.New("executor: refusing to delete a scratch directory with an unsafe name")
	}
	rel := kind
	if id != "" {
		rel = kind + "/" + id
	}
	target := filepath.Join(rc.scratch, filepath.FromSlash(rel))
	if target == "/" || target == rc.home || !strings.HasPrefix(target, rc.scratch+string(filepath.Separator)) {
		return errors.New("executor: refusing to delete a path outside the scratch directory")
	}
	fi, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("executor: a scratch directory could not be inspected")
	}
	rc.emit("debug", id, "removing scratch "+rel)
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		if err := os.Remove(target); err != nil {
			return errors.New("executor: a scratch entry could not be removed")
		}
		return nil
	}
	makeWritable(target)
	if err := os.RemoveAll(target); err != nil {
		return errors.New("executor: a scratch directory could not be removed")
	}
	return nil
}

// makeWritable gives the owner write access to every directory under root (a
// tool may leave read-only ones), never through a link.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if d != nil && d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
}

// treeProfile is the run's sandbox profile with the tree as the repository and
// the leaf's directory as HOME and TMPDIR; nil when the sandbox is off. RunDir,
// GoCache, GoModCache, ReadOnly and Home are the run's, so a leaf can write
// neither a sibling's tree nor the main repository.
func (rc *runCtx) treeProfile(repo, scratch string) *sandbox.Profile {
	if rc.profile == nil {
		return nil
	}
	p := *rc.profile
	p.Repo, p.Scratch = repo, scratch
	p.ReadOnly = append([]string(nil), rc.profile.ReadOnly...)
	p.LoopbackPorts = append([]int(nil), rc.profile.LoopbackPorts...)
	return &p
}

// checkerFor is the Checker of a private tree: a runner with the tree's profile
// when the sandbox is on (cheap: no process), else rc.chk, which takes the
// directory from each call; a test's fake Checker therefore stays in place.
func (rc *runCtx) checkerFor(repo, scratch string) Checker {
	p := rc.treeProfile(repo, scratch)
	if p == nil {
		return rc.chk
	}
	return runner.New(runner.Config{Sandbox: p, OutputCap: rc.cfg.Executor.OutputCapBytes, Grace: 2 * time.Second})
}

// leafCheckIn is runner.LeafCheck of the named files of the leaf in a work tree.
func (rc *runCtx) leafCheckIn(ctx context.Context, wt *workTree, l *Leaf, files []string) runner.Verdict {
	env, err := rc.envAt(wt.Scratch, l.ID, envLeaf)
	if err != nil {
		return runner.Verdict{Class: runner.ClassHarness, Err: err}
	}
	return wt.Chk.CheckLeaf(ctx, runner.LeafCheck{
		Repo: wt.Repo, Dir: l.Dir, TestFunc: l.TestFunc, Files: files, Env: env, TestTimeout: rc.testTimeout(),
	})
}
```

In `gophermind-lib/briefv2/executor/env.go` turn `env` into a wrapper (lines 34-47):

```go
func (rc *runCtx) env(nodeID string, mode envMode) ([]string, error) {
	return rc.envAt(rc.scratch, nodeID, mode)
}

// envAt is env with HOME, TMPDIR and GOPATH under scratch: rc.scratch for the
// main tree, <scratch>/w/<id> for a private tree. GOCACHE and GOMODCACHE stay
// the run's (the build cache is safe for concurrent builds; the module cache is
// read-only outside the deps step).
func (rc *runCtx) envAt(scratch, nodeID string, mode envMode) ([]string, error) {
	tc := map[string]string{}
	for k, v := range rc.cfg.Toolchain {
		tc[k] = v
	}
	if mode == envAcceptance {
		tc["PATH"] = rc.binDir + string(filepath.ListSeparator) + tc["PATH"]
	}
	tc["HOME"] = scratch
	tc["TMPDIR"] = scratch
	tc["GOCACHE"] = rc.goCache
	tc["GOMODCACHE"] = rc.modCache
	tc["GOPATH"] = filepath.Join(scratch, "gopath")
```

(the rest of the old `env` body, from `tc["CGO_ENABLED"] = "0"` on, is unchanged and now ends `envAt`), and replace `leafCheck` (lines 111-119):

```go
// leafCheck is runner.LeafCheck over the named files of the leaf in the main tree.
func (rc *runCtx) leafCheck(ctx context.Context, l *Leaf, files []string) runner.Verdict {
	return rc.leafCheckIn(ctx, rc.mainTree(), l, files)
}
```

In `gophermind-lib/briefv2/executor/start.go`: add to `runCtx` after `afterRevStep`:

```go
	profile   *sandbox.Profile // the run's sandbox profile, nil when the sandbox is off; private trees copy it
	baseFiles []string         // the file list of the current parallel level's base copy (written in a serial phase)
```

after the `ensureDir` loop (line 361) in `preflight`:

```go
	if err := rc.cleanTrees(); err != nil {
		return err
	}
```

and after `rc.chk = runner.New(...)` (line 380): `rc.profile = profile`.

- [ ] **Step 4: Run, with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/executor/ -run 'TestSnapshotAndMakeTree|TestAPrivateTreeHasItsOwn|TestSnapshotLevelRefuses|TestLeftoverTrees|TestTwoTreesAreMade|TestPreflight|TestEnv|TestAcceptanceInterruptedIsNotAPass' -race -count=3 -timeout 45m -parallel 2
```
Expected: `ok` (see the flake rule if only `TestAcceptanceInterruptedIsNotAPass` fails).

- [ ] **Step 5: Commit.**

```bash
git add gophermind-lib/briefv2/executor/worktree.go gophermind-lib/briefv2/executor/worktree_test.go gophermind-lib/briefv2/executor/env.go gophermind-lib/briefv2/executor/start.go
git commit -m "feat(executor): private work trees: base copy, tree, HOME, profile and guarded removal (sub-project F task 4)" -m "Ruling 2: trees are copied from a per-level base, never from the live main tree. Ruling 4: a copy failure or 256 MiB makes the level serial. Ruling 14: removal emits a debug event with the relative path, only when something exists." -m "$TRAILERS"
```

## Task 5: The leaf loop works through a work tree; the turn; candidate files

**Files:**
- Create: `B2/executor/turn.go`, `B2/executor/turn_test.go`, `B2/executor/candidate.go`, `B2/executor/candidate_test.go`, `B2/executor/leaftree_test.go`
- Modify: `B2/executor/leaf.go:54-58` (leafIn), `:92-121` (leafRun), `:228-264` (newLeafRun), `:335` (adoption guard), `:587-680` (adoptOnDisk), `:719-731` (cleanStray), `:1143-1154` (testSource), `:1218-1228` (check)
- Modify: `B2/executor/env.go:121-124` (remove `rc.checkLeaf`, made unused here)

**Interfaces:**
- Consumes: `workTree`, `rc.mainTree`, `rc.leafCheckIn` (Task 4); `writeStateBytes`, `readStateBytes`, `stateDir` (`B2/executor/state.go:244,305,167`).
- Produces:

```go
// leafIn gains: Tree *workTree (nil: the main tree); Turn *turn (nil: serial)
// leafRun gains: wt *workTree; stub []byte; turn *turn; cand *candidate; commit string
func (lr *leafRun) checkLeaf(ctx context.Context) runner.Verdict
type turn struct { prev <-chan struct{}; done chan struct{}; body *sync.Mutex; held bool; once sync.Once }
func newTurn(prev <-chan struct{}, body *sync.Mutex) *turn
func (t *turn) wait(ctx context.Context) error
func (t *turn) release()
const candidatePrefix = "candidate-"
type candidate struct { Revision int; Entry, Tier string; Pos int; SHA256, Source string }
func (c candidate) entry() ladderEntry
func saveCandidate(runDir, id string, c candidate) error
func readCandidate(runDir, id string) (candidate, bool, error)
func removeCandidate(runDir, id string) error
```

- [ ] **Step 1: Write the failing tests.** `gophermind-lib/briefv2/executor/turn_test.go`:

```go
package executor

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTurnsTakeTheMainTreeOneAtATimeInOrder(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	const n = 6
	turns := make([]*turn, n)
	var prev <-chan struct{}
	for i := range turns {
		turns[i] = newTurn(prev, &body)
		prev = turns[i].done
	}
	var mu sync.Mutex
	var order []int
	var cur, max int32
	var wg sync.WaitGroup
	for i := n - 1; i >= 0; i-- { // started in reverse: the order must not depend on it
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer turns[i].release()
			time.Sleep(time.Duration((n-i)*3) * time.Millisecond)
			if err := turns[i].wait(context.Background()); err != nil {
				t.Error(err)
				return
			}
			c := atomic.AddInt32(&cur, 1)
			for {
				m := atomic.LoadInt32(&max)
				if c <= m || atomic.CompareAndSwapInt32(&max, m, c) {
					break
				}
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&cur, -1)
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(order, []int{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("order = %v", order)
	}
	if max != 1 {
		t.Fatalf("%d jobs held the main tree at once", max)
	}
	if !body.TryLock() {
		t.Fatal("the body mutex is still held after every release")
	}
	body.Unlock()
}

func TestTurnWaitEndsWithTheContextAndTakesNothing(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	tn := newTurn(make(chan struct{}), &body)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if err := tn.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v", err)
	}
	if !body.TryLock() {
		t.Fatal("an interrupted wait took the main tree")
	}
	body.Unlock()
	tn.release()
	tn.release() // twice is safe
	select {
	case <-tn.done:
	default:
		t.Fatal("release did not let the next job go")
	}
	free := newTurn(nil, &body)
	if err := free.wait(ctx); err == nil {
		t.Fatal("an ended context took a free turn")
	}
	free.release()
}

func TestAJobThatEndsWithoutItsTurnDoesNotBlockTheNext(t *testing.T) {
	t.Parallel()
	var body sync.Mutex
	first := newTurn(nil, &body)
	second := newTurn(first.done, &body)
	first.release() // it failed before it needed the main tree
	done := make(chan error, 1)
	go func() { done <- second.wait(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the next job never got its turn")
	}
	second.release()
}
```

`gophermind-lib/briefv2/executor/candidate_test.go`:

```go
package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateRoundTripAndGuards(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := candidate{Revision: 1, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "abc", Source: "package greet\n"}
	if err := saveCandidate(dir, "fn-greet", c); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "_state", candidatePrefix+"fn-greet"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("candidate file %v %v", fi, err)
	}
	got, found, err := readCandidate(dir, "fn-greet")
	if err != nil || !found || got != c {
		t.Fatalf("read = %+v %v %v", got, found, err)
	}
	if got.entry() != (ladderEntry{Name: "a/m1", Tier: "standard", Pos: 1}) {
		t.Fatalf("entry = %+v", got.entry())
	}
	if err := removeCandidate(dir, "fn-greet"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := readCandidate(dir, "fn-greet"); found {
		t.Fatal("a removed candidate is still read")
	}
	if err := removeCandidate(dir, "fn-greet"); err != nil {
		t.Fatalf("removing a missing candidate: %v", err)
	}
	// A file that does not parse is derived data: removed and read as absent.
	if err := writeStateBytes(dir, candidatePrefix+"fn-greet", []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, found, err := readCandidate(dir, "fn-greet"); found || err != nil {
		t.Fatalf("garbage read as %v %v", found, err)
	}
	if fileExists(filepath.Join(dir, "_state", candidatePrefix+"fn-greet")) {
		t.Fatal("the garbage file was kept")
	}
	for _, id := range []string{"../x", "Fn", ""} {
		if err := saveCandidate(dir, id, c); err == nil {
			t.Errorf("saveCandidate(%q) accepted", id)
		}
		if err := removeCandidate(dir, id); err == nil {
			t.Errorf("removeCandidate(%q) accepted", id)
		}
	}
}
```

`gophermind-lib/briefv2/executor/leaftree_test.go`:

```go
package executor

import (
	"context"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/runner"
)

// A serial leaf is checked in the main tree as before; a check in a private
// tree reaches the checker with the tree as its repository.
func TestLeafChecksGoToTheLeafsWorkTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	var mu sync.Mutex
	var repos []string
	fc.Hook = func(repo string) { mu.Lock(); repos = append(repos, repo); mu.Unlock() }
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	wt, err := rc.makeTree("fn-farewell")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.dropTree(wt)
	l := rc.plan.Leaf("fn-farewell")
	fc.LeafScript[l.ID] = []runner.Verdict{passVerdict()}
	rc.leafCheckIn(context.Background(), wt, l, []string{l.File})
	mu.Lock()
	defer mu.Unlock()
	if len(repos) != 2 || repos[0] != g.repo || repos[1] != wt.Repo {
		t.Fatalf("checks ran in %q", repos)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/executor/ -run 'TestTurn|TestAJobThatEnds|TestCandidateRoundTrip|TestLeafChecksGoTo' -count=1 -timeout 45m
```
Expected: compile failure (`undefined: newTurn`).

- [ ] **Step 3: Implement** `gophermind-lib/briefv2/executor/turn.go`:

```go
package executor

import (
	"context"
	"sync"
)

// turn is one job's place in the landing order of a parallel level (spec
// section 7.1). Jobs start in id order; each waits for the job before it to
// end before it touches the main tree, so commits are made one at a time and
// in id order. A job releases its turn when it ends, however it ends, so a
// failing leaf never blocks the ones after it. The turn holder also holds the
// scheduler's body mutex (ruling 15). Only the job's own goroutine calls wait
// and release; ids only wait on lower ids, which started earlier and hold their
// own slot, so the chain cannot deadlock.
type turn struct {
	prev <-chan struct{} // done of the previous job that ran in the level; nil for the first
	done chan struct{}   // closed once, by release
	body *sync.Mutex     // rc.sched.body
	held bool
	once sync.Once
}

func newTurn(prev <-chan struct{}, body *sync.Mutex) *turn {
	return &turn{prev: prev, done: make(chan struct{}), body: body}
}

// wait blocks until the previous job ended, then takes the main tree. When the
// context has ended, or ends first, it returns the context's error and takes
// nothing. A turn already held is not taken again.
func (t *turn) wait(ctx context.Context) error {
	if t.held {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.prev != nil {
		select {
		case <-t.prev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	t.body.Lock()
	t.held = true
	return nil
}

// release gives the main tree back and lets the next job go. Safe to call more
// than once.
func (t *turn) release() {
	t.once.Do(func() {
		if t.held {
			t.held = false
			t.body.Unlock()
		}
		close(t.done)
	})
}
```

`gophermind-lib/briefv2/executor/candidate.go`:

```go
package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/briefv2/router"
)

// candidatePrefix names the saved private pass of a leaf: _state/candidate-<id>
// (decision F10).
const candidatePrefix = "candidate-"

// candidate is a reply that passed its leaf check in a private tree, saved
// before the leaf waits for its turn, so a kill between the pass and the
// landing costs no model call. It holds gate-passed source and the ladder entry
// that wrote it: no prompt, no check output, no secret (the reply gate refuses
// a reply that holds one, and landing runs the gate again).
type candidate struct {
	Revision int    `json:"revision"` // the row revision the reply was written for
	Entry    string `json:"entry"`    // "provider/model"
	Tier     string `json:"tier"`
	Pos      int    `json:"pos"`
	SHA256   string `json:"reply_sha256"`
	Source   string `json:"source"`
}

// entry is the ladder entry the pass attempt is recorded against at landing.
func (c candidate) entry() ladderEntry {
	return ladderEntry{Name: c.Entry, Tier: router.Tier(c.Tier), Pos: c.Pos}
}

func saveCandidate(runDir, id string, c candidate) error {
	if !treeIDRe.MatchString(id) {
		return errors.New("executor: refusing a candidate for an unsafe leaf id")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("executor: the candidate of %s could not be encoded", id)
	}
	return writeStateBytes(runDir, candidatePrefix+id, raw)
}

// readCandidate returns the saved candidate of the leaf. A file that does not
// parse is removed and read as absent: it is derived data (ruling 10).
func readCandidate(runDir, id string) (candidate, bool, error) {
	if !treeIDRe.MatchString(id) {
		return candidate{}, false, errors.New("executor: refusing a candidate for an unsafe leaf id")
	}
	raw, found, err := readStateBytes(runDir, candidatePrefix+id)
	if err != nil || !found {
		return candidate{}, false, err
	}
	var c candidate
	if json.Unmarshal(raw, &c) != nil || c.Source == "" || c.Entry == "" {
		return candidate{}, false, removeCandidate(runDir, id)
	}
	return c, true, nil
}

// removeCandidate deletes _state/candidate-<id>. The guard: a leaf id, and a
// regular file directly in _state. A missing file is not an error.
func removeCandidate(runDir, id string) error {
	if !treeIDRe.MatchString(id) {
		return errors.New("executor: refusing to remove a file that is not a saved candidate")
	}
	dir, ok, err := stateDir(runDir, false)
	if err != nil || !ok {
		return err
	}
	path := filepath.Join(dir, candidatePrefix+id)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("executor: the candidate of %s is not a regular file", id)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("executor: the candidate of %s could not be removed", id)
	}
	return nil
}
```

Edit `gophermind-lib/briefv2/executor/leaf.go`:

(a) `leafIn` (lines 54-58) gains two fields:

```go
	Tree     *workTree      // where the leaf works; nil is the main tree (sub-project F)
	Turn     *turn          // the leaf's place in the landing order of a parallel level; nil when serial
```

(b) `leafRun` (lines 92-121) gains, after `worker string`:

```go
	wt     *workTree // the tree the leaf reads and writes: the main tree, or its private copy
	stub   []byte    // the stub source, for a new swap when the leaf moves to the main tree
	turn   *turn     // nil when the level is serial
	cand   *candidate // a private pass waiting for its turn
	commit string     // the short hash finishPass committed, "" in diff_only
```

(c) In `newLeafRun` (lines 228-264) replace `declaredNames(rc.o.Repo, l)` with `declaredNames(wt.Repo, l)` after choosing the tree, and the swap lines:

```go
	wt := in.Tree
	if wt == nil {
		wt = rc.mainTree()
	}
	if exp.Declared, err = declaredNames(wt.Repo, l); err != nil {
		return nil, err
	}
	stub, err := stubFor(rc.plan.Contracts, rc.policy, l)
	if err != nil {
		return nil, err
	}
	swap := NewSwap(wt.Repo, l, wt.Git, stub)
	if rc.diffOnly || wt.Private {
		swap.NoCommit(rc.o.RunDir) // a private tree commits nothing (spec section 6)
	}
```

and in the `lr := &leafRun{...}` literal add `wt: wt, stub: stub, turn: in.Turn,`.

(d) In `runLeaf` line 335 change `if in.Repair == 0 {` to `if in.Repair == 0 && !lr.wt.Private {` (adoption reads the main tree; a private tree starts stub-only by construction, Task 7).

(e) Add after `inProgress`:

```go
// checkLeaf is runner.LeafCheck on the leaf's real file in its work tree.
func (lr *leafRun) checkLeaf(ctx context.Context) runner.Verdict {
	return lr.rc.leafCheckIn(ctx, lr.wt, lr.l, []string{lr.l.File})
}
```

(f) Switch the reads and writes to the work tree: in `adoptOnDisk` `rc.git.Dirty()` becomes `lr.wt.Git.Dirty()`, `filepath.Join(rc.o.Repo, ...)` becomes `filepath.Join(lr.wt.Repo, ...)`, `TakeSnapshot(rc.o.Repo, rc.git)` becomes `TakeSnapshot(lr.wt.Repo, lr.wt.Git)`, and `settle(rc.checkLeaf(ctx, l))` becomes `settle(lr.checkLeaf(ctx))` (`rc.git.LeafCommit` stays: adoption only ever runs in the main tree); in `cleanStray` use `snap.Stray(lr.wt.Repo, lr.wt.Git, l.File, l.StubFile)` and `Revert(lr.wt.Git, stray)`; in `testSource` read `filepath.Join(lr.wt.Repo, filepath.FromSlash(l.TestFile))`; in `check` use `TakeSnapshot(lr.wt.Repo, lr.wt.Git)` and `settle(lr.checkLeaf(ctx))`.

In `gophermind-lib/briefv2/executor/env.go` delete `checkLeaf` (lines 121-124): its two callers now use `lr.checkLeaf`.

- [ ] **Step 4: Run the new tests and the leaf-loop tests, with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/executor/ -run 'TestTurn|TestAJobThatEnds|TestCandidateRoundTrip|TestLeafChecksGoTo|TestLeaf|TestLadder|TestAdopt|TestStray|TestSwap|TestScheduler|TestRevise|TestClaimRace' -race -count=3 -timeout 45m -parallel 2
```
Expected: `ok`. Every existing leaf test still passes: with `in.Tree` nil the leaf works in `rc.mainTree()`, which is exactly `rc.o.Repo`, `rc.git` and `rc.chk`.

- [ ] **Step 5: Commit.**

```bash
git add gophermind-lib/briefv2/executor/turn.go gophermind-lib/briefv2/executor/turn_test.go gophermind-lib/briefv2/executor/candidate.go gophermind-lib/briefv2/executor/candidate_test.go gophermind-lib/briefv2/executor/leaftree_test.go gophermind-lib/briefv2/executor/leaf.go gophermind-lib/briefv2/executor/env.go
git commit -m "refactor(executor): the leaf loop works through a work tree; the turn and candidate files (sub-project F task 5)" -m "Ruling 10: a candidate is JSON with revision, entry, tier, position, reply hash and source; garbage is removed and read as absent. Ruling 15: a turn takes the scheduler's body mutex." -m "$TRAILERS"
```

## Task 6: Private pass, landing, conflicts, the revise hand-off and the resumed candidate

**Files:**
- Create: `B2/executor/land.go`, `B2/executor/land_test.go`
- Modify: `B2/executor/leaf.go` (`revResult` lines 68-76, `runLeaf` lines 335-364, `afterRevision` lines 492-493, `finishPass` lines 783-786, `oneRevision` lines 889-899, `runEntry` line 923, `check` lines 1263-1264)
- Modify: `B2/executor/start.go:83-88` (`runReport.conflicts`)

**Interfaces:**
- Consumes: Task 5 names; `lr.gate`, `lr.record`, `lr.cleanStray`, `lr.finishPass`, `failureOf`, `failureText`, `failureReason`, `settle`.
- Produces:

```go
const revCandidate // revResult: a private pass was saved
type landResult int; const landPassed, landConflict, landInterrupted
func (lr *leafRun) privatePass(ctx context.Context, e ladderEntry, reply packer.Reply) (entryEnd, bool, error)
func (lr *leafRun) toMain(ctx context.Context) error
func (lr *leafRun) land(ctx context.Context) (landResult, error)
func (lr *leafRun) landCandidate(ctx context.Context) (leafOutcome, bool, error)
func (lr *leafRun) resumeCandidate(ctx context.Context) (leafOutcome, bool, error)
func (rc *runCtx) noteConflict()
// runReport gains conflicts int (repMu)
// events: leaf_candidate, leaf_landed (short hash or "uncommitted"), land_conflict (the class)
```

- [ ] **Step 1: Write the failing tests** in `gophermind-lib/briefv2/executor/land_test.go`:

```go
package executor

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/runner"
)

// privateRunCtx runs one leaf the way a parallel level does: a private tree
// from a fresh base copy and a turn that follows prev (nil: the turn is free).
// tree, when set, receives the tree's path. Tree and base are removed after.
func privateRunCtx(t *testing.T, ctx context.Context, rc *runCtx, id string, prev <-chan struct{}, tree *string) (leafOutcome, error) {
	t.Helper()
	if err := rc.snapshotLevel(); err != nil {
		t.Fatal(err)
	}
	defer rc.endLevel()
	wt, err := rc.makeTree(id)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.dropTree(wt)
	if tree != nil {
		*tree = wt.Repo
	}
	tn := newTurn(prev, &rc.sched.body)
	defer tn.release()
	return rc.runLeaf(ctx, rc.plan.Leaf(id), leafIn{Tree: wt, Turn: tn})
}

func privateRun(t *testing.T, rc *runCtx, id string, prev <-chan struct{}, tree *string) (leafOutcome, error) {
	t.Helper()
	return privateRunCtx(t, context.Background(), rc, id, prev, tree)
}

// eventOrder is the kinds, among those named, of the leaf's events in emission order.
func eventOrder(c *events.Collector, id string, kinds ...string) []string {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var out []string
	for _, e := range c.Events() {
		if e.NodeID == id && want[e.Kind] {
			out = append(out, e.Kind)
		}
	}
	return out
}

func candidatePath(g *rig, id string) string {
	return filepath.Join(g.runDir, "_state", candidatePrefix+id)
}

// repoLog records the repository of every leaf check.
func repoLog(fc *fakeChecker) func() []string {
	var mu sync.Mutex
	var repos []string
	fc.Hook = func(repo string) { mu.Lock(); repos = append(repos, repo); mu.Unlock() }
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), repos...) }
}

func TestAPrivatePassLandsInTheMainTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	repos := repoLog(fc)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), passVerdict()}
	var tree string
	out, err := privateRun(t, rc, leafID, nil, &tree)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if got := repos(); len(got) != 2 || got[0] != tree || got[1] != g.repo {
		t.Fatalf("checks ran in %q, want the tree then the main tree", got)
	}
	if n := g.leafCommits(leafID); n != 1 {
		t.Fatalf("leaf commits = %d", n)
	}
	if v := verdictsOf(t, g.board, leafID); v != "pass" {
		t.Fatalf("attempts = %s, want the one pass recorded at landing", v)
	}
	if a := attemptsOf(t, g.board, leafID)[0]; a.Provider != "a" || a.Model != "m1" {
		t.Fatalf("pass attempt = %+v", a)
	}
	if got := eventOrder(g.sink, leafID, "leaf_candidate", "leaf_landed", "leaf_verified"); !reflect.DeepEqual(got, []string{"leaf_candidate", "leaf_landed", "leaf_verified"}) {
		t.Fatalf("events = %v", got)
	}
	if ev := g.sink.OfKind("leaf_landed"); len(ev) != 1 || ev[0].Message == "" || ev[0].Message == "uncommitted" {
		t.Fatalf("leaf_landed = %+v, want the short hash", ev)
	}
	if fileExists(candidatePath(g, leafID)) || fileExists(tree) {
		t.Fatal("the candidate or the tree outlived the landing")
	}
}

func TestALandingConflictContinuesInTheMainTree(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(variant(good(leafID), 1)), reply(good(leafID))}}, true)
	repos := repoLog(fc)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), failVerdict(runner.ClassBuild, "./greet.go:9: clean redeclared in this block"), passVerdict()}
	var tree string
	out, err := privateRun(t, rc, leafID, nil, &tree)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if got := repos(); len(got) != 3 || got[0] != tree || got[1] != g.repo || got[2] != g.repo {
		t.Fatalf("checks ran in %q", got)
	}
	if ev := g.sink.OfKind("land_conflict"); len(ev) != 1 || ev[0].NodeID != leafID || ev[0].Message != runner.ClassBuild {
		t.Fatalf("land_conflict = %+v", ev)
	}
	if v := verdictsOf(t, g.board, leafID); v != "fail,pass" {
		t.Fatalf("attempts = %s, want the failed landing then the pass", v)
	}
	calls := g.leafCalls(leafID)
	if len(calls) != 2 || !strings.Contains(userText(calls[1]), "clean redeclared") {
		t.Fatal("the attempt after the conflict does not carry the landing failure")
	}
	repMu.Lock()
	n := rc.rep.conflicts
	repMu.Unlock()
	if n != 1 || g.leafCommits(leafID) != 1 || fileExists(candidatePath(g, leafID)) {
		t.Fatalf("conflicts %d, commits %d", n, g.leafCommits(leafID))
	}
}

// Review Focus 2: a candidate edited while it waited (a forbidden import and a
// secret) is refused by the gate at landing; nothing of it is committed or stored.
func TestATamperedCandidateIsRefusedAtLanding(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(variant(good(leafID), 1)), reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict(), passVerdict()}
	turnFree := make(chan struct{})
	go func() {
		defer close(turnFree)
		for i := 0; i < 600 && !fileExists(candidatePath(g, leafID)); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		c, found, err := readCandidate(g.runDir, leafID)
		if err != nil || !found {
			t.Error("no candidate was saved")
			return
		}
		c.Source = strings.Replace(c.Source, `import "strings"`, "import (\n\t\"os/exec\"\n\t\"strings\"\n)\n\nvar _ = exec.Command", 1) + "\n// " + canarySecret + "\n"
		if err := saveCandidate(g.runDir, leafID, c); err != nil {
			t.Error(err)
		}
	}()
	out, err := privateRun(t, rc, leafID, turnFree, nil)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if ev := g.sink.OfKind("land_conflict"); len(ev) != 1 || ev[0].Message != ClassImportNotAllowed {
		t.Fatalf("land_conflict = %+v, want the gate's class", ev)
	}
	if strings.Contains(g.gitCmd("log", "-p"), "os/exec") {
		t.Fatal("the tampered source was committed")
	}
	if hasCanary(t, g, canarySecret) {
		t.Fatal("the secret in the tampered candidate reached a store")
	}
}

// Review Focus 2: a candidate written for an earlier revision is dropped and
// the ladder makes a fresh attempt.
func TestAStaleCandidateIsDroppedAndTheLadderRuns(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if err := saveCandidate(g.runDir, leafID, candidate{Revision: 3, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "old", Source: good(leafID)}); err != nil {
		t.Fatal(err)
	}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if n := len(g.leafCalls(leafID)); n != 1 {
		t.Fatalf("model calls = %d, want the fresh attempt", n)
	}
	found := false
	for _, e := range g.sink.OfKind("warning") {
		found = found || (e.NodeID == leafID && e.Message == "a saved candidate of an earlier revision was dropped")
	}
	if !found || fileExists(candidatePath(g, leafID)) {
		t.Fatal("the stale candidate was not dropped with a warning")
	}
}

// Spec section 11 step 3: a saved candidate lands on the next run with no model call.
func TestASavedCandidateLandsWithoutAModelCall(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	if err := saveCandidate(g.runDir, leafID, candidate{Revision: 0, Entry: "a/m1", Tier: "standard", Pos: 1, SHA256: "s", Source: good(leafID)}); err != nil {
		t.Fatal(err)
	}
	if out := runOne(t, rc, leafID); out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v", out)
	}
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("model calls = %d, want none", n)
	}
	a := attemptsOf(t, g.board, leafID)
	if len(a) != 1 || a[0].Provider != "a" || a[0].Model != "m1" || a[0].ReplySHA256 != "s" {
		t.Fatalf("attempts = %+v", a)
	}
	if g.leafCommits(leafID) != 1 || fileExists(candidatePath(g, leafID)) {
		t.Fatal("the candidate was not committed once and removed")
	}
}

// Ruling 6: the revise rung runs in the main tree; the leaf moves there first.
func TestAPrivateLeafMovesToTheMainTreeBeforeItsRevise(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(1))
	rc, fc := g.leafRC(t, Script{
		"implement:" + leafID: append(distinct(leafID, 2), reply(good(leafID))),
		"revise:" + leafID:    {reviseHint("trim tabs too")},
	}, true)
	repos := repoLog(fc)
	failing(fc, leafID, 2)
	fc.LeafScript[leafID] = append(fc.LeafScript[leafID], passVerdict())
	var tree string
	out, err := privateRun(t, rc, leafID, nil, &tree)
	if err != nil || out.Status != blackboard.StatusVerified {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if got := repos(); len(got) != 3 || got[0] != tree || got[1] != tree || got[2] != g.repo {
		t.Fatalf("checks ran in %q, want two in the tree and the last in the main tree", got)
	}
	if got := strings.Join(stagesOf(g.fake), " "); got != "implement:fn-greet implement:fn-greet revise:fn-greet implement:fn-greet" {
		t.Fatalf("stages = %s", got)
	}
	if len(g.sink.OfKind("leaf_candidate")) != 0 || g.leafCommits(leafID) != 1 {
		t.Fatal("the leaf after its revise must pass in the main tree directly")
	}
}

// Spec section 10: a leaf waiting for its turn when the context ends keeps its
// candidate, releases its claim and commits nothing.
func TestAnInterruptedWaitKeepsTheCandidate(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.leafRC(t, Script{"implement:" + leafID: {reply(good(leafID))}}, true)
	fc.LeafScript[leafID] = []runner.Verdict{passVerdict()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for i := 0; i < 600 && !fileExists(candidatePath(g, leafID)); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	out, err := privateRunCtx(t, ctx, rc, leafID, make(chan struct{}), nil)
	if err != nil || !out.Interrupted {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	l := rc.plan.Leaf(leafID)
	if !fileExists(candidatePath(g, leafID)) || g.leafCommits(leafID) != 0 || !fileExists(g.stubPath(l)) || fileExists(g.realPath(l)) {
		t.Fatal("an interrupted wait must keep the candidate, commit nothing and leave the stub")
	}
	if r := g.row(t, leafID); r.Status != blackboard.StatusReady || r.Claim != nil {
		t.Fatalf("row = %s claim %v, want ready and released", r.Status, r.Claim)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/executor/ -run 'TestAPrivatePass|TestALandingConflict|TestATamperedCandidate|TestAStaleCandidate|TestASavedCandidate|TestAPrivateLeafMoves|TestAnInterruptedWait' -count=1 -timeout 45m
```
Expected: compile failure (`rc.rep.conflicts undefined`), then, once only that is added, each test failing because a private pass commits nothing and never lands.

- [ ] **Step 3: Implement** `gophermind-lib/briefv2/executor/land.go`:

```go
package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/packer"
	"gophermind/gophermind-lib/briefv2/runner"
)

// landResult is how a landing attempt ended.
type landResult int

const (
	landPassed      landResult = iota // committed through finishPass
	landConflict                      // a charged failed attempt: the ladder goes on in the main tree
	landInterrupted                   // the context ended: the candidate stays for the next run
)

// privatePass is a pass in a private tree (spec section 6): the reply is saved
// as the leaf's candidate, the stub is put back in the tree, and the ladder
// stops. No attempt is recorded: the blackboard sees the pass at landing.
func (lr *leafRun) privatePass(ctx context.Context, e ladderEntry, reply packer.Reply) (entryEnd, bool, error) {
	c := candidate{Revision: lr.rev, Entry: e.Name, Tier: string(e.Tier), Pos: e.Pos, SHA256: reply.SHA256, Source: string(reply.Source)}
	if err := saveCandidate(lr.rc.o.RunDir, lr.l.ID, c); err != nil {
		return endFailed, true, err
	}
	lr.rc.emit("leaf_candidate", lr.l.ID, "passed in its tree; waiting for its turn")
	if err := lr.swap.Fail(); err != nil {
		return endFailed, true, fmt.Errorf("executor: the stub of leaf %s could not be put back in its tree", lr.l.ID)
	}
	lr.cand = &c
	lr.art = attemptArt{}
	return endFailed, true, nil
}

// toMain moves a leaf from its private tree to the main tree once its turn
// came: the swap, the checker and the declared names are the main tree's from
// here on (the names now include what earlier leaves of the level landed). The
// private tree is removed by the job when the leaf ends. In the main tree it
// does nothing.
func (lr *leafRun) toMain(ctx context.Context) error {
	if !lr.wt.Private {
		return nil
	}
	if lr.turn == nil {
		return errors.New("executor: a leaf in a private tree has no turn")
	}
	if err := lr.turn.wait(ctx); err != nil {
		return err
	}
	main := lr.rc.mainTree()
	declared, err := declaredNames(main.Repo, lr.l)
	if err != nil {
		return err
	}
	swap := NewSwap(main.Repo, lr.l, main.Git, lr.stub)
	if lr.rc.diffOnly {
		swap.NoCommit(lr.rc.o.RunDir)
	}
	lr.wt, lr.swap, lr.expect.Declared = main, swap, declared
	return nil
}

// land replays the saved candidate in the main tree (spec section 7.2): the
// reply gate again (ruling 9), the stub swap, gofmt, build, vet, the named test
// and the stray-file check. A pass commits through finishPass exactly as a
// serial pass does. Any failure is a landing conflict (section 7.3): a charged
// attempt with its real class, the stub back, the candidate removed, a
// land_conflict event, and the failure fed to the next attempt. A context that
// ended keeps the candidate for the next run.
func (lr *leafRun) land(ctx context.Context) (landResult, error) {
	rc, l := lr.rc, lr.l
	c := *lr.cand
	lr.cand = nil
	e := c.entry()
	started := rc.o.Now()
	src := []byte(c.Source)
	lr.art = attemptArt{source: src}
	conflict := func(class, reason string, v *runner.Verdict, next packer.Failure) (landResult, error) {
		if err := lr.record(ctx, e, started, blackboard.VerdictFail, class, reason, v, c.SHA256); err != nil {
			return landConflict, err
		}
		if err := removeCandidate(rc.o.RunDir, l.ID); err != nil {
			return landConflict, err
		}
		rc.noteConflict()
		rc.emit("land_conflict", l.ID, class)
		lr.prev = next
		return landConflict, nil
	}
	if class, note := lr.gate(src); class != "" {
		return conflict(class, failureReason(class, nil), nil, failureText(note))
	}
	snap, err := TakeSnapshot(lr.wt.Repo, lr.wt.Git)
	if err != nil {
		return landConflict, err
	}
	lr.streakSince = time.Now()
	if err := lr.swap.Enter(src); err != nil {
		return landConflict, fmt.Errorf("executor: writing the file of leaf %s failed", l.ID)
	}
	v := settle(lr.checkLeaf(ctx))
	lr.art.out = v.Out.WholeLines()
	stray, err := lr.cleanStray(snap)
	if err != nil {
		return landConflict, err
	}
	if v.Faulted() {
		if ctx.Err() != nil || v.Class == runner.ClassCancelled {
			if ferr := lr.swap.Fail(); ferr != nil {
				return landInterrupted, fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
			}
			return landInterrupted, nil // the candidate file stays in _state
		}
		return landConflict, fmt.Errorf("executor: the check of leaf %s could not run: %w", l.ID, v.Err)
	}
	crit := rc.criticalSince(l.ID, lr.streakSince)
	var class, reason string
	var next packer.Failure
	switch {
	case len(stray) > 0:
		class, next, reason = ClassForbiddenWrite, failureText(noteStray), failureReason(ClassForbiddenWrite, nil)
	case len(crit) > 0:
		class, next, reason = ClassNetworkCritical, failureText(noteCritical), failureReason(ClassNetworkCritical, crit)
	case !v.Pass():
		class, next, reason = v.Class, failureOf(v, rc.secretValues()), v.Reason()
	default:
		recordPass := func() error {
			return lr.record(ctx, e, started, blackboard.VerdictPass, "pass", "", &v, c.SHA256)
		}
		if err := lr.finishPass(ctx, false, "", recordPass); err != nil {
			return landConflict, err
		}
		msg := lr.commit
		if msg == "" {
			msg = "uncommitted"
		}
		rc.emit("leaf_landed", l.ID, msg)
		return landPassed, nil
	}
	if err := lr.swap.Fail(); err != nil {
		return landConflict, fmt.Errorf("executor: the stub of leaf %s could not be restored", l.ID)
	}
	return conflict(class, reason, &v, next)
}

// landCandidate is the leaf's turn: take the main tree, replay the candidate.
// done is true when the leaf ended (verified, or interrupted with its candidate
// kept); false after a conflict, when the ladder continues in the main tree
// from its first entry (ruling 8).
func (lr *leafRun) landCandidate(ctx context.Context) (leafOutcome, bool, error) {
	if err := lr.toMain(ctx); err != nil {
		if ctx.Err() != nil {
			return leafOutcome{Interrupted: true}, true, nil
		}
		return leafOutcome{}, true, err
	}
	res, err := lr.land(ctx)
	switch {
	case err != nil:
		return leafOutcome{}, true, err
	case res == landPassed:
		return leafOutcome{Status: blackboard.StatusVerified}, true, nil
	case res == landInterrupted:
		return leafOutcome{Interrupted: true}, true, nil
	}
	return leafOutcome{}, false, nil
}

// resumeCandidate lands a candidate an earlier process saved (spec section 11
// step 3), with no model call. A candidate of an earlier revision is dropped
// with a warning (ruling 10). done is false when there was none or after a
// landing conflict.
func (lr *leafRun) resumeCandidate(ctx context.Context) (leafOutcome, bool, error) {
	rc, l := lr.rc, lr.l
	c, found, err := readCandidate(rc.o.RunDir, l.ID)
	if err != nil || !found {
		return leafOutcome{}, false, err
	}
	if c.Revision != lr.rev {
		rc.emit("warning", l.ID, "a saved candidate of an earlier revision was dropped")
		return leafOutcome{}, false, removeCandidate(rc.o.RunDir, l.ID)
	}
	lr.cand = &c
	return lr.landCandidate(ctx)
}

// noteConflict counts one landing conflict for the report.
func (rc *runCtx) noteConflict() {
	repMu.Lock()
	rc.rep.conflicts++
	repMu.Unlock()
}
```

Edit `gophermind-lib/briefv2/executor/leaf.go`:

(a) `revResult` constants (lines 70-76): add after `revInterrupted`

```go
	revCandidate                 // a private pass was saved: the leaf waits for its turn and lands (sub-project F)
```

(b) `runLeaf`: replace lines 335-363 with

```go
	if in.Repair == 0 && !lr.wt.Private {
		done, o, aerr := lr.adoptOnDisk(ctx)
		if aerr != nil {
			return lr.exit(ctx, leafOutcome{}, aerr)
		}
		if done {
			return o, nil
		}
		if ctx.Err() != nil {
			return leafOutcome{Interrupted: true}, nil
		}
		if o, done, cerr := lr.resumeCandidate(ctx); cerr != nil || done {
			return lr.exit(ctx, o, cerr)
		}
	}

	for {
		res, rerr := lr.oneRevision(ctx)
		if rerr != nil {
			return lr.exit(ctx, leafOutcome{}, rerr)
		}
		switch res {
		case revVerified:
			return leafOutcome{Status: blackboard.StatusVerified}, nil
		case revInterrupted:
			return leafOutcome{Interrupted: true, Reason: lr.unavailable}, nil
		case revCandidate:
			o, done, lerr := lr.landCandidate(ctx)
			if lerr != nil || done {
				return lr.exit(ctx, o, lerr)
			}
			continue // a landing conflict: the ladder goes on in the main tree
		}
		o, done, aerr := lr.afterRevision(ctx, res)
		if aerr != nil || done {
			return lr.exit(ctx, o, aerr)
		}
	}
```

(c) `afterRevision`: right after `if doRevise {` (line 492) insert

```go
		// The revise rung edits plan files, the test file and the work branch,
		// which only the turn holder may touch (ruling 6).
		if err := lr.toMain(ctx); err != nil {
			if ctx.Err() != nil {
				return leafOutcome{Interrupted: true}, true, nil
			}
			return leafOutcome{}, true, err
		}
```

(d) `finishPass`: replace lines 783-786 with

```go
	lr.commit = hash
	lr.passed = true
	if err := removeReopenMarker(rc.o.RunDir, l.ID); err != nil { // a repair that was committed before a kill
		return err
	}
	if err := removeCandidate(rc.o.RunDir, l.ID); err != nil { // ruling 11
		return err
	}
```

(e) `oneRevision`: in the loop's `default:` add after `if lr.passed { return revVerified, nil }`

```go
			if lr.cand != nil {
				return revCandidate, nil
			}
```

and in the final `switch` add `case lr.cand != nil: return revCandidate, nil` right after `case lr.passed:`.

(f) `runEntry` line 923: `if lr.passed || lr.cand != nil {`.

(g) `check`: at the start of the `default:` branch (line 1263) insert

```go
		if lr.wt.Private {
			return lr.privatePass(ctx, e, reply)
		}
```

In `gophermind-lib/briefv2/executor/start.go` add to `runReport`: `conflicts int // landing conflicts (sub-project F), under repMu`.

- [ ] **Step 4: Run, with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/executor/ -run 'TestAPrivatePass|TestALandingConflict|TestATamperedCandidate|TestAStaleCandidate|TestASavedCandidate|TestAPrivateLeafMoves|TestAnInterruptedWait|TestLeaf|TestLadder|TestAdopt|TestResume|TestRevise|TestRewrite' -race -count=3 -timeout 45m -parallel 2
```
Expected: `ok`. Failure guide: `no reply left for stage "implement:fn-greet"` after a conflict means `runLeaf` returned instead of continuing; a commit in the private tree means `NoCommit` is not set for `wt.Private`.

- [ ] **Step 5: Commit.**

```bash
git add gophermind-lib/briefv2/executor/land.go gophermind-lib/briefv2/executor/land_test.go gophermind-lib/briefv2/executor/leaf.go gophermind-lib/briefv2/executor/start.go
git commit -m "feat(executor): a private pass is saved, lands in its turn, and a conflict goes on in the main tree (sub-project F task 6)" -m "Ruling 6: the revise rung runs in the main tree. Ruling 7: the gate may be asked from a private tree. Ruling 8: after a conflict the revision's ladder restarts in the main tree. Ruling 9: landing re-runs the reply gate. Ruling 10: a stale candidate is dropped with a warning. Ruling 11: finishPass removes the candidate." -m "$TRAILERS"
```

## Task 7: The scheduler runs a level in parallel

**Files:**
- Modify: `B2/executor/schedule.go:35-40` (body comment), `:67-82` (job, workerCount), `:143-188` (scheduleLeaves), `:220-302` (runJobs); add `splitOverlaps`, `runnable`, `needsMain`, `levelWorkers`, `runJob`, `runParallelJob`, `noteParallel`
- Modify: `B2/executor/schedule_test.go:411-455` (replace `TestWorkerCountIsAlwaysOne` and `TestNeverTwoLeafBodiesAtOnce`)
- Modify: `B2/executor/start.go` (`runCtx.afterTree`, `runReport.parallelLevels`)
- Create: `B2/executor/parallel_test.go`

**Interfaces:**
- Consumes: everything of Tasks 4 to 6; `readReopenMarker` (`state.go:399`); `NewSwap(...).State()`.
- Produces:

```go
// job gains: main bool
func runnable(j *job) bool
func (rc *runCtx) workerCount(jobs []*job) int // replaces workerCount(leaves []*Leaf)
func splitOverlaps(level []*Leaf) [][]*Leaf
func (rc *runCtx) needsMain(l *Leaf, status blackboard.Status) (bool, error)
func (rc *runCtx) levelWorkers(jobs []*job, wave, level int) (int, error)
func (rc *runCtx) runJob(ctx context.Context, j *job) (leafOutcome, error)
func (rc *runCtx) runParallelJob(ctx context.Context, j *job, tn *turn) (leafOutcome, error)
func (rc *runCtx) noteParallel()
// runCtx gains: afterTree func(id string) (tests only); runReport gains parallelLevels int
// events: level_parallel ("wave W level L: N leaves, K workers"), tree_ready
// test helpers (parallel_test.go): workersN, twice, (g *rig) leafSubjects, subjectIDs, hookSink, barrier, newBarrier, blockingChecker
```

- [ ] **Step 1: Write the failing tests.** In `gophermind-lib/briefv2/executor/schedule_test.go` replace `TestWorkerCountIsAlwaysOne` and `TestNeverTwoLeafBodiesAtOnce` (lines 411-455) with:

```go
// workerCount is executor.workers capped by the jobs that may run in a private
// tree: main-tree, blocked and finished jobs do not count.
func TestWorkerCountIsTheSettingCappedByPrivateJobs(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, _ := g.schedRC(t, Script{})
	l := g.plan.Leaf("fn-greet")
	mk := func(n int, main bool) []*job {
		var out []*job
		for i := 0; i < n; i++ {
			out = append(out, &job{l: l, status: blackboard.StatusPending, main: main})
		}
		return out
	}
	for _, c := range []struct{ setting, jobs, want int }{{1, 5, 1}, {3, 5, 3}, {3, 2, 2}, {8, 1, 1}, {2, 0, 0}} {
		rc.cfg.Executor.Workers = c.setting
		if got := rc.workerCount(mk(c.jobs, false)); got != c.want {
			t.Errorf("workers %d, %d jobs: %d, want %d", c.setting, c.jobs, got, c.want)
		}
	}
	rc.cfg.Executor.Workers = 4
	jobs := append(mk(2, false), mk(3, true)...)
	jobs = append(jobs, &job{l: l, status: blackboard.StatusVerified}, &job{l: l, status: blackboard.StatusPending, blocker: "fn-x"})
	if got := rc.workerCount(jobs); got != 2 {
		t.Fatalf("workerCount = %d, want 2 (only private, runnable jobs count)", got)
	}
}

// With workers 1 two leaf bodies never overlap and no tree is made.
func TestASerialLevelNeverOverlaps(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	rc.cfg.Executor.Workers = 1
	var cur, max int32
	fc.Hook = func(repo string) {
		if repo != g.repo {
			t.Errorf("a serial level checked in %s", repo)
		}
		n := atomic.AddInt32(&cur, 1)
		for {
			m := atomic.LoadInt32(&max)
			if n <= m || atomic.CompareAndSwapInt32(&max, m, n) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop != nil || len(wr.Verified) != 2 {
		t.Fatalf("scheduleLeaves = %+v, %v", wr, err)
	}
	if m := atomic.LoadInt32(&max); m != 1 {
		t.Fatalf("%d leaf checks overlapped, want strictly one at a time", m)
	}
	if len(g.sink.OfKind("level_parallel"))+len(g.sink.OfKind("tree_ready")) != 0 {
		t.Fatal("a serial level made trees")
	}
}
```

Create `gophermind-lib/briefv2/executor/parallel_test.go`:

```go
package executor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/runner"
	"gophermind/gophermind-lib/briefv2/settings"
)

// workersN sets executor.workers after any rig option given before it.
func workersN(n int) func(*rigOpts) {
	return func(o *rigOpts) {
		prev := o.Settings
		o.Settings = func(c *settings.Config) {
			if prev != nil {
				prev(c)
			}
			c.Executor.Workers = n
		}
	}
}

// twice scripts a pass in the private tree and a pass at landing for each leaf.
func twice(fc *fakeChecker, ids ...string) {
	for _, id := range ids {
		fc.LeafScript[id] = append(fc.LeafScript[id], passVerdict(), passVerdict())
	}
}

// leafSubjects is the subject of every leaf commit on the current branch, oldest first.
func (g *rig) leafSubjects() []string {
	out := strings.TrimSpace(g.gitCmd("log", "--reverse", "--format=%s", "--grep=^gm(fn-"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// subjectIDs is the leaf id of each "gm(<id>): ..." subject.
func subjectIDs(subjects []string) []string {
	var out []string
	for _, s := range subjects {
		if id, _, ok := strings.Cut(strings.TrimPrefix(s, "gm("), "): "); ok {
			out = append(out, id)
		}
	}
	return out
}

// hookSink calls on, then passes the event on.
type hookSink struct {
	next events.Sink
	on   func(events.Event)
}

func (s hookSink) Emit(e events.Event) { s.on(e); s.next.Emit(e) }

// barrier lets its first n callers through together, or each after 20 s.
// arrive is true for a caller that met the others; later callers pass at once
// and get false.
type barrier struct {
	mu   sync.Mutex
	n    int
	seen int
	all  chan struct{}
}

func newBarrier(n int) *barrier { return &barrier{n: n, all: make(chan struct{})} }

func (b *barrier) arrive() bool {
	b.mu.Lock()
	b.seen++
	k := b.seen
	if k == b.n {
		close(b.all)
	}
	b.mu.Unlock()
	if k > b.n {
		return false
	}
	select {
	case <-b.all:
		return true
	case <-time.After(20 * time.Second):
		return false
	}
}

// blockingChecker blocks every private check until the context ends.
type blockingChecker struct {
	*fakeChecker
	main    string
	started chan string
}

func (b *blockingChecker) CheckLeaf(ctx context.Context, c runner.LeafCheck) runner.Verdict {
	if c.Repo == b.main {
		return b.fakeChecker.CheckLeaf(ctx, c)
	}
	b.started <- c.Repo
	<-ctx.Done()
	return runner.Verdict{Class: runner.ClassCancelled}
}

func treesLeft(t *testing.T, rc *runCtx) []string {
	t.Helper()
	var out []string
	for _, d := range []string{"trees", "w"} {
		entries, _ := os.ReadDir(filepath.Join(rc.scratch, d))
		for _, e := range entries {
			out = append(out, d+"/"+e.Name())
		}
	}
	if fileExists(filepath.Join(rc.scratch, "base")) {
		out = append(out, "base")
	}
	return out
}

func TestAParallelLevelOverlapsAndLandsInIDOrder(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell", "fn-greet")
	meet := newBarrier(2)
	var mu sync.Mutex
	var private []string
	met := 0
	fc.Hook = func(repo string) {
		if repo == g.repo {
			return
		}
		ok := meet.arrive()
		mu.Lock()
		private = append(private, repo)
		if ok {
			met++
		}
		mu.Unlock()
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-greet", "fn-farewell"))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("scheduleLeaves = %+v, %v", wr, err)
	}
	if met != 2 || len(private) != 2 || private[0] == private[1] {
		t.Fatalf("private checks %q, met %d: want two distinct trees at once", private, met)
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("commit order = %v", got)
	}
	var verified []string
	for _, e := range g.sink.OfKind("leaf_verified") {
		verified = append(verified, e.NodeID)
	}
	if !reflect.DeepEqual(verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("leaf_verified order = %v", verified)
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) != 1 || !strings.HasSuffix(ev[0].Message, "level 1: 2 leaves, 2 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	if n := len(g.sink.OfKind("tree_ready")); n != 2 {
		t.Fatalf("tree_ready events = %d", n)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Spec 15.7: cancel while the leaves of a parallel level are being checked.
func TestCancelDuringAParallelLevel(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	bc := &blockingChecker{fakeChecker: fc, main: g.repo, started: make(chan string, 4)}
	rc.chk = bc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-bc.started
		<-bc.started
		cancel()
	}()
	wr, err := rc.scheduleLeaves(ctx, g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" || len(wr.Verified)+len(wr.Failed) != 0 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	for _, id := range []string{"fn-farewell", "fn-greet"} {
		l := rc.plan.Leaf(id)
		if r := g.row(t, id); r.Status != blackboard.StatusReady || r.Claim != nil {
			t.Errorf("%s = %s claim %v, want ready and released", id, r.Status, r.Claim)
		}
		if g.leafCommits(id) != 0 || fileExists(candidatePath(g, id)) || !fileExists(g.stubPath(l)) {
			t.Errorf("%s: a commit, a candidate or a missing stub after the cancel", id)
		}
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Spec 15.8: a panic in one leaf fails that leaf; a panic outside a leaf body
// is a harness fault of its job; the other leaf lands and the process lives.
func TestPanicInOneLeafLetsTheOtherLand(t *testing.T) {
	t.Parallel()
	t.Run("inside the leaf", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, workersN(2))
		rc, fc := g.schedRC(t, Script{
			"implement:fn-farewell": {reply(good("fn-farewell"))},
			"implement:fn-greet":    {reply(good("fn-greet"))},
		})
		fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
		twice(fc, "fn-greet")
		fc.Hook = func(repo string) {
			if filepath.Base(repo) == "fn-farewell" {
				panic("boom")
			}
		}
		wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
		if err != nil || !reflect.DeepEqual(wr.Failed, []string{"fn-farewell"}) || !reflect.DeepEqual(wr.Verified, []string{"fn-greet"}) {
			t.Fatalf("wave = %+v, %v", wr, err)
		}
		if left := treesLeft(t, rc); len(left) != 0 {
			t.Fatalf("left behind: %v", left)
		}
	})
	t.Run("outside the leaf", func(t *testing.T) {
		t.Parallel()
		g := newRig(t, workersN(2))
		rc, fc := g.schedRC(t, Script{"implement:fn-farewell": {reply(good("fn-farewell"))}})
		twice(fc, "fn-farewell")
		rc.afterTree = func(id string) {
			if id == "fn-greet" {
				panic("tree glue")
			}
		}
		wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
		if err == nil || !strings.Contains(err.Error(), "panicked") || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell"}) {
			t.Fatalf("wave = %+v, %v", wr, err)
		}
		if r := g.row(t, "fn-greet"); r.Status != blackboard.StatusReady || r.Claim != nil {
			t.Fatalf("fn-greet = %s claim %v", r.Status, r.Claim)
		}
		if left := treesLeft(t, rc); len(left) != 0 {
			t.Fatalf("left behind: %v", left)
		}
	})
}

// F12: a level whose copy would be over the limit runs serially with a warning.
func TestATreeTooLargeRunsTheLevelSerially(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	f, err := os.Create(filepath.Join(g.repo, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTreeBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	assertSerialFallback(t, g, rc, fc, "this level runs one leaf at a time: the tree copy is over 256 MiB")
}

// Review Focus 1: a link out of the repository makes the level serial, not a failure.
func TestALinkOutOfTheTreeRunsTheLevelSerially(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	if err := os.Symlink("/etc/hosts", filepath.Join(g.repo, "hosts-link")); err != nil {
		t.Fatal(err)
	}
	assertSerialFallback(t, g, rc, fc, "this level runs one leaf at a time: a file of the repository cannot be copied into a private tree")
}

func assertSerialFallback(t *testing.T, g *rig, rc *runCtx, fc *fakeChecker, warning string) {
	t.Helper()
	fc.Hook = func(repo string) {
		if repo != g.repo {
			t.Errorf("a check ran in %s", repo)
		}
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	found := false
	for _, e := range g.sink.OfKind("warning") {
		found = found || e.Message == warning
	}
	if !found || len(g.sink.OfKind("level_parallel")) != 0 {
		t.Fatalf("want the warning %q and no parallel level; warnings %+v", warning, g.sink.OfKind("warning"))
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Review Focus 5: two leaves sharing a path go to different sub-levels.
func TestSplitOverlapsPutsASharedPathInALaterSubLevel(t *testing.T) {
	t.Parallel()
	a := &Leaf{ID: "fn-a", File: "p/a.go", StubFile: "p/zz_gm_stub_fn-a.go", TestFile: "p/shared_test.go"}
	b := &Leaf{ID: "fn-b", File: "p/b.go", StubFile: "p/zz_gm_stub_fn-b.go", TestFile: "p/shared_test.go"}
	c := &Leaf{ID: "fn-c", File: "p/c.go", StubFile: "p/zz_gm_stub_fn-c.go", TestFile: "p/fn_c_test.go"}
	ids := func(ls []*Leaf) string {
		var out []string
		for _, l := range ls {
			out = append(out, l.ID)
		}
		return strings.Join(out, " ")
	}
	got := splitOverlaps([]*Leaf{a, b, c})
	if len(got) != 2 || ids(got[0]) != "fn-a fn-c" || ids(got[1]) != "fn-b" {
		t.Fatalf("sub-levels = %d: %v", len(got), got)
	}
	if got := splitOverlaps([]*Leaf{a, c}); len(got) != 1 {
		t.Fatalf("leaves that share nothing were split: %d", len(got))
	}
}

func TestOverlappingLeavesOfALevelLandOneAfterTheOther(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	fc.LeafScript["fn-farewell"] = []runner.Verdict{passVerdict()}
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict()}
	ls := g.leaves("fn-farewell", "fn-greet")
	ls[1].TestFile, ls[1].TestSHA256 = ls[0].TestFile, ls[0].TestSHA256 // a plan defect: one test file for two leaves
	wr, err := rc.scheduleLeaves(context.Background(), ls)
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if len(g.sink.OfKind("level_parallel")) != 0 || len(g.sink.OfKind("tree_ready")) != 0 {
		t.Fatal("overlapping leaves ran in parallel")
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("commit order = %v", got)
	}
}

// needsMain: a real file on disk, a candidate, a reopen marker or an
// escalated row keeps a leaf in the main tree (ruling 5).
func TestNeedsMainKeepsResumedLeavesInTheMainTree(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, _ := g.schedRC(t, Script{})
	farewell, greet := rc.plan.Leaf("fn-farewell"), rc.plan.Leaf("fn-greet")
	if m, err := rc.needsMain(greet, blackboard.StatusPending); err != nil || m {
		t.Fatalf("a stub-only leaf: %v %v", m, err)
	}
	if m, _ := rc.needsMain(greet, blackboard.StatusEscalated); !m {
		t.Fatal("an escalated leaf may run in a private tree")
	}
	if err := saveCandidate(g.runDir, "fn-greet", candidate{Entry: "a/m1", Source: good("fn-greet")}); err != nil {
		t.Fatal(err)
	}
	if m, _ := rc.needsMain(greet, blackboard.StatusReady); !m {
		t.Fatal("a leaf with a candidate may run in a private tree")
	}
	g.plantReal(t, "fn-farewell", good("fn-farewell"))
	if m, _ := rc.needsMain(farewell, blackboard.StatusReady); !m {
		t.Fatal("a leaf with a real file on disk may run in a private tree")
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/executor/ -run 'TestWorkerCountIsTheSetting|TestASerialLevel|TestAParallelLevel|TestCancelDuringAParallel|TestPanicInOneLeaf|TestATreeTooLarge|TestALinkOutOf|TestSplitOverlaps|TestOverlappingLeaves|TestNeedsMain' -count=1 -timeout 45m
```
Expected: compile failure (`job.main undefined`).

- [ ] **Step 3: Implement.** In `gophermind-lib/briefv2/executor/schedule.go`:

(a) `schedState.body` comment (line 39): `// held for the whole of one leaf body in a serial level, and by the turn holder of a parallel level`.

(b) `job` gains `main bool // a parallel level runs it in the main tree in its turn (needsMain)`.

(c) Replace `workerCount` (lines 77-82) with:

```go
// runnable says the job is to run: not blocked, not already finished.
func runnable(j *job) bool {
	return j.blocker == "" && j.status != blackboard.StatusVerified && j.status != blackboard.StatusFailed
}

// workerCount is how many leaves a level runs at once: executor.workers capped
// by the jobs that may run in a private tree. Below 2 the level runs as before,
// one leaf after the other in the main tree, with no copy.
func (rc *runCtx) workerCount(jobs []*job) int {
	n := 0
	for _, j := range jobs {
		if runnable(j) && !j.main {
			n++
		}
	}
	return min(rc.cfg.Executor.Workers, n)
}

// splitOverlaps puts leaves of a level whose File, StubFile or TestFile is the
// same path into different sub-levels, the higher id later (spec section 4):
// they cannot be isolated from each other. First fit, id order kept inside
// each sub-level. Used only when executor.workers > 1 (ruling 12).
func splitOverlaps(level []*Leaf) [][]*Leaf {
	var subs [][]*Leaf
	var used []map[string]bool
	for _, l := range level {
		paths := []string{l.File, l.StubFile, l.TestFile}
		placed := false
		for i := range subs {
			clash := false
			for _, p := range paths {
				clash = clash || used[i][p]
			}
			if !clash {
				subs[i] = append(subs[i], l)
				for _, p := range paths {
					used[i][p] = true
				}
				placed = true
				break
			}
		}
		if !placed {
			m := map[string]bool{}
			for _, p := range paths {
				m[p] = true
			}
			subs = append(subs, []*Leaf{l})
			used = append(used, m)
		}
	}
	return subs
}

// needsMain says a leaf of a parallel level must run in the main tree in its
// turn (ruling 5): the gate is to be asked again, a real file is on disk to
// adopt, a repair left a reopen marker, or a candidate waits to land.
func (rc *runCtx) needsMain(l *Leaf, status blackboard.Status) (bool, error) {
	if status == blackboard.StatusEscalated || status == blackboard.StatusNeedsRevision {
		return true, nil
	}
	if _, found, err := readStateBytes(rc.o.RunDir, candidatePrefix+l.ID); err != nil || found {
		return true, err
	}
	if _, marked, err := readReopenMarker(rc.o.RunDir, l.ID); err != nil || marked {
		return true, err
	}
	st, err := NewSwap(rc.o.Repo, l, rc.git, nil).State()
	if err != nil {
		return false, err
	}
	return st != SwapStubOnly, nil
}

// levelWorkers decides how a level runs. With executor.workers above 1 it marks
// the jobs that need the main tree; when at least two may run in private trees
// it copies the level's base and emits level_parallel. A base that cannot be
// made (over 256 MiB, a link out of the tree, a full disk) makes the level
// serial with a warning: nothing is lost (ruling 4).
func (rc *runCtx) levelWorkers(jobs []*job, wave, level int) (int, error) {
	if rc.cfg.Executor.Workers <= 1 {
		return 1, nil
	}
	for _, j := range jobs {
		if !runnable(j) {
			continue
		}
		m, err := rc.needsMain(j.l, j.status)
		if err != nil {
			return 0, err
		}
		j.main = m
	}
	serial := func() (int, error) {
		for _, j := range jobs {
			j.main = false
		}
		return 1, nil
	}
	n := rc.workerCount(jobs)
	if n < 2 {
		return serial()
	}
	if err := rc.snapshotLevel(); err != nil {
		rc.emit("warning", "", "this level runs one leaf at a time: "+strings.TrimPrefix(err.Error(), "executor: "))
		rc.endLevel()
		return serial()
	}
	private := 0
	for _, j := range jobs {
		if runnable(j) && !j.main {
			private++
		}
	}
	rc.emit("level_parallel", "", fmt.Sprintf("wave %d level %d: %d leaves, %d workers", wave, level, private, n))
	rc.noteParallel()
	return n, nil
}

// noteParallel counts one parallel level for the report.
func (rc *runCtx) noteParallel() {
	repMu.Lock()
	rc.rep.parallelLevels++
	repMu.Unlock()
}
```

Add `"strings"` to the imports.

(d) In `scheduleLeaves` replace lines 169-187 (from `workers := rc.workerCount(leaves)` to the end of the level loop) with:

```go
	var firstErr error
	level := 0
	for _, lv := range levels(leaves) {
		subs := [][]*Leaf{lv}
		if rc.cfg.Executor.Workers > 1 {
			subs = splitOverlaps(lv)
		}
		for _, sub := range subs {
			if wr.Stop != nil || firstErr != nil {
				break
			}
			level++
			jobs, err := rc.classify(ctx, sub, &wr)
			if err != nil {
				return wr, err
			}
			workers, err := rc.levelWorkers(jobs, sub[0].Wave, level)
			if err != nil {
				return wr, err
			}
			var stop bool
			rc.runJobs(ctx, jobs, workers, &wr, &stop)
			if workers > 1 {
				rc.endLevel()
			}
			for _, j := range jobs {
				if err := rc.merge(ctx, j, &wr); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return wr, firstErr
```

(e) Replace `runJobs` (lines 220-302) with:

```go
// runJobs runs the jobs that are to run. With one worker it runs them one
// after the other in the main tree. With more it starts them in id order, at
// most `workers` at a time, each with a turn that follows the previous job's:
// private jobs work in their own tree and land in their turn; main jobs wait
// for their turn first.
func (rc *runCtx) runJobs(ctx context.Context, jobs []*job, workers int, wr *waveResult, stop *bool) {
	var mu sync.Mutex
	stopped := func() bool { mu.Lock(); defer mu.Unlock(); return *stop }
	halt := func(st *stopError) {
		mu.Lock()
		defer mu.Unlock()
		*stop = true
		if wr.Stop == nil && st != nil {
			wr.Stop = st
		}
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var prev <-chan struct{} // done of the previous job that ran (parallel levels)
	for _, j := range jobs {
		if !runnable(j) {
			continue
		}
		sem <- struct{}{}
		if stopped() {
			<-sem
			break
		}
		if ctx.Err() != nil {
			<-sem
			halt(interruptStop(""))
			break
		}
		if rc.sched.interrupted(j.l.ID) >= maxLeafInterrupts {
			<-sem
			rc.emit("interrupted", j.l.ID, "interrupted_repeatedly")
			halt(&stopError{Status: "interrupted", Reason: "interrupted_repeatedly",
				Message: fmt.Sprintf("executor: leaf %s was interrupted earlier in this run and is not claimed again", j.l.ID)})
			break
		}
		if j.status == blackboard.StatusClaimed || j.status == blackboard.StatusInProgress {
			<-sem
			rc.emit("interrupted", j.l.ID, "claim_held")
			halt(&stopError{Status: "interrupted", Reason: "claim_held",
				Message: fmt.Sprintf("executor: leaf %s is claimed by another worker whose heartbeat is current", j.l.ID)})
			break
		}
		if j.status == blackboard.StatusPending {
			if err := rc.o.Board.SetStatus(ctx, rc.plan.RunID, j.l.ID, blackboard.StatusPending, blackboard.StatusReady); err != nil {
				<-sem
				j.err = fmt.Errorf("executor: making %s ready failed", j.l.ID)
				halt(nil)
				break
			}
		}
		j.ran = true
		var tn *turn
		if workers > 1 {
			tn = newTurn(prev, &rc.sched.body)
			prev = tn.done
		}
		run := func() {
			defer func() { <-sem }()
			if tn == nil {
				rc.sched.body.Lock()
				defer rc.sched.body.Unlock()
				j.out, j.err = rc.runJob(ctx, j)
			} else {
				defer tn.release()
				j.out, j.err = rc.runParallelJob(ctx, j, tn)
			}
			var se *stopError
			switch {
			case errors.As(j.err, &se):
				halt(se)
			case j.err != nil:
				halt(nil)
			case j.out.Interrupted:
				rc.sched.markInterrupted(j.l.ID)
				rc.emit("interrupted", j.l.ID, j.out.Reason)
				halt(interruptStop(j.out.Reason))
			}
		}
		if workers == 1 {
			run()
		} else {
			wg.Add(1)
			go func() { defer wg.Done(); run() }()
		}
	}
	wg.Wait()
}

// runJob is one leaf in the main tree: the gate again for a leaf an earlier
// run left escalated, else the leaf.
func (rc *runCtx) runJob(ctx context.Context, j *job) (leafOutcome, error) {
	if j.status == blackboard.StatusEscalated || j.status == blackboard.StatusNeedsRevision {
		return rc.resumeEscalated(ctx, j.l, j.status)
	}
	return rc.runLeaf(ctx, j.l, leafIn{})
}

// runParallelJob is one leaf of a parallel level. A main job waits for its
// turn and runs as runJob does. Any other gets a private tree, removed when the
// leaf ends, and lands in its turn. A panic outside the leaf body (tree setup,
// landing glue) is a harness fault of this job: the tree is removed, the
// caller releases the turn, and the process lives (spec section 10).
func (rc *runCtx) runParallelJob(ctx context.Context, j *job, tn *turn) (out leafOutcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			rc.emit("warning", j.l.ID, "a panic ended the job outside the leaf")
			out, err = leafOutcome{}, fmt.Errorf("executor: the job of leaf %s panicked", j.l.ID)
		}
	}()
	if j.main {
		if werr := tn.wait(ctx); werr != nil {
			return leafOutcome{Interrupted: true}, nil
		}
		return rc.runJob(ctx, j)
	}
	wt, terr := rc.makeTree(j.l.ID)
	if terr != nil {
		return leafOutcome{}, terr // never claimed: the leaf stays ready
	}
	defer rc.dropTree(wt)
	rc.emit("tree_ready", j.l.ID, "private tree ready")
	if rc.afterTree != nil {
		rc.afterTree(j.l.ID)
	}
	return rc.runLeaf(ctx, j.l, leafIn{Tree: wt, Turn: tn})
}
```

In `gophermind-lib/briefv2/executor/start.go` add to `runCtx`: `afterTree func(id string) // tests only: called once a private tree is ready, before the leaf runs` and to `runReport`: `parallelLevels int // levels that ran in parallel (sub-project F), under repMu`.

- [ ] **Step 4: Run the scheduler tests with the race detector, then the whole package.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/executor/ -run 'TestWorkerCount|TestASerialLevel|TestAParallelLevel|TestCancelDuringAParallel|TestPanicInOneLeaf|TestATreeTooLarge|TestALinkOutOf|TestSplitOverlaps|TestOverlappingLeaves|TestNeedsMain|TestTurn|TestScheduler|TestBlocked|TestClaimRace|TestStale|TestInterrupted|TestEscalatedLeafAskedAgain|TestCancelMidLeaf' -race -count=3 -timeout 45m -parallel 2
gt ./gophermind-lib/briefv2/executor/ -count=1 -timeout 45m -parallel 2
```
Expected: `ok` both times (the first full executor run of the plan; apply the flake rule).

- [ ] **Step 5: Commit.**

```bash
git add gophermind-lib/briefv2/executor/schedule.go gophermind-lib/briefv2/executor/schedule_test.go gophermind-lib/briefv2/executor/parallel_test.go gophermind-lib/briefv2/executor/start.go
git commit -m "feat(executor): the scheduler runs a level's leaves in private trees and lands them in id order (sub-project F task 7)" -m "Ruling 4: a base that cannot be made makes the level serial with a warning. Ruling 5: resumed, adopted and escalated leaves run in the main tree in their turn. Ruling 12: overlapping paths split into sub-levels only when workers > 1." -m "$TRAILERS"
```

## Task 8: Gate mutex, preflight notice, report `parallel`, state, resume message

**Files:**
- Modify: `B2/executor/start.go` (`runCtx.gateMu`; preflight notice after `checkChains`, line 321-323), `B2/executor/escalate.go:148-149`, `B2/executor/resume.go:126`, `B2/executor/run.go:347`, `B2/executor/state.go:33-44` (`State.Workers`), `B2/executor/schedule.go` (`noteParallel`)
- Modify: `B2/report/report.go:121-159,164-196,249-292`, `B2/report/summary.go:52`
- Create: `B2/executor/parallel_gate_test.go`, `B2/report/parallel_test.go`

**Interfaces:**
- Produces:

```go
// runCtx gains gateMu sync.Mutex
func modelConcurrency(cfg *settings.Config) int
func (rc *runCtx) parallelReport() *report.Parallel
type report.Parallel struct { Workers int `json:"workers"`; ParallelLevels int `json:"parallel_levels"`; LandingConflicts int `json:"landing_conflicts"` }
// report.Report.Parallel *Parallel `json:"parallel,omitempty"`; report.Input.Parallel *Parallel
// State.Workers int `json:"workers,omitempty"`
// summary line: "Parallel: workers N, levels L, landing conflicts C"
```

- [ ] **Step 1: Write the failing tests.** `gophermind-lib/briefv2/report/parallel_test.go`:

```go
package report_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/report"
)

func TestParallelSectionIsAdditive(t *testing.T) {
	r, err := report.Build(fixture())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), `"parallel"`) || strings.Contains(r.Summary(), "Parallel:") {
		t.Fatalf("a run with no parallel level carries the new key or line: %s", raw)
	}
	in := fixture()
	in.Parallel = &report.Parallel{Workers: 3, ParallelLevels: 2, LandingConflicts: 1}
	r, err = report.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(r)
	if !strings.Contains(string(raw), `"parallel":{"workers":3,"parallel_levels":2,"landing_conflicts":1}`) {
		t.Fatalf("parallel = %s", raw)
	}
	if !strings.Contains(r.Summary(), "Parallel: workers 3, levels 2, landing conflicts 1") || r.SchemaVersion != 3 {
		t.Fatalf("summary or schema: %d\n%s", r.SchemaVersion, r.Summary())
	}
	in.Parallel = &report.Parallel{Workers: 3}
	if r, _ := report.Build(in); r.Parallel != nil {
		t.Fatal("a run with zero parallel levels reports a parallel object")
	}
}
```

`gophermind-lib/briefv2/executor/parallel_gate_test.go`:

```go
package executor

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/human"
)

// overlapGate counts the escalations in flight and answers skip after a pause.
type overlapGate struct {
	*scriptGate
	cur, max, asked int32
}

func (o *overlapGate) Escalate(ctx context.Context, e human.Escalation) (human.Resolution, error) {
	atomic.AddInt32(&o.asked, 1)
	n := atomic.AddInt32(&o.cur, 1)
	for {
		m := atomic.LoadInt32(&o.max)
		if n <= m || atomic.CompareAndSwapInt32(&o.max, m, n) {
			break
		}
	}
	time.Sleep(150 * time.Millisecond)
	atomic.AddInt32(&o.cur, -1)
	return human.Resolution{Action: human.ActionSkip}, nil
}

// Spec 15.9 and F11: two leaves escalating at once reach the gate one at a time.
func TestTwoEscalationsReachTheGateOneAtATime(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0), workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": distinct("fn-farewell", 2),
		"implement:fn-greet":    distinct("fn-greet", 2),
	})
	failing(fc, "fn-farewell", 2)
	failing(fc, "fn-greet", 2)
	og := &overlapGate{scriptGate: g.gate}
	rc.o.Gate = og
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || !reflect.DeepEqual(wr.Failed, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if asked, max := atomic.LoadInt32(&og.asked), atomic.LoadInt32(&og.max); asked != 2 || max != 1 {
		t.Fatalf("the gate was asked %d times with %d at once, want 2 and 1", asked, max)
	}
}

// F8: workers above every provider's max_concurrent is a notice, not an error.
func TestWorkersAboveModelConcurrencyIsANotice(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(3))
	g.schedRC(t, Script{})
	found := false
	for _, e := range g.sink.OfKind("warning") {
		found = found || e.Message == "workers 3 exceeds model concurrency 1; extra workers only overlap checks"
	}
	if !found {
		t.Fatalf("warnings = %+v", g.sink.OfKind("warning"))
	}
	g1 := newRig(t)
	g1.schedRC(t, Script{})
	for _, e := range g1.sink.OfKind("warning") {
		if strings.Contains(e.Message, "model concurrency") {
			t.Fatal("workers 1 gave the notice")
		}
	}
}

// The report and the run state say a level ran in parallel.
func TestTheReportAndTheStateCountParallelLevels(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell", "fn-greet")
	if _, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet")); err != nil {
		t.Fatal(err)
	}
	rep, err := rc.buildReport(context.Background(), finishResult{Status: "failed", Reason: "leaves_not_verified"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Parallel == nil || rep.Parallel.Workers != 2 || rep.Parallel.ParallelLevels != 1 || rep.Parallel.LandingConflicts != 0 {
		t.Fatalf("parallel = %+v", rep.Parallel)
	}
	st, err := LoadState(g.runDir)
	if err != nil || st.Workers != 2 {
		t.Fatalf("state workers = %d, %v", st.Workers, err)
	}
}

// Ruling 16: a resume refused by live claims names every live leaf.
func TestResumeNamesEveryLiveLeaf(t *testing.T) {
	t.Parallel()
	g := newRig(t)
	g.started(t)
	g.plantDead(t, "fn-greet", true)
	g.plantDead(t, "fn-farewell", true)
	rc, err := startRun(context.Background(), g.options())
	if rc != nil {
		defer rc.close()
	}
	if err == nil || !strings.Contains(err.Error(), "nodes fn-farewell, fn-greet are held by live workers") {
		t.Fatalf("startRun error = %v", err)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

```bash
gt ./gophermind-lib/briefv2/report/ -run TestParallelSectionIsAdditive -count=1
gt ./gophermind-lib/briefv2/executor/ -run 'TestTwoEscalationsReachTheGate|TestWorkersAboveModelConcurrency|TestTheReportAndTheState|TestResumeNamesEveryLiveLeaf' -count=1 -timeout 45m
```
Expected: compile failures (`report.Parallel undefined`, `st.Workers undefined`).

- [ ] **Step 3: Implement.** In `gophermind-lib/briefv2/report/report.go` add after `RevisedLeaf`'s type (or after `Report`):

```go
// Parallel is what the executor's parallel levels did (sub-project F). It is
// present only when at least one level ran in parallel.
type Parallel struct {
	Workers          int `json:"workers"`
	ParallelLevels   int `json:"parallel_levels"`
	LandingConflicts int `json:"landing_conflicts"`
}
```

`Report` gains after `Rewritten`: `Parallel *Parallel \`json:"parallel,omitempty"\``; `Input` gains after `Revised`: `Parallel *Parallel`; in `Build`, after the `in.Revised` loop (line 292):

```go
	if in.Parallel != nil && in.Parallel.ParallelLevels > 0 {
		p := *in.Parallel
		r.Parallel = &p
	}
```

In `gophermind-lib/briefv2/report/summary.go` after line 52 (`Weak tests`):

```go
	if r.Parallel != nil {
		line("Parallel: workers %d, levels %d, landing conflicts %d", r.Parallel.Workers, r.Parallel.ParallelLevels, r.Parallel.LandingConflicts)
	}
```

In `gophermind-lib/briefv2/executor/state.go` `State` gains: `Workers int \`json:"workers,omitempty"\` // executor.workers of the last invocation that ran a parallel level (the report only)`.

In `gophermind-lib/briefv2/executor/schedule.go` replace `noteParallel`:

```go
// noteParallel counts one parallel level for the report and records the
// workers value in the run state (spec section 11: the report only).
func (rc *runCtx) noteParallel() {
	repMu.Lock()
	rc.rep.parallelLevels++
	repMu.Unlock()
	stateMu.Lock()
	defer stateMu.Unlock()
	if rc.state.Workers == rc.cfg.Executor.Workers {
		return
	}
	prev := rc.state.Workers
	rc.state.Workers = rc.cfg.Executor.Workers
	if err := rc.state.Save(rc.o.RunDir); err != nil {
		rc.state.Workers = prev
		rc.emit("warning", "", "the workers value could not be recorded in the run state")
	}
}
```

In `gophermind-lib/briefv2/executor/run.go` add after `buildReport`:

```go
// parallelReport is the report's parallel object, nil when no level ran in
// parallel in this invocation (ruling 23).
func (rc *runCtx) parallelReport() *report.Parallel {
	repMu.Lock()
	defer repMu.Unlock()
	if rc.rep.parallelLevels == 0 {
		return nil
	}
	return &report.Parallel{Workers: rc.cfg.Executor.Workers, ParallelLevels: rc.rep.parallelLevels, LandingConflicts: rc.rep.conflicts}
}
```

and on line 347 add `Parallel: rc.parallelReport(),` to the `report.Input` literal.

In `gophermind-lib/briefv2/executor/start.go` add `gateMu sync.Mutex // one question to the gate at a time (decision F11)` to `runCtx`, and after `if err := checkChains(cfg); err != nil { return err }` in `preflight`:

```go
	if w, m := cfg.Executor.Workers, modelConcurrency(cfg); w > 1 && w > m {
		rc.emit("warning", "", fmt.Sprintf("workers %d exceeds model concurrency %d; extra workers only overlap checks", w, m))
	}
```

and below `preflight`:

```go
// modelConcurrency is the largest max_concurrent among the providers the model
// chains name (decision F8, ruling 17).
func modelConcurrency(cfg *settings.Config) int {
	named := map[string]bool{}
	for _, chain := range cfg.Models {
		for _, e := range chain {
			p, _, _ := settings.SplitEntry(e)
			named[p] = true
		}
	}
	m := 0
	for _, p := range cfg.Providers {
		if named[p.Name] && p.MaxConcurrent > m {
			m = p.MaxConcurrent
		}
	}
	return m
}
```

In `gophermind-lib/briefv2/executor/escalate.go` wrap the call at line 148:

```go
	rc.gateMu.Lock()
	res, err := rc.o.Gate.Escalate(ctx, human.Escalation{NodeID: l.ID, Reason: reason, History: rc.scrubHistory(history),
		Revisions: revisions, Proposal: rc.scrubText(ex.Proposal)})
	rc.gateMu.Unlock()
```

In `gophermind-lib/briefv2/executor/resume.go` replace line 126:

```go
		if len(live) == 1 {
			return fmt.Errorf("executor: node %s is held by a live worker", live[0])
		}
		return fmt.Errorf("executor: nodes %s are held by live workers", strings.Join(live, ", "))
```

- [ ] **Step 4: Run, with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor gophermind-lib/briefv2/report && go vet ./gophermind-lib/briefv2/executor/ ./gophermind-lib/briefv2/report/
gt ./gophermind-lib/briefv2/report/ -count=1
gt ./gophermind-lib/briefv2/executor/ -run 'TestTwoEscalationsReachTheGate|TestWorkersAboveModelConcurrency|TestTheReportAndTheState|TestResumeNamesEveryLiveLeaf|TestResume|TestReport|TestEscalat|TestBriefRun' -race -count=3 -timeout 45m -parallel 2
gt ./cmd/gophermind/ -run 'TestBriefReport|TestRunSink' -count=1 -timeout 15m
```
Expected: `ok` each.

- [ ] **Step 5: Commit.**

```bash
git add gophermind-lib/briefv2/executor/start.go gophermind-lib/briefv2/executor/escalate.go gophermind-lib/briefv2/executor/resume.go gophermind-lib/briefv2/executor/run.go gophermind-lib/briefv2/executor/state.go gophermind-lib/briefv2/executor/schedule.go gophermind-lib/briefv2/executor/parallel_gate_test.go gophermind-lib/briefv2/report/report.go gophermind-lib/briefv2/report/summary.go gophermind-lib/briefv2/report/parallel_test.go
git commit -m "feat(executor,report): one gate question at a time, the concurrency notice, the parallel report object (sub-project F task 8)" -m "Ruling 13: schema_version stays 3, parallel is additive and omitted. Ruling 16: one live leaf keeps the old refusal text. Ruling 17: model concurrency is the largest max_concurrent among chained providers. Rulings 23 and 24: counters are per invocation; workers is the configured value." -m "$TRAILERS"
```

## Task 9: The data-race review, the ledger test, no leaked goroutines

**Files:**
- Create: `B2/executor/parallel_race_test.go`
- Modify: `B2/ledger/fs_test.go` (new test after `TestFSConcurrentRecordsGetDistinctIDs`, line 291; import `encoding/json`)

**Interfaces:**
- Produces: `func goroutinesSettle(t *testing.T, before int)` (used by Task 10).

- [ ] **Step 1: Read the code against the checklist of spec section 14 and write down each answer.** For each item, open the named code and confirm the stated guard; record the list verbatim in the commit body (step 6). The expected findings, from the code as this plan leaves it:

1. `runCtx` fields written after `startRun`: `state` (`stateMu`), `rep` (`repMu`: `reasons`, `blocked`, `conflicts`, `parallelLevels`, `weak`, `repairs`), `sched.interrupts` (`schedState.mu`), `baseFiles` (written by `snapshotLevel`/`endLevel` in the serial phase of `scheduleLeaves`, read by `makeTree` in job goroutines started after it and finished before `endLevel`), `serveAddr`, `bins`, `accept` (acceptance and wave checks, serial), `plan.Revised` (`revMu`), `profile` (preflight only), `chk`, `afterTree` (tests, before any wave).
2. `runJobs`: `j.out`, `j.err` are written by the job goroutine and read in `merge` after `wg.Wait()`; `j.ran`, `j.main` are written before the goroutine starts; `*stop` and `wr.Stop` only inside `halt` under `mu`; `wr.Verified` and friends only in `merge`.
3. The turn: `done` is closed once, in `release` under `sync.Once`, never sent on; `held` is touched only by the job's goroutine.
4. `leafRun` fields are touched only by its leaf's goroutine; the main-tree `Swap` is made in `toMain` after `turn.wait`.
5. The main `gitland.CLI` during a parallel level: `rc.git` in `leaf.go` is used only by `adoptOnDisk` (`LeafCommit`), which runs in the main tree only (`!lr.wt.Private`), i.e. a main job in its turn; `finishPass` (and `recordTip`, `tipNow` inside it) runs only in `land` after `toMain`, or in a main job; `needsMain` and `snapshotLevel` run in the serial phase. Static check: `grep -n 'rc\.git\.\|rc\.o\.Repo' gophermind-lib/briefv2/executor/leaf.go gophermind-lib/briefv2/executor/land.go` prints exactly the `LeafCommit` line of `adoptOnDisk`.
6. Each private tree has its own `gitland.CLI` and `Checker` (`makeTree`, `checkerFor`); `rc.chk` is only read.
7. `events.Sink`: `events.Collector` locks; the CLI's `runSink` and `printSink` lock `printSink.mu` (`cmd/gophermind/brief_run.go:334-373`, `brief_plan.go:43`); `projectrun.Sink` locks `s.mu` (`projectrun/sink.go:35`). The proxy log (`logMu`), `artifacts.Writer` (no mutable state: per-node folders) and `router.Router` (`r.mu`, slots) hold their own guards. Shared files: `_state/leaf-results.json` (`repMu`), notes and escalations (`notesMu`), the state file (`stateMu`), candidates (one file per leaf).
8. Test doubles: `fakeChecker` (`f.mu` around `LeafScript`, `calls`, `files`; `Hook` read under the lock and called outside it), `scriptedProvider` (`sp.mu`), `scriptGate` (`s.mu`), `memSecrets` (`m.mu`), `traceBoard` (`b.mu`), `blockingChecker`, `overlapGate` (atomics). Tests set `LeafScript` and `Hook` before a level starts.
9. `blackboard.FS.paths` is under `f.mu`, claims under per-node lock files: `TestClaimRaceExactlyOneWinner` with `-race`; the ledger under `calls.lock`: the new test below.
10. No goroutine outlives `scheduleLeaves`: heartbeats stop in `hbStop` (waits for the goroutine), trees are dropped before the job returns, `wg.Wait()` ends `runJobs`: the new goroutine test below.

Report and stop if any finding differs from the code.

- [ ] **Step 2: Write the failing ledger test** (it passes on the current ledger: it pins the property). In `gophermind-lib/briefv2/ledger/fs_test.go` add `"encoding/json"` to the imports and, after `TestFSConcurrentRecordsGetDistinctIDs`:

```go
// Spec 15.11 and open item 5: eight writers recording 200 calls each get unique
// ids, calls.seq ends at the largest id, every line parses, and no Record
// waits a second or more for the lock.
func TestFSEightWritersKeepIDsUniqueAndTheSeqExact(t *testing.T) {
	dir := t.TempDir()
	l := ledger.NewFS(runfs.Fixed(dir))
	ctx := context.Background()
	const writers, each = 8, 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	var maxWait time.Duration
	ids := make(chan int64, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				c := &ledger.Call{RunID: "r", Stage: fmt.Sprintf("implement:fn-%d", w), Provider: "p", Outcome: ledger.OutcomeOK}
				start := time.Now()
				if err := l.Record(ctx, c); err != nil {
					t.Error(err)
					return
				}
				d := time.Since(start)
				mu.Lock()
				if d > maxWait {
					maxWait = d
				}
				mu.Unlock()
				ids <- c.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	var max int64
	for id := range ids {
		if seen[id] {
			t.Fatalf("id %d handed out twice", id)
		}
		seen[id] = true
		if id > max {
			max = id
		}
	}
	if len(seen) != writers*each {
		t.Fatalf("%d ids, want %d", len(seen), writers*each)
	}
	seq, err := os.ReadFile(filepath.Join(dir, "_state", "calls.seq"))
	if err != nil || strings.TrimSpace(string(seq)) != fmt.Sprint(max) {
		t.Fatalf("calls.seq = %q, want %d (%v)", seq, max, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "_state", "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != writers*each {
		t.Fatalf("calls.jsonl has %d lines", len(lines))
	}
	for i, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("line %d does not parse: %v", i+1, err)
		}
	}
	t.Logf("longest Record: %s", maxWait)
	if maxWait >= time.Second {
		t.Fatalf("a Record waited %s for the lock; the plan makes 1 s a failure", maxWait)
	}
}
```

Create `gophermind-lib/briefv2/executor/parallel_race_test.go`:

```go
package executor

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// goroutinesSettle fails the test unless the goroutine count comes back to at
// most before within 5 s. Meaningful only in a test that is not parallel
// (ruling 20): Go pauses parallel tests while a sequential one runs.
func goroutinesSettle(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= before {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			buf = buf[:runtime.Stack(buf, true)]
			t.Fatalf("%d goroutines outlived the level (before %d):\n%s", n-before, before, buf)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Spec 14 item 10: no goroutine outlives a parallel level. Not parallel on purpose.
func TestNoGoroutineOutlivesAParallelLevel(t *testing.T) {
	g := newRig(t, workersN(2))
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell"))},
		"implement:fn-greet":    {reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell", "fn-greet")
	before := runtime.NumGoroutine()
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || wr.Stop != nil || len(wr.Verified) != 2 {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	goroutinesSettle(t, before)
}
```

- [ ] **Step 3: Run the ledger and blackboard packages with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/ledger gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/ledger/ ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/ledger/ ./gophermind-lib/briefv2/blackboard/ -race -count=3 -v -run 'TestFSEightWriters|TestFSConcurrent|Claim|Race' 2>&1 | grep -E '^(ok|FAIL|---|.*longest Record)'
```
Expected: `ok` for both packages and a `longest Record:` line under 1 s for each count.

- [ ] **Step 4: Run the goroutine test and the concurrency subset with the race detector.**

```bash
gt ./gophermind-lib/briefv2/executor/ -run 'TestNoGoroutineOutlives|TestAParallelLevel|TestCancelDuringAParallel|TestPanicInOneLeaf|TestTwoEscalations|TestTurn|TestClaimRaceExactlyOneWinner|TestChainsIntact|TestPlanIntactNeverFails|TestTwoTreesAreMade' -race -count=3 -timeout 45m -parallel 2
grep -n 'rc\.git\.\|rc\.o\.Repo' gophermind-lib/briefv2/executor/leaf.go gophermind-lib/briefv2/executor/land.go
```
Expected: `ok`; the grep prints exactly one line, `rc.git.LeafCommit(l.ID)` in `adoptOnDisk`.

- [ ] **Step 5: Fix what the review found.** If step 1 or step 4 found a field without a guard, add the guard in the file that owns the field, add a `-race` test that reaches it in `parallel_race_test.go`, and rerun step 4. If nothing was found, this step changes no file.

- [ ] **Step 6: Commit.** Put the ten findings of step 1 (one line each, as confirmed) into the second `-m`:

```bash
git add gophermind-lib/briefv2/ledger/fs_test.go gophermind-lib/briefv2/executor/parallel_race_test.go
git commit -m "test(executor,ledger): the data-race review of parallel levels, eight ledger writers, no leaked goroutines (sub-project F task 9)" -m "Race review (spec section 14): 1 runCtx fields guarded or serial-phase only; 2 runJobs results read after wg.Wait, stop under mu; 3 done closed once under sync.Once; 4 leafRun owned by its goroutine, main swap made after turn.wait; 5 main CLI only in the turn (grep: LeafCommit in adoptOnDisk only); 6 per-tree CLI and Checker; 7 sinks, proxy log, artifacts, router locked; 8 doubles locked; 9 claims and ledger raced under -race; 10 no goroutine outlives a level. Ruling 20: the goroutine check is its own sequential test." -m "$TRAILERS"
```

## Task 10: The six-leaf fixture, the acceptance test, determinism, the tree bound

**Files:**
- Modify: `B2/executor/rig_test.go:145` (newRigIn passes `PlannerDirs`)
- Create: `B2/executor/sixpack_test.go`, `B2/executor/parallel_accept_test.go`

**Interfaces:**
- Consumes: `workersN`, `twice`, `leafSubjects`, `subjectIDs`, `barrier` (Task 7); `goroutinesSettle` (Task 9); `fastChecker` (`run_test.go:34`).
- Produces:

```go
var sixNew []sixFn; var sixIDs []string // fn-cheer fn-farewell fn-greet fn-shout fn-thank fn-welcome
func sixGood(id string) string
func sixpackPlanner(t *testing.T) string
func sixpack(t *testing.T, workers int) func(*rigOpts)
func sixScript(g *rig) Script
func (g *rig) sixChecker() *fakeChecker // three passes per leaf, repo checks scripted
type recChecker struct { Checker; main string; meet *barrier; mu sync.Mutex; calls []runner.LeafCheck; met int }
```

- [ ] **Step 1: Let `newRigIn` plan with extra fixture directories.** In `gophermind-lib/briefv2/executor/rig_test.go` line 145 change `rigOpts{BriefEdit: extra.BriefEdit, Settings: ...` to `rigOpts{BriefEdit: extra.BriefEdit, PlannerDirs: extra.PlannerDirs, Settings: ...`.

- [ ] **Step 2: Write the fixture generator** `gophermind-lib/briefv2/executor/sixpack_test.go`:

```go
package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/runner"
)

// sixFn is one of the four functions the six-leaf fixture adds to the
// greeter's greet package (ruling 18).
type sixFn struct{ id, name, word string }

var sixNew = []sixFn{
	{"fn-cheer", "Cheer", "Hooray"},
	{"fn-shout", "Shout", "Hey"},
	{"fn-thank", "Thank", "Thank you"},
	{"fn-welcome", "Welcome", "Welcome"},
}

// sixIDs is the six independent leaves of the greet level, in id order.
var sixIDs = []string{"fn-cheer", "fn-farewell", "fn-greet", "fn-shout", "fn-thank", "fn-welcome"}

const sixImpl = `package greet

import "strings"

// %[1]s trims the name and returns %[2]s, <name>!. An empty name is an error.
func %[1]s(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &NameError{Reason: "name is empty"}
	}
	return "%[2]s, " + name + "!", nil
}
`

const sixTest = `package greet

import "testing"

func Test%[1]s(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"builds the message", "Ada", "%[2]s, Ada!", false},
		{"trims the name", "  Ada  ", "%[2]s, Ada!", false},
		{"refuses an empty name", "   ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := %[1]s(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("%[1]s(%%q) error = %%v, wantErr %%v", c.in, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("%[1]s(%%q) = %%q, want %%q", c.in, got, c.want)
			}
		})
	}
}
`

// sixGood is the passing reply of a leaf of the six-leaf plan.
func sixGood(id string) string {
	for _, f := range sixNew {
		if f.id == id {
			return fmt.Sprintf(sixImpl, f.name, f.word)
		}
	}
	return good(id)
}

func readFixtureJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
}

func writeFixtureJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func cloneJSON(t *testing.T, n map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(n)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// sixpackPlanner writes the planner fixture of the six-leaf level into a new
// directory searched before testdata/greeter/planner: the greet component's
// decomposition, contract and enrichment with four functions cloned from
// fn-greet, their Test-writer replies, and a coverage map that names them.
func sixpackPlanner(t *testing.T) string {
	t.Helper()
	src, dir := filepath.Join(greeterDir, "planner"), t.TempDir()
	find := func(list []map[string]any, id string) map[string]any {
		for _, n := range list {
			if n["id"] == id {
				return n
			}
		}
		t.Fatalf("the greeter fixture has no %s", id)
		return nil
	}

	var dec []map[string]any
	readFixtureJSON(t, filepath.Join(src, "decompose.greet.txt"), &dec)
	baseDec := find(dec, "fn-greet")
	for _, f := range sixNew {
		n := cloneJSON(t, baseDec)
		n["id"] = f.id
		n["title"] = f.name + " a name"
		n["description"] = "Build the " + strings.ToLower(f.name) + " message for a name, refusing an empty one."
		outs := n["contract"].(map[string]any)["outputs"].([]any)
		outs[0].(map[string]any)["description"] = f.word + ", <name>! with the name trimmed"
		dec = append(dec, n)
	}
	writeFixtureJSON(t, filepath.Join(dir, "decompose.greet.txt"), dec)

	var con map[string]any
	readFixtureJSON(t, filepath.Join(src, "contract.greet.txt"), &con)
	fns := con["functions"].([]any)
	for _, f := range sixNew {
		fns = append(fns, map[string]any{
			"id": f.id, "package": "greet", "file": "internal/greet/" + strings.ToLower(f.name) + ".go",
			"signature": "func " + f.name + "(name string) (string, error)",
			"doc":       f.name + " trims the name and returns " + f.word + ", <name>!. An empty name is an error.",
			"uses":      []any{"name-error"},
		})
	}
	con["functions"] = fns
	writeFixtureJSON(t, filepath.Join(dir, "contract.greet.txt"), con)

	var enr []map[string]any
	readFixtureJSON(t, filepath.Join(src, "enrich.greet.txt"), &enr)
	baseEnr := find(enr, "fn-greet")
	for _, f := range sixNew {
		n := cloneJSON(t, baseEnr)
		n["id"] = f.id
		n["rationale"] = "Serves the " + f.id + " part of the brief so callers get the behaviour the contract promises."
		enr = append(enr, n)
	}
	writeFixtureJSON(t, filepath.Join(dir, "enrich.greet.txt"), enr)

	var cov map[string]any
	readFixtureJSON(t, filepath.Join(src, "coverage.txt"), &cov)
	for _, m := range cov["map"].([]any) {
		e := m.(map[string]any)
		if e["requirement"] == "F1" || e["requirement"] == "C2" {
			nodes := e["nodes"].([]any)
			for _, f := range sixNew {
				nodes = append(nodes, f.id)
			}
			e["nodes"] = nodes
		}
	}
	writeFixtureJSON(t, filepath.Join(dir, "coverage.txt"), cov)

	for _, f := range sixNew {
		writeFixtureJSON(t, filepath.Join(dir, "testwrite."+f.id+".txt"), map[string]any{
			"tests": []any{
				map[string]any{"name": "builds the message", "polarity": "success", "covers": "happy", "given": `"Ada"`, "expect": f.word + ", Ada! and a nil error"},
				map[string]any{"name": "trims the name", "polarity": "boundary", "covers": "input:name", "given": `"  Ada  "`, "expect": f.word + ", Ada! and a nil error"},
				map[string]any{"name": "refuses an empty name", "polarity": "negative", "covers": "error:1", "given": `"   "`, "expect": "an empty string and a *NameError"},
			},
			"test_file": fmt.Sprintf(sixTest, f.name, f.word),
		})
	}
	return dir
}

// sixpack is the rig of the six-leaf level with executor.workers set.
func sixpack(t *testing.T, workers int) func(*rigOpts) {
	dir := sixpackPlanner(t)
	return func(o *rigOpts) {
		o.PlannerDirs = append([]string{dir}, o.PlannerDirs...)
		workersN(workers)(o)
	}
}

// sixScript has one passing reply per leaf of the plan.
func sixScript(g *rig) Script {
	s := Script{}
	for _, l := range g.plan.Leaves {
		s["implement:"+l.ID] = []step{reply(sixGood(l.ID))}
	}
	return s
}

// sixChecker passes every check: three verdicts per leaf cover a private pass,
// a landing and a spare; the repository-wide checks pass.
func (g *rig) sixChecker() *fakeChecker {
	fc := g.fastChecker()
	for _, l := range g.plan.Leaves {
		fc.LeafScript[l.ID] = []runner.Verdict{passVerdict(), passVerdict(), passVerdict()}
	}
	return fc
}

func TestSixpackPlansSixIndependentLeavesInOneLevel(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 3))
	wave := g.plan.Leaf("fn-greet").Wave
	if len(levels(g.leaves(sixIDs...))) != 1 {
		t.Fatal("the six leaves are not one level")
	}
	for _, id := range sixIDs {
		if g.plan.Leaf(id) == nil || g.plan.Leaf(id).Wave != wave || g.plan.Leaf(id).Package != "greet" {
			t.Fatalf("%s is not a greet leaf of wave %d: %+v", id, wave, g.plan.Leaf(id))
		}
	}
}
```

- [ ] **Step 3: Run the fixture test.**

```bash
gofmt -w gophermind-lib/briefv2/executor && gt ./gophermind-lib/briefv2/executor/ -run '^TestSixpackPlansSixIndependentLeavesInOneLevel$' -count=1 -timeout 45m
```
Expected: `ok`. Failure guide: `planner.Run = ... want done` names the stage whose fixture the planner refused; compare the generated file in the test's temp dir with the matching greeter fixture (the clone must keep every field the greeter's `fn-greet` entry has).

- [ ] **Step 4: Write the acceptance tests** `gophermind-lib/briefv2/executor/parallel_accept_test.go`:

```go
package executor

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/runner"
)

// recChecker records the test function and repository of every leaf check and
// makes the first two private checks meet at a barrier (ruling 19).
type recChecker struct {
	Checker
	main  string
	meet  *barrier
	mu    sync.Mutex
	calls []runner.LeafCheck
	met   int
}

func (r *recChecker) CheckLeaf(ctx context.Context, c runner.LeafCheck) runner.Verdict {
	r.mu.Lock()
	r.calls = append(r.calls, runner.LeafCheck{TestFunc: c.TestFunc, Repo: c.Repo})
	r.mu.Unlock()
	if r.meet != nil && c.Repo != r.main && r.meet.arrive() {
		r.mu.Lock()
		r.met++
		r.mu.Unlock()
	}
	return r.Checker.CheckLeaf(ctx, c)
}

// GOAL-UNIFICATION task 6 acceptance (spec 15.1): six independent leaves with
// workers 3, the real runner, all verified, fixed commit order, no file outside
// a leaf's paths, private checks in distinct trees that overlapped.
func TestParallelSixIndependentLeaves(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 3))
	g.wire(sixScript(g))
	rec := &recChecker{main: g.repo, meet: newBarrier(2)}
	rep, err := run(context.Background(), g.options(), runFlags{afterStart: func(rc *runCtx) { rec.Checker = rc.chk; rc.chk = rec }})
	if err != nil || rep.Status != "verified" {
		t.Fatalf("run = %s (%s) %v: %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	var six []string
	for _, id := range subjectIDs(g.leafSubjects()) {
		if inList(sixIDs, id) {
			six = append(six, id)
		}
	}
	if !reflect.DeepEqual(six, sixIDs) {
		t.Fatalf("leaf commits of the level in order %v, want %v", six, sixIDs)
	}
	for _, id := range sixIDs {
		l := g.plan.Leaf(id)
		hash := strings.TrimSpace(g.gitCmd("log", "--format=%H", "--grep=^gm("+id+"): "))
		files := strings.Fields(g.gitCmd("show", "--name-only", "--format=", hash))
		want := []string{l.File, l.StubFile}
		sort.Strings(files)
		sort.Strings(want)
		if !reflect.DeepEqual(files, want) {
			t.Errorf("%s commit changes %v, want %v", id, files, want)
		}
	}
	wave0 := strings.TrimSpace(g.gitCmd("log", "--format=%H", "--grep=^gm(wave0)"))
	allowed := map[string]bool{}
	for _, l := range g.plan.Leaves {
		allowed[l.File], allowed[l.StubFile] = true, true
	}
	for _, f := range strings.Fields(g.gitCmd("diff", "--name-only", wave0+"..HEAD")) {
		if !allowed[f] {
			t.Errorf("%s changed outside every leaf's paths", f)
		}
	}
	if st := strings.TrimSpace(g.gitCmd("status", "--porcelain")); st != "" {
		t.Fatalf("the tree is not clean:\n%s", st)
	}
	byFunc := map[string]string{}
	for _, l := range g.plan.Leaves {
		byFunc[l.TestFunc] = l.ID
	}
	scratch := filepath.Join(g.repo, ".gophermind", g.id+"-scratch")
	trees, landed := map[string]string{}, 0
	for _, c := range rec.calls {
		id := byFunc[c.TestFunc]
		if !inList(sixIDs, id) {
			continue
		}
		if c.Repo == g.repo {
			landed++
			continue
		}
		if want := filepath.Join(scratch, "trees", id); c.Repo != want {
			t.Errorf("%s was checked in %s, want its own tree", id, c.Repo)
		}
		trees[id] = c.Repo
	}
	if len(trees) != 6 || landed != 6 || rec.met != 2 {
		t.Fatalf("private trees %d, landings %d, met %d: want 6, 6, 2", len(trees), landed, rec.met)
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) == 0 || !strings.Contains(ev[0].Message, "6 leaves, 3 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	var ver []string
	for _, e := range g.sink.OfKind("leaf_verified") {
		if inList(sixIDs, e.NodeID) {
			ver = append(ver, e.NodeID)
		}
	}
	if !reflect.DeepEqual(ver, sixIDs) {
		t.Fatalf("leaf_verified order = %v", ver)
	}
	if rep.Parallel == nil || rep.Parallel.Workers != 3 || rep.Parallel.ParallelLevels < 1 {
		t.Fatalf("report parallel = %+v", rep.Parallel)
	}
	for _, d := range []string{"trees", "w", "base"} {
		if entries, _ := os.ReadDir(filepath.Join(scratch, d)); len(entries) != 0 {
			t.Errorf("%s holds %d entries after the run", d, len(entries))
		}
	}
	if m, _ := filepath.Glob(filepath.Join(g.runDir, "_state", candidatePrefix+"*")); len(m) != 0 {
		t.Errorf("candidates left: %v", m)
	}
}

type levelRun struct{ subjects, nodes, verified []string }

// sixLevelRun runs the six-leaf level once with the fake checker, each check
// delayed by a random 0 to 30 ms (seed 0: no delay).
func sixLevelRun(t *testing.T, workers int, seed int64) levelRun {
	t.Helper()
	g := newRig(t, sixpack(t, workers))
	rc, _ := g.schedRC(t, sixScript(g))
	fc := g.sixChecker()
	rc.chk = fc
	if seed != 0 {
		rng := rand.New(rand.NewSource(seed))
		var mu sync.Mutex
		fc.Hook = func(string) {
			mu.Lock()
			d := time.Duration(rng.Intn(30)) * time.Millisecond
			mu.Unlock()
			time.Sleep(d)
		}
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	if err != nil || wr.Stop != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
		t.Fatalf("workers %d seed %d: %+v, %v", workers, seed, wr, err)
	}
	var out levelRun
	out.subjects = g.leafSubjects()
	out.nodes = strings.Fields(g.gitCmd("log", "--reverse", "--format=%(trailers:key=GopherMind-Node,valueonly)", "--grep=^gm(fn-"))
	for _, e := range g.sink.OfKind("leaf_verified") {
		out.verified = append(out.verified, e.NodeID)
	}
	return out
}

// Spec 15.5: twenty runs with random delays and 2, 3 or 6 workers give the
// serial run's commit subjects, node trailers and leaf_verified order.
func TestParallelDeterministicCommitOrder(t *testing.T) {
	t.Parallel()
	serial := sixLevelRun(t, 1, 0)
	if !reflect.DeepEqual(subjectIDs(serial.subjects), sixIDs) {
		t.Fatalf("serial order = %v", serial.subjects)
	}
	for i := 0; i < 20; i++ {
		w := []int{2, 3, 6}[i%3]
		got := sixLevelRun(t, w, int64(i+1))
		if !reflect.DeepEqual(got.subjects, serial.subjects) || !reflect.DeepEqual(got.nodes, serial.nodes) || !reflect.DeepEqual(got.verified, sixIDs) {
			t.Fatalf("run %d (workers %d): subjects %v nodes %v verified %v; serial %v %v", i, w, got.subjects, got.nodes, got.verified, serial.subjects, serial.nodes)
		}
	}
}

// Review Focus 4: with more leaves than workers, never more than `workers`
// trees on disk or checks in flight, and nothing left after.
func TestMoreLeavesThanWorkersNeverHoldMoreTrees(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 2))
	rc, _ := g.schedRC(t, sixScript(g))
	fc := g.sixChecker()
	rc.chk = fc
	treesDir := filepath.Join(rc.scratch, "trees")
	var mu sync.Mutex
	maxTrees, cur, maxChecks := 0, 0, 0
	fc.Hook = func(string) {
		entries, _ := os.ReadDir(treesDir)
		mu.Lock()
		if len(entries) > maxTrees {
			maxTrees = len(entries)
		}
		cur++
		if cur > maxChecks {
			maxChecks = cur
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	if err != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if maxTrees < 1 || maxTrees > 2 || maxChecks > 2 {
		t.Fatalf("trees at once %d, checks at once %d: want at most 2", maxTrees, maxChecks)
	}
	if ev := g.sink.OfKind("level_parallel"); len(ev) != 1 || !strings.HasSuffix(ev[0].Message, "6 leaves, 2 workers") {
		t.Fatalf("level_parallel = %+v", ev)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Ruling 5: a leaf with a real file on disk is adopted in the main tree in its
// turn while the others run in private trees; no model call for it.
func TestAnAdoptedLeafOfAParallelLevelLandsInItsTurn(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 2))
	rc, _ := g.schedRC(t, sixScript(g))
	g.plantReal(t, "fn-cheer", sixGood("fn-cheer"))
	rec := &recChecker{Checker: g.sixChecker(), main: g.repo}
	rc.chk = rec
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	if err != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if n := len(g.leafCalls("fn-cheer")); n != 0 {
		t.Fatalf("the adopted leaf made %d model calls", n)
	}
	cheer := 0
	for _, c := range rec.calls {
		if c.TestFunc == "TestCheer" {
			cheer++
			if c.Repo != g.repo {
				t.Fatalf("the adopted leaf was checked in %s", c.Repo)
			}
		}
	}
	if cheer != 1 || !reflect.DeepEqual(subjectIDs(g.leafSubjects()), sixIDs) {
		t.Fatalf("cheer checks %d, order %v", cheer, g.leafSubjects())
	}
}
```

- [ ] **Step 5: Run, with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/executor/ -run 'TestSixpack|TestParallelSixIndependentLeaves|TestMoreLeavesThanWorkers|TestAnAdoptedLeafOfAParallel' -race -count=3 -timeout 45m -parallel 2
gt ./gophermind-lib/briefv2/executor/ -run 'TestParallelDeterministicCommitOrder|TestRigInReopens' -race -count=1 -timeout 45m -parallel 2
```
Expected: `ok`. Failure guide: `met 1` in the acceptance test means the private checks did not overlap (the level ran serially: look for a `warning` event saying why); a commit order other than `sixIDs` means a landing did not wait for its turn.

- [ ] **Step 6: Commit.**

```bash
git add gophermind-lib/briefv2/executor/rig_test.go gophermind-lib/briefv2/executor/sixpack_test.go gophermind-lib/briefv2/executor/parallel_accept_test.go
git commit -m "test(executor): six independent leaves verify in parallel in id order, deterministically (sub-project F task 10)" -m "Ruling 18: the six-leaf fixture is generated from the greeter (one package, six leaves; wave 2 covers a second package). Ruling 19: the acceptance test runs the real runner with a recording checker and a barrier." -m "$TRAILERS"
```

## Task 11: Failure isolation, landing conflict, kill and resume, diff_only

**Files:**
- Create: `B2/executor/parallel_kill_test.go`

**Interfaces:**
- Consumes: `sixpack`, `sixScript`, `sixChecker`, `sixGood`, `sixIDs` (Task 10); `hookSink`, `workersN`, `twice`, `treesLeft`, `leafSubjects`, `subjectIDs` (Task 7); `candidatePath` (Task 6); `blockHere` (`resume_test.go:599`); `oneShot` (`revise_test.go:34`).
- Produces: `func diffOnly() func(*rigOpts)`.

- [ ] **Step 1: Write the tests** `gophermind-lib/briefv2/executor/parallel_kill_test.go`:

```go
package executor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/human"
	"gophermind/gophermind-lib/briefv2/runner"
)

// Spec 15.2: one leaf fails its ladder and writes a stray file in its tree;
// the others land in id order, untouched.
func TestParallelFailingLeafDoesNotTouchOthers(t *testing.T) {
	t.Parallel()
	g := newRig(t, oneShot(0), sixpack(t, 3))
	script := sixScript(g)
	script["implement:fn-greet"] = distinct("fn-greet", 2)
	rc, _ := g.schedRC(t, script)
	fc := g.sixChecker()
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), failVerdict("test_fail", "x", "TestGreet")}
	rc.chk = fc
	g.gate.queue = []human.Resolution{{Action: human.ActionSkip}}
	stray := filepath.Join("internal", "greet", "stray_note.txt")
	var mu sync.Mutex
	var leaks []string
	wrote := false
	fc.Hook = func(repo string) {
		if repo == g.repo {
			return
		}
		if filepath.Base(repo) == "fn-greet" {
			mu.Lock()
			first := !wrote
			wrote = true
			mu.Unlock()
			if first {
				_ = os.WriteFile(filepath.Join(repo, stray), []byte("x"), 0o644)
			}
			return
		}
		if fileExists(filepath.Join(repo, stray)) {
			mu.Lock()
			leaks = append(leaks, filepath.Base(repo))
			mu.Unlock()
		}
	}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
	others := []string{"fn-cheer", "fn-farewell", "fn-shout", "fn-thank", "fn-welcome"}
	if err != nil || !reflect.DeepEqual(wr.Verified, others) || !reflect.DeepEqual(wr.Failed, []string{"fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	if len(leaks) != 0 || fileExists(filepath.Join(g.repo, stray)) {
		t.Fatalf("the stray file reached %v or the main tree", leaks)
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, others) {
		t.Fatalf("commit order = %v", got)
	}
	l := rc.plan.Leaf("fn-greet")
	if fileExists(candidatePath(g, "fn-greet")) || !fileExists(g.stubPath(l)) || fileExists(g.realPath(l)) {
		t.Fatal("the failed leaf left a candidate or lost its stub")
	}
	if r := reasonsOf(t, g.board, "fn-greet"); len(r) != 2 || !strings.HasPrefix(r[0], ClassForbiddenWrite) {
		t.Fatalf("reasons = %v", r)
	}
	if left := treesLeft(t, rc); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Spec 15.3: two leaves of one package declare the same helper. The lower id
// lands; the higher is refused at landing by the gate (ruling 9), goes on in
// the main tree and verifies; commit order is still id order.
func TestParallelLandingConflictFallsBackToSerial(t *testing.T) {
	t.Parallel()
	g := newRig(t, workersN(2))
	helper := "\nfunc clean(s string) string { return strings.TrimSpace(s) }\n"
	rc, fc := g.schedRC(t, Script{
		"implement:fn-farewell": {reply(good("fn-farewell") + helper)},
		"implement:fn-greet":    {reply(good("fn-greet") + helper), reply(good("fn-greet"))},
	})
	twice(fc, "fn-farewell")
	fc.LeafScript["fn-greet"] = []runner.Verdict{passVerdict(), passVerdict()}
	wr, err := rc.scheduleLeaves(context.Background(), g.leaves("fn-farewell", "fn-greet"))
	if err != nil || !reflect.DeepEqual(wr.Verified, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("wave = %+v, %v", wr, err)
	}
	ev := g.sink.OfKind("land_conflict")
	if len(ev) != 1 || ev[0].NodeID != "fn-greet" || ev[0].Message != runner.ClassMalformed {
		t.Fatalf("land_conflict = %+v", ev)
	}
	if v := verdictsOf(t, g.board, "fn-greet"); v != "fail,pass" {
		t.Fatalf("fn-greet attempts = %s", v)
	}
	calls := g.leafCalls("fn-greet")
	if len(calls) != 2 || !strings.Contains(userText(calls[1]), noteMalformed) {
		t.Fatal("the attempt after the conflict does not say why")
	}
	if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, []string{"fn-farewell", "fn-greet"}) {
		t.Fatalf("commit order = %v", got)
	}
}

// Spec 15.4, in process: cancel before the first landing (three candidates
// saved) or after the second landing, then resume with two workers.
func TestParallelKillAndResume(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		name, kind  string
		at, landed  int
		wantCandMin int
	}{
		{"before the first landing", "leaf_candidate", 3, 0, 3},
		{"after the second landing", "leaf_landed", 2, 2, 0},
	} {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			g := newRig(t, sixpack(t, 3))
			rc, _ := g.schedRC(t, sixScript(g))
			g.fake.Loop = true // a leaf cut off before its candidate asks again on resume
			fc := g.sixChecker()
			rc.chk = fc
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			release := make(chan struct{})
			var once sync.Once
			var mu sync.Mutex
			n, cands := 0, 0
			if v.kind == "leaf_candidate" {
				// The lowest id waits in its check until two others saved a candidate, so none lands first.
				fc.Hook = func(repo string) {
					if filepath.Base(repo) == "fn-cheer" {
						select {
						case <-release:
						case <-time.After(30 * time.Second):
						}
					}
				}
			}
			rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
				mu.Lock()
				defer mu.Unlock()
				if e.Kind == "leaf_candidate" {
					if cands++; cands == 2 {
						once.Do(func() { close(release) })
					}
				}
				if e.Kind == v.kind {
					if n++; n == v.at {
						cancel()
					}
				}
			}}
			wr, err := rc.scheduleLeaves(ctx, g.leaves(sixIDs...))
			if err != nil || wr.Stop == nil || wr.Stop.Status != "interrupted" {
				t.Fatalf("first run = %+v, %v", wr, err)
			}
			committed := 0
			for _, id := range sixIDs {
				committed += g.leafCommits(id)
			}
			had := map[string]bool{}
			for _, id := range sixIDs {
				if fileExists(candidatePath(g, id)) {
					had[id] = true
				}
			}
			if committed != v.landed || len(had) < v.wantCandMin {
				t.Fatalf("after the stop: %d commits, candidates %v", committed, had)
			}
			if left := treesLeft(t, rc); len(left) != 0 {
				t.Fatalf("left behind: %v", left)
			}
			rc.close()

			g.cfg.Executor.Workers = 2
			before := map[string]int{}
			for _, id := range sixIDs {
				before[id] = len(g.leafCalls(id))
			}
			rc2, err := startRun(context.Background(), g.options())
			if rc2 != nil {
				defer rc2.close()
			}
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			rc2.chk = g.sixChecker()
			wr2, err := rc2.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
			if err != nil || wr2.Stop != nil || !reflect.DeepEqual(wr2.Verified, sixIDs) {
				t.Fatalf("resumed run = %+v, %v", wr2, err)
			}
			for id := range had {
				if n := len(g.leafCalls(id)) - before[id]; n != 0 {
					t.Errorf("%s had a candidate and made %d model calls on resume", id, n)
				}
			}
			for _, id := range sixIDs {
				if c := g.leafCommits(id); c != 1 {
					t.Errorf("%s committed %d times", id, c)
				}
			}
			if got := subjectIDs(g.leafSubjects()); !reflect.DeepEqual(got, sixIDs) {
				t.Fatalf("commit order = %v", got)
			}
		})
	}
}

// TestParallelChildProcess is the process TestParallelSIGKILLAndResume kills:
// it blocks at the third saved candidate.
func TestParallelChildProcess(t *testing.T) {
	if os.Getenv("GM_PAR_CHILD") != "1" {
		return
	}
	dir := os.Getenv("GM_PAR_DIR")
	g := newRigIn(t, dir, Script{}, sixpack(t, 3))
	g.wire(sixScript(g))
	fc := g.sixChecker()
	var mu sync.Mutex
	n := 0
	hook := func(rc *runCtx) {
		rc.chk = fc
		rc.o.Sink = hookSink{next: rc.o.Sink, on: func(e events.Event) {
			if e.Kind != "leaf_candidate" {
				return
			}
			mu.Lock()
			n++
			k := n
			mu.Unlock()
			if k == 3 {
				blockHere(dir)
			}
		}}
	}
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: hook})
	t.Fatalf("the child ran to its end (%s) without reaching the kill point: %v", rep.Status, err)
}

// Spec 15.4, SIGKILL: a process killed mid-level leaves stale claims and
// candidates; the next run (workers 2) lands the candidates with no model call.
func TestParallelSIGKILLAndResume(t *testing.T) {
	t.Parallel()
	if goCacheOverride == "" {
		t.Skip("no shared go cache")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newRigIn(t, dir, Script{}, sixpack(t, 3)).close() // plans once, in the parent
	cmd := exec.Command(os.Args[0], "-test.run=^TestParallelChildProcess$")
	cmd.Env = append(os.Environ(), "GM_PAR_CHILD=1", "GM_PAR_DIR="+dir, "GM_TEST_GOCACHE="+goCacheOverride)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	marker := filepath.Join(dir, "in-flight")
	deadline := time.After(8 * time.Minute)
wait:
	for {
		select {
		case <-exited:
			t.Fatalf("the child exited before the kill point:\n%s", out.String())
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatalf("the child never reached the kill point:\n%s", out.String())
		case <-time.After(50 * time.Millisecond):
			if fileExists(marker) {
				break wait
			}
		}
	}
	_ = cmd.Process.Kill()
	<-exited
	runDir := filepath.Join(dir, "repo", ".gophermind", greeterID)
	had := map[string]bool{}
	for _, id := range sixIDs {
		if fileExists(filepath.Join(runDir, "_state", candidatePrefix+id)) {
			had[id] = true
		}
	}
	if len(had) == 0 {
		t.Fatalf("the kill left no candidate:\n%s", out.String())
	}
	time.Sleep(1500 * time.Millisecond) // stale_claim_seconds is 1 in newRigIn
	g := newRigIn(t, dir, Script{}, sixpack(t, 2))
	g.wire(sixScript(g))
	rep, err := run(context.Background(), g.options(), runFlags{skipAcceptance: true, afterStart: useChecker(g.sixChecker())})
	if err != nil || rep.Status != "verified" {
		t.Fatalf("resumed run = %s (%s) %v: %v", rep.Status, rep.StopReason, rep.Failures, err)
	}
	for id := range had {
		if n := len(g.leafCalls(id)); n != 0 {
			t.Errorf("%s had a candidate and made %d model calls", id, n)
		}
	}
	var six []string
	for _, id := range subjectIDs(g.leafSubjects()) {
		if inList(sixIDs, id) {
			six = append(six, id)
		}
	}
	if !reflect.DeepEqual(six, sixIDs) {
		t.Fatalf("commit order = %v", six)
	}
	for _, id := range sixIDs {
		if c := g.leafCommits(id); c != 1 {
			t.Errorf("%s committed %d times", id, c)
		}
	}
}

// diffOnly makes the brief land as diff_only.
func diffOnly() func(*rigOpts) {
	return func(o *rigOpts) {
		prev := o.BriefEdit
		o.BriefEdit = func(s string) string {
			if prev != nil {
				s = prev(s)
			}
			return strings.Replace(s, "landing: commit", "landing: diff_only", 1)
		}
	}
}

// Spec 15.13: diff_only with three workers leaves the same working tree diff
// as one worker, and commits no leaf.
func TestParallelDiffOnlyMatchesSerial(t *testing.T) {
	t.Parallel()
	diff := func(workers int) []byte {
		g := newRig(t, diffOnly(), sixpack(t, workers))
		rc, _ := g.schedRC(t, sixScript(g))
		rc.chk = g.sixChecker()
		wr, err := rc.scheduleLeaves(context.Background(), g.leaves(sixIDs...))
		if err != nil || !reflect.DeepEqual(wr.Verified, sixIDs) {
			t.Fatalf("workers %d: %+v, %v", workers, wr, err)
		}
		for _, id := range sixIDs {
			if g.leafCommits(id) != 0 || !fileExists(g.realPath(g.plan.Leaf(id))) {
				t.Fatalf("workers %d: %s was committed or is not on disk", workers, id)
			}
		}
		d, err := rc.git.Diff("main")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if s, p := diff(1), diff(3); !bytes.Equal(s, p) {
		t.Fatalf("diff_only diffs differ:\nserial %d bytes, parallel %d bytes", len(s), len(p))
	}
}
```

- [ ] **Step 2: Run, with the race detector.**

```bash
gofmt -w gophermind-lib/briefv2/executor && go vet ./gophermind-lib/briefv2/executor/
gt ./gophermind-lib/briefv2/executor/ -run 'TestParallelFailingLeaf|TestParallelLandingConflict|TestParallelKillAndResume|TestParallelDiffOnly' -race -count=3 -timeout 45m -parallel 2
gt ./gophermind-lib/briefv2/executor/ -run 'TestParallelSIGKILLAndResume' -count=1 -timeout 45m
```
Expected: `ok`. Failure guide: in the conflict test a class `forbidden_write` instead of `malformed` means the packer reports the name collision through `Forbidden`; read `packer.ParseReply` (`B2/packer/reply.go:139`) and assert the class the gate actually gives (both are gate classes, ruling 9 holds). A resume that asks the model for a leaf with a candidate means `needsMain` did not keep it in the main tree.

- [ ] **Step 3: Commit.**

```bash
git add gophermind-lib/briefv2/executor/parallel_kill_test.go
git commit -m "test(executor): failure isolation, landing conflict, kill and resume, diff_only under parallel levels (sub-project F task 11)" -m "Ruling 9: the conflict of two leaves declaring one helper is caught by the landing gate (malformed)." -m "$TRAILERS"
```

## Task 12: Docs and the final matrix

**Files:**
- Modify (worktree): `docs/briefv2/README.md:194,326,363,424`, `docs/superpowers/specs/2026-09-30-v2-executor-design.md:228,532`
- Modify (main checkout, committed by the controller): `docs/unification-overview.md:41`, `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md:54`, `docs/superpowers/specs/2026-10-08-parallel-waves-design.md:6`

- [ ] **Step 1: README.** In `docs/briefv2/README.md`:

(a) line 194: replace ``--workers` must be 1: leaves share one working tree.` with

```text
`--workers n` (1 to 8) sets `executor.workers` for this run; without it the setting is used (see "Parallel leaves").
```

(b) line 326: replace `  workers: 1                    # must be 1` with

```text
  workers: 1                    # 1 to 8: leaves of one level run at once in private copies
```

(c) line 424: replace `- Leaves are built one at a time (\`workers\` is 1).` with

```text
- Repairs, the revise rung, acceptance and wave checks run one at a time whatever `workers` says; only the first pass over a level's leaves is parallel.
```

(d) insert before `## Revising a leaf` (line 363):

```markdown
## Parallel leaves

`executor.workers` (1 to 8, default 1) is how many leaves of one level run at once. A level
is the leaves of a wave that do not depend on each other. With 1 nothing changes: the
leaves run one after the other in the repository. Above 1:

- Before the level starts, the repository's files (tracked, and untracked but not ignored;
  never `.git`, `.gophermind/` or ignored files) are copied once to
  `.gophermind/<id>-scratch/base`. Each leaf gets its own copy of that under `trees/<leaf>`,
  with a throwaway git repository, its own HOME and TMPDIR under `w/<leaf>` and its own
  sandbox profile. Its checks, its stray-file check and its stub swap all run in its copy.
- A leaf that passes in its copy saves the reply to `_state/candidate-<leaf>` and waits for
  its turn. Turns go in leaf id order: the leaf replays its reply in the repository with
  the full check and commits it there exactly as a serial run does. The work branch stays
  linear and its commits are in the order `workers: 1` gives. `leaf_verified` comes at
  landing.
- A reply that passed alone but fails next to the leaves that landed before it (two leaves
  of one package that both declare a helper `clean`) is a landing conflict
  (`land_conflict`). It counts as a failed attempt, and the leaf goes on with its ladder in
  the repository.
- A level whose copy would be over 256 MiB, or whose files cannot be copied (a symbolic
  link that points outside the repository, a submodule), runs one leaf at a time with a
  `warning`.
- A killed run resumes with any `workers` value. Saved candidates land with no model call,
  and copies left behind are deleted when the next run starts.

`workers` does not change how many model calls run at once: that is each provider's
`max_concurrent`. On the mini (`max_concurrent: 1`) extra workers only overlap the checks of
one leaf with the model call of another, and the run says so in a warning. Each worker runs
`go build`, `go vet` and `go test`: check `memory_pressure` and `sysctl vm.swapusage` before
raising it on the mini. The report's `parallel` object (workers, parallel levels, landing
conflicts) and a `Parallel:` summary line appear only when a level ran in parallel.
```

- [ ] **Step 2: Executor spec.** In `docs/superpowers/specs/2026-09-30-v2-executor-design.md` line 228:

(a) replace `run leaves in id order, one at a time (\`executor.workers\` must be 1, see the ruling below)` with `run leaves in id order (one at a time unless \`executor.workers\` is above 1, see the ruling below)`;

(b) replace the text from `**Ruling (workers):**` up to and including `Lifting it needs one worktree per leaf.` with

```text
**Ruling (workers):** with `executor.workers` at 1 (the default) leaf model loops are serialised under one lock around each leaf body. Above 1 (at most 8), the leaves of one level run at once, each in a private copy of the repository with its own throwaway git repository, HOME and sandbox profile, and land one at a time in id order by replaying the saved reply with the full leaf check in the repository (sub-project F, `docs/superpowers/specs/2026-10-08-parallel-waves-design.md` in the main checkout). The work branch stays linear and its commit order equals the serial run's. Repairs, revisions, acceptance and wave checks stay serial. Model concurrency is the providers' `max_concurrent`, not `workers`.
```

(c) line 532: replace ``CheckWorkers` is the CLI's `--workers` refusal.` with ``CheckWorkers` is the CLI's `--workers` range check (1 to 8; the flag's 0 means the setting).`

- [ ] **Step 3: Main checkout docs (the controller commits these with this plan).**

(d) `docs/unification-overview.md` line 41: replace `| F | Parallel waves in the executor | none yet | not specified |` with

```text
| F | Parallel waves in the executor | F spec; plan F (12 tasks) | built on the branch |
```

(e) `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md` line 54: in the F row replace the last cell `its own spec; the executor runs one worker today` with

```text
`docs/superpowers/specs/2026-10-08-parallel-waves-design.md`; built on the branch (plan `docs/superpowers/plans/2026-10-13-parallel-waves.md`, 12 tasks)
```

(f) `docs/superpowers/specs/2026-10-08-parallel-waves-design.md` line 6: replace `Sequencing: written, not started.` with `Sequencing: built on the branch after A (GOAL-UNIFICATION task 6), by \`docs/superpowers/plans/2026-10-13-parallel-waves.md\`.`

- [ ] **Step 4: The final matrix.** From the worktree root:

```bash
gofmt -l gophermind-lib cmd                     # prints nothing
go build ./... && go vet ./gophermind-lib/... ./cmd/...
gt ./gophermind-lib/briefv2/executor/ -count=1 -timeout 45m -parallel 2
gt ./gophermind-lib/briefv2/executor/ -count=1 -race -timeout 90m -parallel 2
gt $(go list ./gophermind-lib/briefv2/... | grep -v '/briefv2/executor$') ./cmd/... -count=1 -timeout 45m
gt ./gophermind-lib/briefv2/gitland/ ./gophermind-lib/briefv2/pathsafe/ ./gophermind-lib/briefv2/ledger/ ./gophermind-lib/briefv2/blackboard/ ./gophermind-lib/briefv2/report/ ./gophermind-lib/briefv2/settings/ -race -count=1
gt ./gophermind-osx/... -count=1 -timeout 30m -skip TestKeychain_RealStoreRoundTrip
```
Expected: every package `ok`; the `-race` run of the executor package is the spec's "`go test -race` passes" (ruling 21). Apply the flake rule to `TestAcceptanceInterruptedIsNotAPass` only.

Static gates (each prints nothing unless said otherwise):

```bash
grep -rn 'until leaf-isolated trees exist' gophermind-lib cmd docs/briefv2
grep -rn 'worktree add\|"worktree"' gophermind-lib/briefv2
grep -n 'RemoveAll' gophermind-lib/briefv2/executor/*.go | grep -v _test.go | grep -v 'worktree.go'
grep -n 'rc\.git\.\|rc\.o\.Repo' gophermind-lib/briefv2/executor/leaf.go gophermind-lib/briefv2/executor/land.go   # exactly one line: LeafCommit in adoptOnDisk
git diff de24662 -- . | grep -c "$(printf '\342\200\224')"   # prints 0: no em dash added by this plan
git status --short | head -3
```
The last line shows a clean tree after the commit below. Confirm with `ls "${TMPDIR:-/tmp}" | grep -c '^tmp\.'` before and after that `gt` left no temp homes, and `git log --oneline origin/main..HEAD | head -1` only to read (never push).

- [ ] **Step 5: Commit.**

```bash
git add docs/briefv2/README.md docs/superpowers/specs/2026-09-30-v2-executor-design.md
git commit -m "docs: parallel leaves in the README and the executor spec's workers ruling (sub-project F task 12)" -m "Ruling 21: the whole-package race run uses -timeout 90m." -m "$TRAILERS"
```
The main-checkout edits (d), (e) and (f) are committed by the controller together with this plan. The branch is not merged and not pushed.

## Self-review

**Spec coverage.** Every decision and section maps to a task and a test:

| Spec | Task | Proof |
|---|---|---|
| F1 copy plus throwaway `git init` | 3, 4 | `TestNewScratchMakesABaselineWithTheGuard`, `TestNewScratchInsideARepositoryIsItsOwnRepository`, `TestSnapshotAndMakeTreeCopyTheLevel` |
| F2 the leaves of one level in parallel | 7 | `TestAParallelLevelOverlapsAndLandsInIDOrder`, `TestParallelSixIndependentLeaves` |
| F3 by content, in order, no cherry-pick | 6, 7 | `land` replays through `finishPass`; `TestAPrivatePassLandsInTheMainTree` |
| F4 id order by a turn chain | 5, 7, 10 | `TestTurnsTakeTheMainTreeOneAtATimeInOrder`, `TestParallelDeterministicCommitOrder` |
| F5 the landing check is a full check | 6 | `TestALandingConflictContinuesInTheMainTree` (checks: tree, main, main) |
| F6 verified means landed | 6, 10 | event order `leaf_candidate, leaf_landed, leaf_verified`; `leaf_verified` in id order |
| F7 default 1, range 1 to 8 | 2 | `TestExecutorWorkersRange`, `TestCheckWorkers`, `TestBriefRunWorkersFlagOverridesTheSettingForThisRun` |
| F8 model concurrency unchanged, notice | 8 | `TestWorkersAboveModelConcurrencyIsANotice` |
| F9 repairs, acceptance, wave checks serial | 6, 7 | `runJobs` only parallelises a level's first pass; revise hand-off `TestAPrivateLeafMovesToTheMainTreeBeforeItsRevise` |
| F10 candidate persistence | 5, 6, 11 | `TestCandidateRoundTripAndGuards`, `TestASavedCandidateLandsWithoutAModelCall`, `TestParallelKillAndResume`, `TestParallelSIGKILLAndResume` |
| F11 gate serialised | 8 | `TestTwoEscalationsReachTheGateOneAtATime` |
| F12 256 MiB limit | 4, 7 | `TestSnapshotLevelRefusesALinkOutOfTheTreeAndAnOversizedTree`, `TestATreeTooLargeRunsTheLevelSerially` |
| Section 4 overlap split | 7 | `TestSplitOverlapsPutsASharedPathInALaterSubLevel`, `TestOverlappingLeavesOfALevelLandOneAfterTheOther` |
| Section 5.2 layout and cleanup | 4 | `TestLeftoverTreesOfAKilledRunAreRemovedWithoutFollowingALink`, `treesLeft` assertions in Tasks 7, 10, 11 |
| Section 5.3 checker, sandbox, environment | 4 | `TestAPrivateTreeHasItsOwnHomeAndProfile` |
| Section 6 call sites move to the tree | 5 | `TestLeafChecksGoToTheLeafsWorkTree`, full executor run at Task 7 |
| Section 7.3 conflicts | 6, 11 | `TestALandingConflictContinuesInTheMainTree`, `TestParallelLandingConflictFallsBackToSerial` |
| Section 7.4 linear history | 10 | `TestParallelSixIndependentLeaves` (commit contents, diff since Wave 0, clean tree) |
| Section 8 shared state | 9 | the review in Task 9's commit, `TestFSEightWritersKeepIDsUniqueAndTheSeqExact` |
| Section 9 stray check and swap per tree | 5, 11 | `TestParallelFailingLeafDoesNotTouchOthers` (stray in a tree never reaches others) |
| Section 10 failure isolation, panic, stop, cancel, disk | 6, 7, 11 | `TestPanicInOneLeafLetsTheOtherLand`, `TestCancelDuringAParallelLevel`, `TestAnInterruptedWaitKeepsTheCandidate`, `makeTree` failure is a harness fault |
| Section 11 resume | 6, 8, 11 | `TestASavedCandidateLandsWithoutAModelCall`, `TestResumeNamesEveryLiveLeaf`, kill tests, `cleanTrees` at preflight |
| Section 12 settings and CLI | 2 | as F7 |
| Section 13 events and report | 6, 7, 8 | `leaf_candidate`, `leaf_landed`, `land_conflict`, `level_parallel`, `tree_ready`, `warning`; `TestParallelSectionIsAdditive`, `TestTheReportAndTheStateCountParallelLevels` |
| Section 14 race checklist | 9 | items 1 to 10 recorded in the commit body; `-race` in every concurrency task |
| Section 15 tests 1 to 13 | 10, 11, 7, 8, 2, 9, 3, 4 | 1 `TestParallelSixIndependentLeaves`; 2 `TestParallelFailingLeafDoesNotTouchOthers`; 3 `TestParallelLandingConflictFallsBackToSerial`; 4 `TestParallelKillAndResume`, `TestParallelSIGKILLAndResume`; 5 `TestParallelDeterministicCommitOrder`; 6 `TestWorkerCountIsTheSettingCappedByPrivateJobs`, `TestASerialLevelNeverOverlaps`, the full suite; 7 `TestCancelDuringAParallelLevel`; 8 `TestPanicInOneLeafLetsTheOtherLand`; 9 `TestTwoEscalationsReachTheGateOneAtATime`; 10 Task 2 tests and the grep gate; 11 `TestFSEightWritersKeepIDsUniqueAndTheSeqExact`; 12 Task 3 and Task 4 tests; 13 `TestParallelDiffOnlyMatchesSerial` |
| Section 17 open item 5 (contention measured) | 9 | the ledger test logs the longest wait and fails at 1 s |
| GOAL-UNIFICATION task 6 acceptance | 10, 12 | `TestParallelSixIndependentLeaves` plus the whole-package `-race` run |
| Re-review fact (a): the RWMutex window | 1 | `TestChainsIntactReadsTheMemoryUnderTheChainLock` (red before the fix), `TestPlanIntactNeverFailsWhileARevisionRunsOnAnotherGoroutine` under `-race` |

**Placeholder scan.** No "TBD", no "similar to Task N", no step without code or a command. The one step whose content depends on what the code shows is Task 9 step 5 (fix what the review found); it names the file, the test file and the command, and changes nothing when the review finds nothing. Line numbers are those of `de24662`; an implementer who finds them shifted by an earlier task of this plan edits the named function.

**Type consistency.** `workTree`, `mainTree`, `snapshotLevel`, `endLevel`, `makeTree`, `dropTree`, `removeScratch`, `cleanTrees`, `treeProfile`, `checkerFor`, `envAt`, `leafCheckIn` are defined in Task 4 and used by Tasks 5 to 11 with the same signatures. `turn`, `newTurn`, `wait`, `release`, `candidate`, `candidatePrefix`, `saveCandidate`, `readCandidate`, `removeCandidate`, `leafIn.Tree`, `leafIn.Turn`, `leafRun.wt`, `.stub`, `.turn`, `.cand`, `.commit`, `lr.checkLeaf` are defined in Task 5. `revCandidate`, `landResult`, `privatePass`, `toMain`, `land`, `landCandidate`, `resumeCandidate`, `noteConflict`, `runReport.conflicts` in Task 6. `job.main`, `runnable`, `workerCount(jobs)`, `splitOverlaps`, `needsMain`, `levelWorkers`, `runJob`, `runParallelJob`, `noteParallel`, `runCtx.afterTree`, `runReport.parallelLevels` and the helpers `workersN`, `twice`, `leafSubjects`, `subjectIDs`, `hookSink`, `barrier`, `newBarrier`, `blockingChecker`, `treesLeft` in Task 7. `report.Parallel`, `Report.Parallel`, `Input.Parallel`, `State.Workers`, `gateMu`, `modelConcurrency`, `parallelReport` in Task 8. `goroutinesSettle` in Task 9. `sixNew`, `sixIDs`, `sixGood`, `sixpackPlanner`, `sixpack`, `sixScript`, `sixChecker`, `recChecker` in Task 10. `diffOnly` in Task 11. `candidatePath` and `privateRun` (Task 6) are reused by Tasks 7 and 11.

**Review Focus.** The five input classes are in the section near the top; each has its test in the owning task (Tasks 3, 4, 6, 7, 10), named there. Classes the spec's own tests already cover (a failing leaf, a conflict, cancel, panic, kill, gate, diff_only) are not repeated.

**Ambiguities resolved** are the 24 numbered rulings near the top; each is repeated in the commit body of the task that applies it.
