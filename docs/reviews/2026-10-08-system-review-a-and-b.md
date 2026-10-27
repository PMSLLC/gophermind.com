# System Review: Sub-projects A and B against the code

Status (added 2026-10-27): point-in-time review of the A and B plans as they stood on 2026-10-08. Both sub-projects were built on `main` afterwards (for example `runfs`, `runview`, and the Enrich commits `6fe29d2`, `ef97650`, `8bb2d4e`), and some findings below are fixed there. Nothing here was re-checked against the built code; treat each finding as a lead to verify, not as a current defect. System finding 6 (main not docs-only) is superseded: main now carries code by design.

Date: 2026-10-08. Reviewer: Claude (Opus 5.5), read-only.

Inputs reviewed:
- `docs/superpowers/plans/2026-10-07-filesystem-run-state.md` (A, 9 tasks)
- `docs/superpowers/plans/2026-10-08-node-widening-and-question-model.md` (B, 13 tasks, commit `12be1bc`)
- `docs/superpowers/specs/2026-10-07-planner-phaseflow-unification-design.md`
- `docs/superpowers/specs/2026-10-07-node-widening-and-question-model-design.md`

Every claim was checked against the worktree `~/OtherProjects/PMSLLC/gophermind-planner-core`, branch `feat/briefv2-planner-core`, HEAD `08db179`. Code paths are relative to `gophermind-lib/briefv2/` unless they start with `cmd/`. "A:NNN" and "B:NNN" are line numbers in the two plans. Nothing was run and nothing was changed.

## 1. Verdict

Both plans are well grounded: almost every identifier, line range and interface they cite exists as described. Neither is ready to execute as written.

- **A**: 6 defects make tests fail or behave wrongly, 3 of them concurrency or durability bugs in the new `runfs` primitives, and 1 is a design limit (Windows).
- **B**: 4 defects block the build or the test suite, and 1 policy question breaks every existing approval. B also has several smaller correctness and privacy gaps.

All of them are plan-text fixes. None needs a design change except two policy questions for John (section 5).

## 2. System-level findings

1. **The PhaseFlow bridge design is obsolete.** The earlier draft ("Planner and PhaseFlow Bridge": a `TaskRunner` on PhaseFlow's wave loop, an `ExportLegacy`-style exporter, a FallbackRunner versus router question) is replaced by unification decisions 2, 4, 15 and 16. The v2 executor runs leaves, PhaseFlow is retired, and the run folder is read through `runview` (sub-project D). The one idea that survives is decision 5: the router owns model fallback. Don't revive the bridge sections.
2. **The approval-hash policy is the main cross-plan risk.** `RenderPlan` folds the name and length of every `hashedFiles` entry into the plan hash even when the file is absent (`planner/approve.go:224-232`). B changes that hash twice: Task 7 adds `_state/understanding.json` and Task 11 adds a "Node groups" section to the hashed text. `executor.LoadPlan` calls `VerifyApproval` (`planner/exported.go:26`), so after B every run approved before B fails to load. That includes an executor run already in progress. Decision 11 (finish the graded run first) keeps the graded run itself safe. Any later resume is not.
3. **"Untouched run folder" stops meaning untouched.** `untouchedRunDir` (`planner/load.go:212-242`) allows only `status.json` in `_state`. A adds `calls.jsonl`, `calls.seq`, `calls.lock`, `events.jsonl` and `replies/`; B adds `clarify/` and `understanding.json`. After A, a run that crashes in its first stage can't be replanned, and the error asks for a resume. Neither plan updates the function or records the behaviour change.
4. **The locking design is the foundation for sub-project F and has a race today.** Unification open item 5 says to measure lock contention before F raises the worker count. Finding A-N1 below is a correctness bug, not just a contention issue, and it occurs exactly when workers contend on leftover locks after a crash. Fix it in A, before F.
5. **Windows is a shipped target, but the A design is POSIX-only.** `.goreleaser.yaml:17` builds for Windows. On Windows, `os.Rename` onto a file another process has open fails. That hits `brief status` while `brief run` is running, and the 0600/0700 mode assertions also fail. Either gate the feature and tests by GOOS, or record "run state is POSIX-only" as a decision.
6. **Main is not docs-only right now.** The main checkout has an untracked `gophermind-lib/briefv2/planner/` and `.planning/plan/`, plus a deleted `.planning/assignments.json`. That contradicts decision 9. Find out who wrote them before any commit on main.
7. **Cost on the mini.** For a 50-leaf plan in about 8 components, B's Enrich adds roughly 25 to 35 model calls, and up to about 55 with repairs, before router retries. Each batch prompt repeats the brief section and up to 6 KB of decisions. The mini serves one request at a time. Unification section 5 already says to measure this on csvstat before the real brief; this review puts a number on it.
8. **Tier routing is half done.** Decision 10 (B spec) and unification section 5 ask for planning calls to be routed by `model_tier`. B routes only Enrich (`callSpec.tier`); Test-writer still uses the hard-coded strong tier (`planner/planner.go:203`), and B's own self-review (B:7397) admits it.

## 3. Sub-project A: filesystem run state

### 3.1 Defects, most severe first

| # | Severity | Finding | Evidence | Fix |
|---|---|---|---|---|
| A1 | High | Task 5 breaks the executor canary tests. Artifacts default to on (A:1725, A:2067), and the rig uses `settings.Default()`. `hasCanary` and `e2eStores` walk the run folder, which now includes `attempts/`, where the markers (not declared secrets) are saved verbatim. | `executor/harness_test.go:844`, `executor/e2e_test.go:229`, `run_test.go:698-714`, `resume_test.go:1131-1159`, `e2e_test.go:920,1070`, `leaf_test.go:761` | Decide per test: turn artifacts off in the rig, or exclude `attempts/` from the canary walk and add a separate test that `attempts/` holds only scrubbed text. The plan must name each test. |
| A2 | High | Task 7 misses two command tests built on `blackboard.db`. | `cmd/gophermind/brief_run_test.go:1236-1281` | Rewrite both tests against the run folder. |
| A3 | High | Task 7 under-specifies removing the database from tests. Raw SQL, SQLite file reads, `*SQLite`-typed fields and a `dbPath` parameter remain. | `planner/rerun_test.go:20,59`, `router/router_test.go:82,599-626`, `planner/e2e_test.go:503-504`, `packer/packer_test.go:510-520`, `executor/rig_test.go:84-85,129-169`, `planner/harness_test.go:87-88,153` | List each site and its replacement. |
| A4 | Medium | Planner replies are saved only in debug mode. `saveReply` is placed beside `dumpReply` inside `if p.dumpDir(r) != ""`. | A:2396, `planner/planner.go:207-212` | Call it outside that block. |
| A5 | Medium | `TestFSEventsAreOneJSONLinePerChange` can't pass: it decodes `run_id`/`node_id` into untagged fields. | A:669 | Add json tags. |
| A6 | Medium | The Task 8 no-SQL grep matches the doc comment at `blackboard/blackboard.go:8`. | A:2578 | Edit the comment in Task 8. The `executor/typedecl.go:34` hit is a known false positive (A:2592). |

### 3.2 New defects in the `runfs` primitives

| # | Severity | Finding | Evidence | Fix |
|---|---|---|---|---|
| A-N1 | Medium | **The lock reclaim can give two holders at once.** Stat-then-Remove of a stale lock lets two waiters each remove a lock, so one deletes the other's fresh lock. `unlock` also removes the lock unconditionally, so a holder paused for more than 30s deletes the next holder's lock. | A:407, A:412-414 | Write a unique token into the lock and remove only your own. Steal a stale lock by renaming it to a unique name first. |
| A-N2 | Medium | **One partial line breaks the ledger permanently.** `AppendLine` doesn't fsync and doesn't check for a trailing newline. After a crash, the next append fuses onto the partial line, and `readAll` fails on every call from then on (`Summary`, `List`, `Amend`, `brief status`). | A:429-447, A:1612-1613 | When the file doesn't end in a newline, write one first. Skip unparseable lines and log them as a warning. |
| A-N3 | Low | **Duplicate call ids after a power loss.** No directory fsync after rename and no fsync on append, so the `calls.seq` rename can be lost while the `calls.jsonl` line survives. That contradicts "never a duplicate" (A:1517-1519). | A:382 | Derive the next id from the highest id in `calls.jsonl` and drop the seq file. |
| A-N4 | Low | **`Watch` stalls silently** if `events.jsonl` is truncated or recreated, because it keeps a byte offset. The SQLite version used row ids. | A:1173, A:1185 | When the size drops below the offset, reset the offset. |
| A-N5 | Low | **`runtimePath`'s cache is never cleared.** A miss reloads the whole tree under `f.mu`. A reused run id with a new layout writes to old paths, and `MkdirAll` recreates them. | A:880-897 | Key the cache by the plan hash, or reload on a miss and drop stale entries. |

### 3.3 Unrecorded deviations from the spec

Add these to A's Deviations section, or fix them:
- `saveReply` doesn't scrub, but spec 4.3 rule 9 says replies go through the scrubbers.
- The lock's stale age is hard-coded to 30s (A:763, A:1455), but spec rule 3 says `stale_claim_seconds`.
- The replies path is `_state/replies/<stage>-<n>.txt`, but the spec says `_state/replies/<stage>/`.
- Spec 4.6 test 6 still names `.state.json` and `.claim`, which deviations 1 and 2 replace. Task 9 amends this; check that it does.

### 3.4 Verified correct

- **Blackboard interface** (`blackboard.go:110-148`, 13 methods): matched by the FS implementation, including `ErrNotFound`, `ErrBadTransition` and `ErrNotOwner`.
- **`Amend` callers:** `markMalformed` (`router/parsed.go:59`) is the only production caller.
- **`tree.Store.Load` skip list:** includes `*.runtime.json`, `_state/`, `logs/`, `.lock` and `.tmp-*` (`tree/store.go:76-84`).
- **Deleting `ResetRun`:** safe, because `rundir.Create` refuses an existing folder (`rundir.go:34-36`).
- **`LookupRun`:** exists (`planner/state.go:56`).
- **Database importers:** the only other importers of `db` are the cmd files the plan lists.
- **Settings names:** exist as named.
- **Path safety:** node ids and run ids are checked against regexes.

## 4. Sub-project B: node widening and the question model

### 4.1 Blocking defects

| # | Severity | Finding | Evidence | Fix |
|---|---|---|---|---|
| B1 | High | **The executor and cmd fixtures are never updated.** `executor/testdata/greeter/planner` (used by the executor rig and `cmd/gophermind/brief_run_test.go:219`) has no `enrich.*`, `enrich_comp.*` or `enrich_root.txt`, and its testwrite replies have no `polarity`/`covers`. fn-hello and fn-bye are `handler` nodes, so their security group can't be "not applicable". Task 13 only looks at `planner/testdata/greeter*`. Expect "no fixture reply for stage enrich:greet". | B Task 13 steps 1, 3, 5.2 | Add a step that writes these fixtures, and extend the error-count table to fn-bye, fn-hello and fn-serve. |
| B2 | High | **Every approval made before B fails to verify** (system finding 2). | Task 7 (B:3907), Task 11, `approve.go:224-232`, `exported.go:26` | John decides the policy (section 5, Q2). |
| B3 | High | **Resuming a run planned before B repeats Clarify and overwrites `answers.json`.** `clarifyDone` now needs a `Complete` store, and `loadQStore` doesn't migrate. A mid-stage question on such a run also loses every old answer. This breaks B's own Review Focus 6. | B:3381-3384, B:1970-1979, B:2196-2200, B:3770-3788, old `planner/clarify.go:39` | John decides (Q1), then add a migration or a refusal in Task 4. |
| B4 | High | **`TestPlanEndToEnd` hangs.** Its programmatic gate answers only `KindAsk` and `KindApprove`, never `KindConfirm`. | `planner/e2e_test.go:80-90`, B:3827 | Add a `KindConfirm` case in Task 6. |
| B5 | High | **Compile errors in the test packages:** <ul><li>Task 3 appends unprefixed identifiers to `package human_test` (B:1223-1338).</li><li>Task 6 removes `parseClarify`, but `planner/safeerr_internal_test.go:60-72` still calls it.</li><li>Task 8 redeclares `type draftFile` (B:5248), which already exists at `planner/decompose_test.go:18`.</li></ul> | as cited | <ul><li>Prefix with `human.`.</li><li>Move the canary checks to `parseQuestionList`.</li><li>Reuse `g.drafts()`.</li></ul> |
| B6 | High | **`embedDecisions` shares one record map between the root and the node.** A hand edit to one changes both, so two tests expecting exactly one problem fail. The finish-time check is also always true, because `finishTree` rebuilds the decisions from the store just before checking them, so it can never catch a hand edit on disk (spec section 10 asks for that). | B:2297-2321, B:1853-1856, B:5595, B:5872-5880 | Call `recordOf(q)` once per node. Check the on-disk tree before the rebuild. |

### 4.2 Correctness gaps

| # | Severity | Finding | Evidence | Fix |
|---|---|---|---|---|
| B7 | Medium | **A question during an Enrich repair corrupts the tree.** The id `enrich-_fix-qN` breaks the schema pattern `^[a-z0-9][a-z0-9:-]*$`, so `ParseNode` fails. The shared stage name `enrich:_fix` also replays one component's question into another component's repair prompt. | B:280-300, `planner.go:237-260,282`, `decompose.go:695` | Name the repair stage per component without an underscore, for example `enrich:fix:<comp>`. |
| B8 | Medium | **Open questions on function nodes have no exit.** `groupDefects` accepts a non-empty `open_questions`, so approve then refuses. The fix it suggests (`brief answer`) only changes settled questions. Components and the root already reject them in `structureCall`. | Task 8 group checks | Make a non-empty `open_questions` an Enrich defect, so it goes to the repair loop. |
| B9 | Medium | **An unattended run can fail permanently on a fact question.** A fact with a valid `fact_key` that the probe can't answer, and with no `recommended`, errors on every resume. | B:2168, B:3509-3512 | In unattended mode, require a recommendation for facts too, or turn unanswerable facts into decisions when parsing. |
| B10 | Medium | **The approval doesn't bind the component and root groups.** `hashedFiles` leaves out `_state/enriched.json` and `_state/clarify/questions.json`, but `finishTree` rebuilds nodes from them after approval. | Task 7, Task 9 | Add both to `hashedFiles`, together with the Q2 policy. |
| B11 | Low | **A crash mid-round can lose answers.** `File.Ask` renames `QUESTIONS.md` to `QUESTIONS.answered.md` before the planner saves the answers. On resume, nothing reads the `.answered.md` file back. | `human/file.go:111`, B:3521-3545 | Save first, then rename. Or read `.answered.md` on resume. |
| B12 | Low | **A question answered mid-Enrich can't be cited.** The settled-decision set is computed once per Enrich run, so citing that answer fails with `decision_unknown`. | Task 8 | Recompute the set per batch. |
| B13 | Low | **`IsStdPackageName("uuid")` returns true** because `packer/stdlist.txt:179` lists it, so third-party `uuid.UUID` is never flagged. It only produces a warning. | B:1019 | Match on the full import path where it is available. |

### 4.3 Privacy

- **Window between commits.** After Task 8, drafts carry `rationale` and `decision_ids`, but `writeTests` still sends the whole draft at node scope (`mustJSON(d)`, `testwriter.go:162`) until Task 9. Move `testwriterView` into Task 8 so no commit leaks.
- **Unescaped opening tag.** `sectionTagRE` (`packer.go:380`) doesn't list `guidance`, so a forged opening `<guidance>` tag is not escaped. The closing tag is. Add it in Task 12.
- **Verified:** `testwriterView` and `guidanceFrom` use fixed allow-lists, each with a canary test, and Task 13 scans every Test-writer prompt. Enrich uses component scope and the root call brief scope, and `router/privacy.go:17-24` keeps node scope on public providers only with `--allow-public`.

### 4.4 Spec coverage gaps

- **Tier routing (spec section 4, decision 10):** Test-writer is not tier-routed (system finding 8).
- **Spec section 6, trust boundaries:** counted per plan, not per component.
- **Spec section 6, approval summary:** component and root assumptions are missing from it.
- **Spec section 5.3, summary:** the go.mod checks that were skipped are not named in it.
- **Spec section 10, missing tests:**
  - no golden bytes-per-node size test;
  - no Enrich stage test for a `QUESTION:` reply pausing and resuming;
  - no test for a truncated reply halving the batch;
  - no test that approve refuses an unconfirmed understanding. The open-question refusal is tested (B:7316-7335).
- **Spec section 7.7:** a mid-stage answer resets the whole component (Decompose and Enrich), not one call. This deviation is not listed.
- **Fixture mismatch:** Task 13 says Farewell's parameter is `n`, but the fixture has `func Farewell(name string)`.

### 4.5 Verified correct

- **Load skip list:** `tree.Store.Load` parses any root-level `*.json` that is not on the skip list, so deviation 1 (`_state/understanding.json`) is right.
- **Schema additions:** optional and additive. `contract.errors` items have no `additionalProperties: false`, so old fixtures validate.
- **Stage mechanism:** stages are `{name, run, done}` and resume skips any stage whose `done` is true. `clarify:more` maps to `clarify.more.txt`.
- **Human gate implementers:** all listed, so the interface change compiles apart from B4 and B5.
- **Question store against hostile replies:** handles cycles, forward refs, duplicate ids, caps and invalid recommendations. Error messages carry positions and counts only.
- **Task 12 and executor hashing:** Task 12 leaves `hashNodes` alone. A node planned before B gives an empty guidance section and an unchanged prompt.
- **Helpers and signatures:** every helper Tasks 1 to 13 call exists or is created by the plan, and signatures match across tasks.

## 5. Questions for John

1. **Runs planned before B (B3).** Import an existing `answers.json` as a complete question store, so resume works and Clarify is skipped? Or refuse to resume a pre-B run? Recommendation: import. It is about 30 lines in `loadQStore`, and refusing strands any run in progress.
2. **Approvals made before B (B2, B10).** Accept them by hashing a `hashedFiles` entry only when the file exists, and putting the new summary section behind a `schema_version`? Or invalidate them and require re-approval? Recommendation: invalidate, but make `VerifyApproval` name the cause ("approved before node widening; run brief approve again") instead of a bare hash mismatch. Silently accepting an approval that never saw the groups weakens the gate the approval exists for.
3. **Windows (system finding 5).** Support Windows for run state, or record it as POSIX-only for now? Recommendation: POSIX-only for now, gate the tests by GOOS, and say so in the README.

## 6. Recommended order

1. John answers section 5.
2. Amend plan A: section 3 fixes, the lock and append fixes, and the deviations. Each fix is plan text only.
3. Amend plan B: blockers B1 to B6 first, then B7 to B10, then the privacy window and the spec gaps.
4. Keep decision 11: neither plan starts until the GOAL.md graded run has finished.
5. Before any commit on main, resolve the stray code in the main checkout (system finding 6).
