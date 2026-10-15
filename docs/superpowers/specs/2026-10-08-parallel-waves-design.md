# Parallel Waves in the Executor: Design (sub-project F)

Status: draft for review. Date: 2026-10-08.
Scope: sub-project F of `2026-10-07-planner-phaseflow-unification-design.md` (the executor runs leaves of a wave in parallel, each in an isolated working tree). Docs only; no code is written by this document.
Builds on: the executor spec on branch `feat/briefv2-planner-core` (`docs/superpowers/specs/2026-09-30-v2-executor-design.md`: section 8.1 the swap, 8.2 the scheduler and its `workers` ruling, 8.3 wave checks, 10 resume, 12 landing, rules R6, R10, R13, R15, R17, R18), A (`2026-10-07-filesystem-run-state.md`: claims, ledger and events as files), and `docs/GOAL-UNIFICATION.md` task 6 (the binding behaviour and the acceptance test).
Sequencing: built on the branch after A (GOAL-UNIFICATION task 6), by `docs/superpowers/plans/2026-10-13-parallel-waves.md`. Dependency order: A first (done before F; F needs the file-based blackboard and ledger), then F. F touches `executor`, `gitland`, `settings` and one CLI flag; it does not touch the planner.
Superseded by the plan F rulings (`docs/superpowers/plans/2026-10-13-parallel-waves.md`, "Rulings taken while grounding the plan in the code"): where this document and those rulings differ, the rulings and the built code win. Known differences: the report `schema_version` stays 3 (E already raised it), a landing conflict restarts the current revision's ladder in the main tree with a fresh failure count (instead of budgets counting), the landing reply gate classes a duplicate helper as `malformed`, the six-leaf fixture uses one package for the six leaves, and `checkerFor` takes the scratch repo explicitly.
Code paths below are relative to `gophermind-lib/briefv2/` in the worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core`. After G7 the root is renamed `build/`; the file names stay.

## 1. Why this exists

The executor already has a worker pool and refuses to use it:

- `executor/schedule.go`: `runJobs` starts jobs through a `sem` channel of size `workers` and a `sync.WaitGroup`, but `workerCount` returns 1 whatever the setting says, and `schedState.body` (a mutex held around the whole leaf body) makes two bodies never overlap even if it did not.
- `settings/executor.go`, `validate`: `if e.Workers != 1 { return fmt.Errorf("executor.workers must be 1 until leaf-isolated trees exist") }`. The same wording is in `executor.CheckWorkers` (`executor/options.go`), which `cmd/gophermind/brief_run.go` calls on `--workers` (flag default 1, help text "must be 1").
- Why: every leaf works in the one repository tree. The stray-file check (`executor/stray.go`: `TakeSnapshot` and `Snapshot.Stray` compare `git status` before and after an attempt) and the stub swap (`executor/swap.go`: delete `zz_gm_stub_<id>.go`, write the real file) both look at that whole tree. Two leaves at once would blame each other's files and swap each other's stubs. The executor spec ruling in 8.2 says lifting this "needs one worktree per leaf".

What serial costs: a wave takes the sum of its leaves. The mini serves one model call at a time, so model time cannot be shared, but the non-model steps (gofmt, build, vet, `go test`, the file swap, and the failed attempts that never reach the model again) can overlap with another leaf's model call. On a faster endpoint (a public provider with `max_concurrent` above 1) the gain is larger. The default stays 1.

## 2. Decisions

| # | Question | Decision | Why |
|---|---|---|---|
| F1 | Isolation mechanism | A private copy of the repository's tracked and untracked-unignored files, with its own throwaway `git init` and one baseline commit. Not `git worktree add`, not `git clone --shared` | Section 5.1 |
| F2 | What runs in parallel | The model loops and checks of the leaves of one level (`levels()` in `schedule.go`; leaves of one wave have no dependencies among themselves, so a wave is normally one level) | Dependent leaves must see their dependencies as real code, so a level starts only after the previous level landed |
| F3 | How results reach the work branch | By content, in order: a leaf that passes in its private tree is replayed once in the main tree, in node-id order, and committed there by the existing `CommitLeaf`. No cherry-pick, no rebase, no merge | History is linear by construction; `gitland` cannot express rebase or reset (R13) and the guard in `gitland/cli.go` forbids them |
| F4 | Landing order | Ascending node id within a level, enforced by a turn chain, whatever order the leaves finish in | Deterministic commits and events |
| F5 | The landing check | The replay is a full leaf check (gofmt, build, vet, named test, stray-file check) in the main tree, so every commit is verified in the exact tree it lands on. A failure there is a landing conflict: the leaf continues its ladder serially in the main tree | Parallel results are never trusted more than serial ones |
| F6 | Verified means landed | A leaf is `verified` on the blackboard only after its commit exists. A private pass is not a status | Keeps the invariant resume relies on (verified implies a commit or an adopted file) |
| F7 | Default and range | `executor.workers` default 1, valid range 1 to 8. 1 runs exactly today's path with no copy | The mini serves one request at a time; the old range 1 to 64 was never reachable |
| F8 | Model concurrency | Unchanged and not controlled by `workers`. The router's per-provider `max_concurrent` slots (`router/attempt.go`, `acquire`) are the only limit on simultaneous model calls | On the mini (`MaxConcurrent: 1`) extra workers queue at the slot; they overlap checks only |
| F9 | Repairs, acceptance, wave checks | Stay serial in the main tree. Only the first pass over a level's leaves is parallel | The repair loop (8.3) edits several already-landed leaves from one failure report; parallelising it adds nothing a first-pass level does not |
| F10 | Candidate persistence | A private pass writes the candidate source to `_state/candidate-<id>` before the leaf waits for its turn; resume replays it with no model call | A kill between a private pass and its landing must not cost model calls |
| F11 | Gate (human) questions | Serialised by one mutex; the `human.Gate` implementations are not required to be concurrency safe | A terminal prompt cannot take two questions at once |
| F12 | Tree size | A copy over 256 MiB makes the level run serially, with a `warning` event | A multi-gigabyte repo times 8 workers must not fill the disk |

## 3. Goals and non-goals

Goals (binding, from GOAL-UNIFICATION task 6):

- `executor.workers` above 1 runs the leaves of a wave in parallel, each in an isolated working tree, so the stray-file check, the stub swap and the test run cannot race.
- Landing order is deterministic and the work branch `gm/<brief-id>` stays a linear history.
- A leaf's failure does not corrupt another leaf's tree or the main tree.
- The default stays 1.
- Acceptance: a fixture of six independent leaves with `workers: 3` all verify, the commits are in a fixed order, no file outside a leaf's own paths changes, and `go test -race` passes.

Non-goals:

- Faster model calls. The mini's single slot is a hard limit (section 8.4).
- Parallel repair rounds, parallel acceptance or parallel waves. Waves stay sequential (section 4).
- Running two executor processes on one run. A second process still hits `claim_held` in `scheduleLeaves`.
- Streaming landing (landing a leaf the moment its predecessors landed, before the level ends). The turn chain already lands in id order as soon as predecessors finish; nothing more is added.
- Changing the planner, the node format or the report schema version (additions are optional fields).

## 4. How a level runs

```text
runWave(w) -> scheduleLeaves(todo) -> for each level (levels()):
   classify (blocked / already terminal / held by another worker)   [serial, as today]
   n = min(executor.workers, number of jobs to run)
   n == 1 or tree copy too big:  today's path, main tree, no copy
   n >= 2:
      for each job in id order (sem of size n):
          claim, heartbeat, make private tree, run the ladder in the tree
          on a private pass: save _state/candidate-<id>, wait for the turn
          on turn: replay in the main tree (the landing check), commit, mark verified
          release the turn, remove the tree
   merge results in id order (as today)
-> waveChecks(w) in the main tree (build, vet, per-package tests), unchanged
```

`runJobs` keeps its shape: it starts jobs in id order through the semaphore, so the lowest ids start first. What changes is that `workerCount` returns `min(rc.cfg.Executor.Workers, jobsToRun)` and the `schedState.body` mutex is taken only when the job runs in the main tree. Claims, `classify`, `merge`, stop handling, `maxLeafInterrupts` and the stale-claim sweep (`staleHeartbeats` times the heartbeat interval) are untouched.

Two leaves of one level whose `File`, `StubFile` or `TestFile` paths overlap (a plan defect the planner should have refused) cannot be isolated from each other. `classify` finds them by path and moves the later one (by id) to a following sub-level, so it runs after the earlier one has landed. This is a rule about files, not about packages: two leaves in one package are normal.

## 5. Isolated trees

### 5.1 Copy, not worktree

Options weighed:

| Option | Verdict |
|---|---|
| `git worktree add` per leaf | Rejected. John's git wrapper blocks `git worktree add` (and `checkout`, `stash`) in some checkouts, and the executor resolves `git` through `exec.LookPath` (`gitland/cli.go`, `NewCLI`), so it may reach the wrapper. A worktree also writes into the main `.git` (`worktrees/`, refs, the shared object store), while the sandbox profile denies all writes under `<Repo>/.git` (`sandbox/sandbox.go`, `Profile`). It would share the main repo's index and `HEAD` bookkeeping, the thing isolation must not share |
| `git clone --shared` | Rejected. Not blocked, but its objects live behind an `alternates` pointer to the main store, so a `gc` or a pack change in the main repo can break a tree; it copies committed state only (wrong in `diff_only`, where nothing is committed); and it gives each tree full history that no leaf check reads |
| Copy the file list into a scratch directory and `git init` it | Chosen |

The copy: the main CLI lists `git ls-files -z --cached --others --exclude-standard` (a new read-only `gitland.Repo` method, `Files()`, over the same fixed-argument runner as `Dirty`); the executor copies each listed regular file with `pathsafe` rules (no symlink following, no path outside the repo, executable bit kept); symlinks are copied as links, never followed. `.git`, `.gophermind/` and every ignored file are not copied. Using the file list rather than `HEAD` is what makes `diff_only` work: the copy holds the working tree as it is, committed or not.

The tree is then made a repository with a new constructor `gitland.NewScratch(dir, runID, files)`: `init`, `add -A`, one commit `gm(baseline)`, using the same private HOME, fixed identity and `fixedPrefix` as `NewCLI`. It returns a `*CLI` with the same guard (no push, reset, rebase, checkout, clean, gc). `Dirty`, `Ignored` and `Restore` therefore work unchanged in the tree, which is all the stray-file check needs. Nothing in a tree is ever pushed or merged; it is deleted.

If a future checkout does allow `git worktree add` safely, nothing here changes: trees are an implementation detail behind one constructor.

### 5.2 Layout and cleanup

```text
<repo>/.gophermind/<id>-scratch/      rc.scratch (start.go), excluded from git already
    trees/<leaf-id>/                  the private tree (profile Repo, runner Dir)
    w/<leaf-id>/                      per-leaf HOME and TMPDIR (profile Scratch)
    gocache/                          shared Go build cache (safe for concurrent builds)
```

The main repo's `Dirty()` does not list `.gophermind`, so trees never dirty it. A tree is removed when its leaf ends (landed, failed, escalated, interrupted). Removal follows the standing rule: the path is built into a variable, checked to be absolute, to equal `rc.scratch + "/trees/" + id` with `id` matching `^[a-z0-9][a-z0-9-]*$`, printed in a debug event, and only then `os.RemoveAll`. At `startRun` (and before every parallel level) the whole `trees/` and `w/` directories are removed the same way: a killed run leaves only derived data, never anything worth keeping, because the candidate is in `_state/` (section 7.4).

### 5.3 Checker, sandbox and environment per tree

- The sandbox profile is baked into the runner (`start.go`: `runner.New(runner.Config{Sandbox: profile, ...})`). A tree needs its own profile with `Repo: <tree>`, `Scratch: w/<id>`, and the same `RunDir`, `GoCache`, `GoModCache`, `ReadOnly` and `Home`. `runCtx` gains `checkerFor(wt *workTree) Checker`: in production it builds a `runner.New` with that profile (cheap, no process); when the sandbox is off or a test installed a fake `Checker`, it returns `rc.chk`. `LeafCheck.Repo` already carries the tree path, which is how a fake can assert isolation. The profile's writes-under-`.gophermind` and secret-file denies apply to the tree path, so a leaf cannot write a sibling's tree (not in its profile) or the main repo.
- `rc.env(nodeID, mode)` (`executor/env.go`) sets `HOME` and `TMPDIR` to `rc.scratch`, `GOPATH` to `scratch/gopath`. It takes the work tree and uses `w/<id>` and `w/<id>/gopath`. `GOCACHE` stays shared (the Go build cache is built for concurrent use); `GOMODCACHE` stays shared and read-only outside the deps step (R7), so no leaf can change what another builds against.
- The proxy is one shared forward proxy (`proxy.Proxy`), already per node: `Proxy.URL(node)` puts the node id in the proxy userinfo, and `Failures(node, since)` and `Warnings(node, since)` filter by node, so one leaf's critical-host failures are never charged to another. No change.

## 6. Leaf attempts in a private tree

`leafRun` (`executor/leaf.go`) reads the repository through `rc.o.Repo`, `rc.git` and `rc.chk`. A new value `workTree{Repo string; Git gitland.Repo; Chk Checker; Scratch string; Private bool}` carries them, and `leafRun` gets a field `wt`. The main tree is `rc.mainTree()`; with `workers` 1 every leaf gets it and behaves as today. The call sites that move to `lr.wt`:

| Where | Today | In a private tree |
|---|---|---|
| `newLeafRun`: `declaredNames`, `NewSwap` | `rc.o.Repo`, `rc.git` | `wt.Repo`, `wt.Git`; the swap also calls `NoCommit` (below) |
| `check`, `adoptOnDisk`: `TakeSnapshot` | main repo and git | the tree and its git |
| `cleanStray` | `snap.Stray(rc.o.Repo, rc.git, l.File, l.StubFile)` and `Revert(rc.git, ...)` | the tree's git; allowed paths still only the leaf's `File` and `StubFile` |
| `testSource`: `readNoFollow(... l.TestFile)` | main | the tree (the Wave 0 test file was copied) |
| `rc.checkLeaf`, `leafCheck`, `rc.env` | main | `wt.Chk`, `wt.Repo`, `wt` scratch |
| `finishPass` | commits through the swap | does not commit in a private tree (below) |

The swap in a private tree runs in its existing `NoCommit` mode (`swap.go`: `NoCommit` and `PassNoCommit`, written for `diff_only`). That mode already commits nothing and keeps the verified content out of git. Its `_state/prior-<id>` files are keyed by leaf id, so they cannot collide.

What a private pass is: `check` reaches its `default:` branch, where today it calls `finishPass`. In a private tree it instead (1) saves the candidate source to `_state/candidate-<id>` through `writeStateBytes` (atomic; same family as `priorPrefix`), (2) emits `leaf_candidate`, (3) removes the real file and puts the stub back in the tree (the tree is about to be discarded anyway), and (4) waits for its turn. It records no pass attempt: the blackboard sees one pass attempt per leaf, at landing.

Failed private attempts are recorded exactly as today (class, reason, hashes, artifacts under `attempts/`), so the ladder, `fix_attempts`, revisions, escalation, the identical-reply guard (R17) and `max_revisions` behave the same. Blackboard attempt numbering and `artifacts.Writer.Save` are per node, so concurrent leaves never write the same attempt folder.

## 7. Landing

### 7.1 The turn chain

A level's runnable jobs, in id order, each get a `done` channel. Job k, once its private pass is saved, waits on `done` of job k-1 (the previous job that ran in this level) or on `ctx.Done()`; when it has landed or ended in any way it closes its own. The lowest id has no predecessor, and ids only wait on lower ids, which started earlier and hold their own slot, so the chain cannot deadlock. A job that fails, escalates, is interrupted or panics closes `done` on the way out (a `defer`), so a failing leaf never blocks the ones after it.

The turn is the only place where the main tree and the main `gitland.CLI` are touched during a parallel level. That is the single-writer rule that keeps the work branch linear: commits are made one at a time, by the goroutine holding the turn.

### 7.2 The landing attempt

On its turn the leaf calls `lr.land(ctx, candidate)`, which moves the leaf to the main tree (`lr.wt = rc.mainTree()`, a new `Swap` over it, `NoCommit` off) and runs the existing `check` path once with the candidate as the reply source: snapshot, `Swap.Enter` (delete the stub, write the real file atomically), gofmt/build/vet/named test, stray-file check, and on pass `finishPass` with `swap.Pass(title)`, which calls `CommitLeaf` exactly as in a serial run. The commit message, trailers (`GopherMind-Node`, `GopherMind-Run`) and `SetResult`/`SetStatus(verified)` are the serial code. The pass attempt is recorded here.

Because earlier ids of the level are already committed in the main tree, the replay sees them as real code, the same as in a serial run. The only difference from serial is that the model call happened without them.

### 7.3 Conflicts

A conflict is any failure of the landing check. The usual cause: two leaves of one package each passed alone and together do not compile (both added a helper `parse`; `declaredNames` in `newLeafRun` only saw the tree at level start). Handling:

1. The landing attempt fails like any attempt: recorded with its real class (`build`, `vet`, `test`) and reason, `swap.Fail()` restores the stub, the candidate file is removed, and an event `land_conflict` names the leaf.
2. The leaf does not escalate. It continues its ladder serially in the main tree, holding the turn: the failure text becomes `lr.prev`, the next attempt is a normal model attempt (`lr.oneRevision`) with `wt` the main tree. Budgets count: the failed landing is a charged attempt, so `fix_attempts` and `max_revisions` bound the loop.
3. Later jobs keep waiting on their turn, so ids after it land after it, whatever it costs. Order stays the id order.

There is no textual merge, so there is no merge conflict. File-level overlap between leaves is excluded before the level starts (section 4).

If `CommitLeaf` itself fails (`ErrIndexNotClean`, git errors) it is a harness fault, as in serial. The job returns the error, `halt(nil)` stops claiming, jobs behind it do not land, and the candidates of those jobs stay in `_state/` for resume. Nothing already committed is touched; no reset exists to undo it.

### 7.4 Linear history and the commit order

`git log --format=%s gm/<id>` after a run with `workers: N` equals, for the leaf commits, the serial run's log: Wave 0, then per wave per level the leaves in ascending id order, any repair commits, the final empty commit. There is no merge commit, no cherry-pick, no rebase; `Finish` still fast-forwards the base (R13). Hashes differ between runs (timestamps); subjects and the `GopherMind-Node` trailers do not, and the determinism test compares those.

## 8. Shared state

### 8.1 What is shared and why it is safe

| State | Where | Safety under parallel leaves |
|---|---|---|
| Blackboard claims | `blackboard/fs.go`: `mutate` takes `<node>.runtime.json.lock` through `runfs.Lock` (O_EXCL, 30 s stale reclaim, 40 s wait) per node | Locks are per node. `Claim` has one winner (`TestClaimRaceExactlyOneWinner`). The in-process `paths` map is under `f.mu`. Each leaf claims only itself |
| Heartbeats | `startHeartbeat` in `leaf.go`, one goroutine per leaf | Independent; a leaf waiting for its turn keeps heartbeating, so its claim is never swept as stale. Worst-case wait for a turn is a slower predecessor's whole ladder, which can exceed `stale_claim_seconds` only if heartbeats stop |
| Stale sweep | `ReleaseStale` at the start of `scheduleLeaves` | Runs before any claim of the level; unchanged |
| `_state/events.jsonl` | `blackboard/fs.go` `emit` through `runfs.AppendLine` (one `write` on an `O_APPEND` file) | Concurrent appends do not interleave. Event order across leaves is arrival order, not id order; consumers must not assume id order (the `runview` `Follow` reads lines as written) |
| Ledger ids | `ledger/fs.go` `Record`: `calls.lock`, then `nextID` reads, bumps and atomically rewrites `calls.seq`, then appends to `calls.jsonl` | One lock, short critical section: ids are unique and gap-free except for crashes (documented). With 8 workers the lock sees at most 8 contenders for milliseconds. The 40 s wait is far above this |
| Run state `_state/executor.json`, `leaf-results.json`, notes | `stateMu` (read-modify-write of `rc.state`), `repMu` (`rc.rep.reasons`, results) in `leaf.go` | Already written for a pool (the locks exist); the review checklist (section 14) re-checks every access |
| Critical-host streak | `proxy/streak.go` `Streak.mu`, then `observeCritical` under `stateMu` | Per node ids; the file is saved under the lock |
| Artifacts | `artifacts/artifacts.go` `Writer.Save(nodeID, ...)`; `attempts/<node>/NNN` | Per node folder, per node counter (`nextNumber`) |
| Router state | `router/router.go`: `r.mu` guards cooldown, disabled, missing, `ledgerErrs`; `slots` channels per provider | Built for concurrent calls (the planner and executor both call it) |
| Proxy | one `proxy.Proxy`; `logMu`, `mu` | Per-node URL and filters (section 5.3); the request log gets lines from several nodes, each tagged |
| Event sink | `events.Sink` ("Implementations must be safe for concurrent use") | `Collector` has a mutex. Any other sink wired by the CLI must meet the interface contract; the review checks the CLI's |
| Report counters | `rc.rep.weak`, `rc.rep.repairs`, `rc.rep.blocked` | Written in serial phases (Wave 0, repair) and under `repMu` in `merge`/`keepReason`; the new counters (section 13) use `repMu` |
| Human gate | `human.Gate` | One mutex around `Escalate` (decision F11); the leaf keeps its slot and heartbeat while it waits for an answer |

### 8.2 Ledger ids and `calls.seq`

Nothing changes in `ledger.FS`. The property to preserve and to test: with N goroutines calling `Record`, every id is unique, the largest id in `calls.jsonl` equals `calls.seq`, and the file has no partial line. A kill between the `calls.seq` write and the append leaves a gap, never a duplicate (`nextID`'s own comment); parallelism adds no new window.

### 8.3 Artifacts, report, scrub

Attempt folders are keyed by node, so parallel leaves write disjoint folders. The report is built once at the end from the ledger rows, the blackboard rows and the report counters, so concurrent writers do not exist at report time. Scrubbing of replies and check output (R15) is applied per attempt by the same writer. Candidate files in `_state/` hold gate-passed source (the same class of data as `prior-<id>`) and are deleted at landing.

### 8.4 Per-provider limits and the mini

`workers` is the number of leaf loops, not of model calls. Every model call goes through the router, which takes a slot for the provider (`acquire`) for the duration of the call only, then releases it. The call timeout (`CallTimeoutFor`) starts after the slot is taken, so a queued worker does not burn its timeout while waiting. Consequences:

- On the mini (`mini` has `MaxConcurrent: 1`, Ollama holding about 22 GB), `workers: 3` still sends one request at a time. The other leaves are either checking, waiting for the slot, or waiting for their turn. This satisfies "the mini serves one request at a time" without any extra rule in the executor.
- A chain with several providers (a public provider with `MaxConcurrent` 2) can serve several leaves at once, bounded by the sum of the slots on the entries each leaf is trying.
- At preflight, when `workers` exceeds the largest `max_concurrent` among providers in the executor chains, the executor emits one `warning` event: `workers N exceeds model concurrency M; extra workers only overlap checks`. It is a notice, not an error: an operator may want the overlap on the mini.
- Memory: each worker runs `go build`, `go vet` and `go test` (`-race` in wave checks only; leaf checks follow `runner`). The mini sits at 8 to 10 percent free memory, so the notes for operators say: check `memory_pressure` and `sysctl vm.swapusage` before raising `workers` there.

## 9. Stray-file check and stub swap per tree

- **Stray-file check.** `TakeSnapshot`/`Stray` run against the tree's own `gitland.CLI`. Every other leaf's writes are in other directories, so a sibling's files can never appear in this leaf's `git status`. The allowed set stays `{File, StubFile}`; anything else the check wrote (including ignored files) is reverted by `Revert(wt.Git, ...)` and fails the attempt as `forbidden_write`. The landing check repeats the check in the main tree, which is also where files written outside the tree (a sandbox failure) would show up.
- **Stub swap.** `Swap.Enter` and `Fail` work on `wt.Repo` through `pathsafe`; the stub swap is therefore per tree. In the main tree during landing there are never two swaps at once (turn chain). The `schedState.body` mutex remains for main-tree jobs.
- **Wave-0 content.** The tree starts from the main tree after the previous level landed: test files, type declarations, `go.mod`, `go.sum` and the stubs of leaves not yet landed. Siblings of the level are stubs in a leaf's tree. This matches serial behaviour for independent leaves, because a leaf that depends on a sibling is in a later level by definition (`levels()`).

## 10. Failure isolation and cancellation

- **A leaf failing.** Its attempts, stub restores and `forbidden_write` reverts happen in its own tree; the tree is deleted; its candidate (if any) is deleted; its row goes `failed` or `escalated`. No other tree and not the main tree has been touched. Its `done` channel closes so successors land. `merge` then counts it in id order, as today.
- **A panic.** `runLeaf` already turns a panic into a terminal failure of that leaf (`recover` in `leaf.go`). The goroutine wrapper in `runJobs` gets its own `recover` for panics outside `runLeaf` (tree setup, landing glue): it records a harness fault for that job, closes `done` and removes the tree, and the run stops through `halt(nil)`. A panic never kills the process.
- **A stop.** A `*stopError` (for example three critical-network failures on one leaf) halts claiming, as today. In-flight leaves finish their ladders; those that passed privately land in id order, then the stop is returned. This is the serial meaning of "in-flight leaves finish".
- **Cancel, SIGINT, `max_run_minutes`.** `runContext` (`limits.go`) cancels the context. Running checks end with `ClassCancelled` (the runner kills the process group), leaves return `Interrupted`, `markInterrupted` and the existing release path free their claims, trees are removed by the leaf's `defer`. A leaf waiting for its turn sees `ctx.Done()`, does not land, leaves its candidate in `_state/`, releases its claim and exits `Interrupted`. Nothing new is committed after the context ends.
- **Disk or copy failure** while making a tree: that job is a harness fault (plain error, claim released, leaf stays ready); no other job is affected, and no work was lost.

## 11. Resume

A run killed mid-level (SIGKILL, power) leaves: committed leaves of the level (a prefix of the id order, possibly with gaps if an early id failed), claims held by dead workers, candidate files, stale trees. On the next `brief run`:

1. `resume` and `scheduleLeaves` sweep stale claims (`ReleaseStale`; a claim with a fresh heartbeat refuses the resume with the existing "held by a live worker" message, now naming every live leaf, ids sorted).
2. `startRun` deletes `trees/` and `w/` (section 5.2).
3. Each released leaf is `ready`. In `runLeaf`, `adoptOnDisk` finds the main tree stub-only, and a new step reads `_state/candidate-<id>`: when it exists, the leaf takes the main tree and the turn, and runs the landing attempt with the candidate, with no model call. When the landing check fails, the leaf continues its ladder normally.
4. A leaf killed after its commit but before `SetStatus(verified)` is the existing "committed" adoption path (`rc.git.LeafCommit(id)`), unchanged. A leaf killed between writing its file in the main tree and committing is the existing "real file only on disk" path, unchanged: the landing check is the same check `adoptOnDisk` runs.
5. Leaves with no candidate and no commit start from the stub in new trees. Their earlier failed attempts are on the blackboard, so ladder budgets are not reset.

Resume does not require the same `workers` value. A run started with 3 can resume with 1 and the reverse; order and results are the same by F3 and F4. `_state/executor.json` records the value used for the report only.

## 12. Settings and the CLI

- `settings/executor.go`: `Workers` stays `int` with `yaml:"workers"`, default 1 (`DefaultExecutor`). `validate`: 1 to `MaxWorkers` (8, a constant in `settings`); the sentence `executor.workers must be 1 until leaf-isolated trees exist` and the `if e.Workers != 1` block are deleted. The error for a bad value names the key and the range only, as the other keys do (never the value).
- `executor.CheckWorkers(n)` (`executor/options.go`) now checks the same range and returns `--workers must be between 1 and 8`. Its test (`TestCheckWorkers` in `options_test.go`) and `settings/executor_test.go` change to the new range.
- `cmd/gophermind/brief_run.go`: `--workers` default becomes 0, meaning "use `executor.workers` from settings"; 1 to 8 overrides it for this run. The help text reads "leaves run at once (default from settings, 1)". The override is applied to the settings value handed to `Options.Settings`, not stored.
- `workerCount` returns `min(Workers, jobs to run)`, and 1 when the tree copy limit (F12) is exceeded or `diff_only` is combined with a repo whose file list cannot be copied. Not a setting: `executor.max_tree_mib` is deliberately absent (no configurability that was not asked for); the limit is a constant.
- Interaction with provider limits is section 8.4; `max_concurrent` stays the provider setting and validation of it is untouched.

## 13. Events and report

New events (all through `rc.emit`, so the sink and scrubbing are unchanged):

| Kind | Node | Message |
|---|---|---|
| `level_parallel` | empty | `wave W level L: N leaves, K workers` (once per parallel level) |
| `tree_ready` | leaf id | `private tree ready` |
| `leaf_candidate` | leaf id | `passed in its tree; waiting for its turn` |
| `leaf_landed` | leaf id | the short commit hash |
| `land_conflict` | leaf id | the failure class of the landing check |
| `warning` | empty | the workers versus model concurrency notice; the tree copy limit notice |

Existing events (`leaf_started`, `leaf_verified`, `leaf_failed`, `interrupted`, `blocked`) keep their meaning. `leaf_verified` is now emitted at landing, so for a parallel level `leaf_verified` events are in id order even though `leaf_started` and the attempt events interleave.

Report: `report.Report` (`report/report.go`) gains optional, omitted-when-empty fields in an object `parallel`: `workers` (the value used), `parallel_levels` (count), `landing_conflicts` (count). `SchemaVersion` stays 2 (additive). The summary prints one line only when `parallel_levels` is above 0. A graded run on the mini with the default prints nothing new.

## 14. Data-race review checklist (`go test -race`)

The review is a task of its own, done before the acceptance test is written, and its result recorded in the commit message. Each item is a read-the-code check, then a test run with `-race -count=3`.

1. Every field of `runCtx` written after `startRun` is under a lock or is written only in a serial phase: `rc.state` (`stateMu`), `rc.rep` (`repMu`), `rc.sched.interrupts` (`schedState.mu`), `rc.serveAddr`, `rc.bins`, `rc.accept`. List each with its guard in the review.
2. `runJobs`: `j.out`, `j.err`, `j.ran` are written by the job goroutine and read after `wg.Wait()` (happens-before holds); `wr.Stop` and `*stop` only inside `halt` under `mu`; `wr.Verified` and friends only in `merge`, after the wait.
3. The turn chain: `done` channels are closed exactly once (`sync.Once` or a single `defer`), never sent on.
4. `leafRun` fields are touched only by the leaf's own goroutine, except through the turn (the main-tree `Swap` is created by the goroutine that holds the turn).
5. The main `gitland.CLI` (`mu` guards commits) is called from one goroutine at a time during a parallel level; the `Dirty`, `Ignored`, `Restore` calls have no lock of their own, so the review verifies that no leaf goroutine calls the main CLI outside its turn (`rc.recordTip` and `tipNow` included, they run inside `finishPass`).
6. The per-tree `gitland.CLI` and `Checker` are never shared between leaves; `checkerFor` builds a value per tree and `rc.chk` is only read.
7. `events.Sink`, the proxy request log (`logMu`), `artifacts.Writer` and `router.Router` hold their own locks; the CLI's concrete sink is read and either shown safe or wrapped.
8. The test doubles are made race-safe in the same change: the fake `Checker` (`fc.LeafScript`, `fc.Hook` in `schedule_test.go` and `rig_test.go` are plain maps and funcs today), the scripted fake model (`Script`), `memSecrets` (already locked), the fake `Gate`.
9. `blackboard.FS.paths` (guarded by `f.mu`) and the lock files: confirmed by running `TestClaimRaceExactlyOneWinner` and a new ledger concurrency test with `-race`.
10. No goroutine outlives `scheduleLeaves`: heartbeats stop in `hbStop`, tree removers finish before `merge`; `goleak`-style check in the determinism test (count goroutines before and after).

## 15. Test plan

Tests run offline with the fake model and fake `Checker` rigs of `executor/rig_test.go` and one real-runner end-to-end test on a small fixture module. Run with the throwaway home from GOAL-UNIFICATION and `-race`. The package takes about 11 minutes serial; new tests are quick.

1. **Six-leaf acceptance (`TestParallelSixIndependentLeaves`).** Fixture `executor/testdata/sixpack`: six functions in six files across two packages, no `depends_on`, `workers: 3`. Expect: all six `verified`; run status `verified`; `git log --format=%s` lists the six leaf commits in ascending id order after Wave 0; each leaf commit's `git show --name-only` is exactly its `File` and the removal of its `StubFile`; `git status` is clean and `git diff <wave0>..HEAD --name-only` is the union of the six leaf files and stubs and nothing else; the fake `Checker` recorded `LeafCheck.Repo` values that were never the main repo during the parallel phase and were pairwise distinct; at least two checks overlapped in time (barrier hook); `go test -race ./...` of the package passes.
2. **Failing-leaf isolation (`TestParallelFailingLeafDoesNotTouchOthers`).** Same fixture; one leaf's model replies are scripted to fail the ladder and one writes a stray file. Expect: the others verify and land in id order; the failing one is `failed` or `escalated` (gate fake answers skip); the stray file is reverted in its tree and never appears in the main tree; hashes of the other leaves' trees (taken at the barrier) are unchanged by the failure; no `candidate-` file remains for the failed leaf; the failed leaf's stub is still in the main tree.
3. **Landing conflict (`TestParallelLandingConflictFallsBackToSerial`).** Two leaves of one package each declare the same helper in their replies. Expect: both pass privately; the lower id lands; the higher fails its landing check with a `build` class, emits `land_conflict`, continues its ladder in the main tree against a second scripted reply and verifies; commit order is still id order; the attempt count of the conflicted leaf shows the failed landing.
4. **Kill and resume (`TestParallelKillAndResume`).** Run `workers: 3` on the six-leaf fixture with a hook that cancels the context after three leaves have a candidate saved and before the first landing (variant B: after the second landing). Then run again with `workers: 2`. Expect: no model call for leaves that had candidates (the fake model counts calls); every leaf verifies; commit order equals the uninterrupted run's; trees directories are gone; claims released. Also a SIGKILL variant with a child process that is killed, leaving stale claims, resumed after `stale_claim_seconds` set to 1 in the test settings.
5. **Determinism (`TestParallelDeterministicCommitOrder`).** Repeat the six-leaf run 20 times with random per-leaf delays (seeded differently each repeat) and `workers` in 2, 3, 6; compare the leaf-commit subject list and the `GopherMind-Node` trailer list to the serial run's. All identical. The `leaf_verified` event order is also id order.
6. **Serial parity.** Existing suite unchanged and green with `workers: 1`; `TestWorkerCountIsAlwaysOne` and `TestNeverTwoLeafBodiesAtOnce` (`schedule_test.go`) are replaced by tests of `workerCount` (min rule, tree-size fallback) and of the body mutex in main-tree mode.
7. **Cancellation.** Cancel while three leaves run: all claims released, trees removed, no commit after cancel, process groups killed (a fake check that blocks on the context).
8. **Panic.** A fake model that panics in one leaf: that leaf `failed`, siblings land, the process survives.
9. **Gate serialisation.** Two leaves escalate at once: the gate fake sees calls one after the other, never overlapping.
10. **Settings and CLI.** `settings`: 1 and 8 valid, 0 and 9 refused with the key and range only; default 1. `CheckWorkers`: same range. `brief run --workers 3` reaches `Options.Settings` as 3; `--workers 0` keeps the setting; the old refusal text is gone (grep gate).
11. **Ledger concurrency.** 8 goroutines record 200 calls each: ids unique, `calls.seq` equals the maximum id, every line parses.
12. **Tree construction.** `gitland.NewScratch` and the copier: symlink not followed, `.git` and `.gophermind` not copied, ignored files not copied, executable bit kept, path escape refused; `Files()` includes untracked unignored files; cleanup refuses a path outside `trees/`; a copy over the limit makes the level serial with the warning.
13. **diff_only.** Six leaves with `landing: diff_only` and `workers: 3`: files land in the working tree, no commit exists, `changes.patch` equals the serial run's.

## 16. Rulings and their cost if wrong

| # | Ruling | Cost if wrong |
|---|---|---|
| F1 | Copy plus throwaway `git init`, not a worktree or a shared clone | A repo with a very large tracked tree copies slowly (F12 falls back to serial). Cheap to replace behind `NewScratch` |
| F3, F5 | Landing by replay of the candidate in the main tree, with a full check | Every passing leaf is checked twice (seconds of `go test` per leaf, no model call). Gains correctness equal to serial. A cheaper "replay without re-check" would let a combination failure surface only at the wave check |
| F4 | Id order, enforced by a turn chain, rather than completion order | A slow low id delays the commit of a fast high id; nothing is lost because the candidate is saved |
| F6 | Verified only after the commit | The blackboard shows fewer `verified` leaves during a level than finished private passes; the events show `leaf_candidate` for those |
| F7 | Range 1 to 8 | A user who wants more edits one constant; going above 8 on one disk and one `-race` toolchain is unlikely to help |
| F8 | `workers` does not limit model calls | An operator may think `workers: 3` means three model calls; section 8.4 and the preflight warning say otherwise |
| F9 | Repairs stay serial | A wave with many failures repairs slowly; same as today |
| F10 | Candidates persisted | An extra small file per leaf in `_state/`; without it a kill loses model calls on the free mini |
| F11 | One gate mutex | Two escalations are asked one at a time, in arrival order, which can differ between runs; commit order does not depend on it |
| F12 | 256 MiB copy limit as a constant | A repo just above it runs serially without being told why beyond the warning; the constant is easy to raise |

## 17. Assumptions and open items

1. **Assumption: the CLI hands the executor a settings copy.** `brief_run.go` was read only around the `--workers` check; the exact place where the settings value reaches `Options.Settings` must be confirmed when the override is written. If it is shared, the override copies the struct.
2. **Assumption: `git ls-files` and `git init` pass the `gitland` guard.** `guard` (`gitland/cli.go`) forbids `push`, `reset`, `rebase`, `checkout`, `stash`, `clean`, `gc`, `prune`, `remote`, `fetch`, `pull`, `filter-branch`, `update-ref` and flags like `--force`; `ls-files`, `init`, `add` and `commit` are not on the list. The scratch constructor uses only those.
3. **Open: the user's git wrapper.** Whether the wrapper intercepts the `git` that `exec.LookPath` finds during an executor run is not known; F1 avoids the question by never calling `worktree`. If the wrapper also blocks `git init` in a scratch directory under a protected checkout, the fallback is a tree without a repository whose stray check is a before/after hash walk of the tree (`hashPath` already exists). Not built unless needed.
4. **Open: sandbox profile cost.** One profile compiled per tree means `sandbox-exec` compiles the profile per command anyway (it does today); no measurement was taken. Measure with 8 workers before trusting the figure.
5. **Open: contention.** The unification spec (section 4.3, open item 5) asks that lock contention be measured before it is trusted. The ledger and runtime locks are held for milliseconds; the test in 15.11 records the maximum wait seen, and the plan makes a number above 1 s a failure.
6. **Not decided here:** a streaming landing optimisation and running the repair loop in parallel (section 3).
