# System Review: Sub-projects A and B

First written 2026-10-08 as a review of the A and B plans against the branch code (`feat/briefv2-planner-core` at `08db179`). Rechecked 2026-10-27 against the built code on `main` at `d627459`, after both sub-projects had landed. Each finding below carries its status as of the recheck. Reviewer: Claude (Opus 5.5), read-only; nothing was run.

Paths are relative to `gophermind-lib/build/` (the old `gophermind-lib/briefv2/`) unless they start with `cmd/` or another top-level folder. The plans reviewed were `docs/superpowers/plans/2026-10-07-filesystem-run-state.md` (A) and `docs/superpowers/plans/2026-10-08-node-widening-and-question-model.md` (B).

## 1. Where things stand

Thirteen findings remain open, one of them partly fixed (section 2, most severe first). Everything else from the 2026-10-08 review is fixed (section 3), accepted, or no longer applies (section 4). Section 5 records that two of the three policy questions were decided in code.

The most severe open items:
- The `runfs` lock and append primitives still have the race and the partial-line defects (A-N1 and A-N2). These matter most for sub-project F (parallel waves), where several workers contend for the same locks after a crash.
- Function nodes with open questions still stall at approve (B8).
- The repair stage name `enrich:_fix` is still shared by every component (B7).

## 2. Open findings, most severe first

| # | Severity | Finding | Evidence (2026-10-27) | Suggested fix |
|---|---|---|---|---|
| A-N1 | Medium | **The lock reclaim can give two holders at once.** A stale lock is removed with stat-then-remove, so two waiters can each remove one, and one deletes the other's fresh lock. `unlock` removes the file without checking that it holds the lock, so a holder paused past 30s deletes the next holder's lock. The comment at `runfs/runfs.go:89-93` calls the window negligible. It matters more once F adds workers. | `runfs/runfs.go:104,109-111` | Write a unique token into the lock and remove only your own. Steal a stale lock by renaming it to a unique name first. |
| A-N2 | Medium | **One partial line breaks the ledger permanently.** `AppendLine` doesn't fsync and doesn't check for a trailing newline, and `readAll` fails on any line that isn't JSON. After a crash, `Summary`, `List`, `Amend` and `brief status` fail for that run from then on. The A-N3 fix depends on this too: `maxLoggedID` skips lines it can't parse, so an id fused onto a partial line is invisible to it. | `runfs/runfs.go:126-144`, `ledger/fs.go:141,229-231` | Before appending, write a newline when the file doesn't end in one. Skip unparseable lines with a warning. |
| B8 | Medium | **Open questions on a function node have no way out.** `mergeEnrichment` copies a function reply's `open_questions`, and `groupDefects` doesn't reject a non-empty list, so the node stalls at approve. The approve error points to `brief answer`, which can't clear such a question. Component and root replies are rejected. | `planner/groups.go:132-140,236-239`, `planner/enrich.go:180-183,758`, `planner/approve.go:56-58` | Make a non-empty `open_questions` on a function node a group defect, so it goes through the repair loop. |
| B7 | Medium | **The repair stage is shared across components.** Commit `6fe29d2` widened the id pattern, so `enrich-_fix-qN` now parses. The stage name is still the shared `enrich:_fix`, so `callAsking` can replay one component's answers and pending question into another component's repair. A question raised there maps to no component, so its reset is broader still (see 7.7). | `planner/enrich.go:26,440-443`, `planner/planner.go:297-341` | Name the repair stage per component, for example `enrich:fix:<comp>`, so `componentOfStage` resets the right one. |
| S5 | Medium | **Windows is still a release target, but run state is POSIX-only.** `runfs` has no GOOS gate, the mode-0600 test isn't gated, and no doc says the run state is POSIX-only. On Windows, a rename onto a file another process has open fails, which hits `brief status` while `brief run` is running. | `.goreleaser.yaml:17,55`, `runfs/runfs.go:70,104`, `runfs/runfs_test.go:30` | Decide (question 3 in section 5). The recommendation is to record run state as POSIX-only and gate the tests by GOOS. |
| A-stale | Low | **Two settings disagree about when a holder is dead.** Locks go stale after a hard-coded 30s. Claims go stale after `stale_claim_seconds` (default 120), which only drives `ReleaseStale`. Spec 4.3 rule 3 says the lock should use `stale_claim_seconds`. | `runfs/runfs.go:82`, `blackboard/fs.go:59`, `ledger/fs.go:43`, `settings/executor.go:46` | Pass the setting through, or record the 30s as a deviation and explain why it differs. |
| B6b | Low | **The tree-finish decision check always passes.** `embedDecisions` rebuilds the documents from the store just before `finishGroupDefects` checks them, so a hand edit to the tree on disk is never seen. Commits `70b15f8` and `79926dd` added a dangling-id check but no on-disk check. | `planner/testwriter.go:953,1021-1022`, `planner/decompose.go:925`, `planner/finish.go:173-188` | Run `checkEmbedded` against the tree read from disk before the rebuild, or on resume when a tree exists. |
| B11 | Low | **A crash in the middle of a round can lose answers.** `File.Ask` renames `QUESTIONS.md` to `QUESTIONS.answered.md` before it returns, which is before the planner saves the answers. On resume, nothing reads the `.answered.md` file back. | `human/file.go:69-71,130-132` | Rename after the caller has saved, or read `.answered.md` back on resume. |
| A-N4 | Low | **`Watch` stalls silently if `events.jsonl` shrinks.** It keeps a byte offset and never resets it, so once the file is truncated or recreated, the offset is past the end. | `blackboard/fs.go:468,480-484` | When the file is smaller than the offset, reset the offset to 0. |
| A-N5 | Low | **The `runtimePath` cache is never cleared.** A reused run folder with a different component layout gets the old paths. | `blackboard/fs.go:176-190` | Key the cache by the plan hash, or drop it when the tree is reloaded. |
| S3 | Low | **Partly fixed: a crash during Clarify still blocks a replan.** `untouchedRunDir` now allows A's state files, but not B's `_state/clarify/`, so a crash during Clarify still forces a resume. | `planner/load.go:255-262`, `planner/clarify.go:31` | Add `clarify/` to the allowed list, or record that a Clarify crash means resume. |
| 7.7 | Low | **A mid-stage answer redoes the whole component, but spec 7.7 says one call.** `resetComponent` resets the whole component, and the deviation is not recorded in `docs/build/README.md`. | `planner/change.go:128-137,166` | Record the deviation, or narrow the reset. |
| S7 | Info | **Enrich cost is unmeasured on the mini.** Enrich makes about 0.25 calls per leaf plus about 1 per component: roughly 25 to 30 calls for 50 leaves in 8 components, before up to 2 repair rounds and batch halving. `enrich_batch_size` (default 4, range 1 to 32) is now a setting. | `settings/settings.go:138,199,419`, `planner/enrich.go:25,512-531` | Measure it on the csvstat brief before the real brief. |

## 3. Fixed since 2026-10-08

| # | What was wrong | Where it is fixed |
|---|---|---|
| S1 | The PhaseFlow bridge draft was obsolete | PhaseFlow is retired: no `phaseflow` package remains, `gates/removal_test.go:77-85` bans it, and `gophermind phase` prints a "removed" message (`54cb713`) |
| S2 / B2 | Approvals made before B failed with a bare hash mismatch | `VerifyApproval` names the cause when an approval has no `UnderstandingHash` (`planner/exported.go:30-38`, `957848f`). An approval written after the understanding existed but before node groups still gets the generic message, which affects only dev runs. |
| S8 | Test-writer did not route by node tier | `p.tierFor(model_tier)` in `planner/testwriter.go:515` and `planner/revise.go:466` (`79926dd`) |
| A1 | Artifacts broke the executor canary tests | The walks skip `attempts/`, and a dedicated test covers secrets there (`executor/harness_test.go:847-851`, `executor/e2e_test.go:236-240`, `executor/artifacts_test.go:57`) |
| A2, A3 | Tests still used SQLite and `blackboard.db` | Only negative assertions remain, and the gate is `nosql_test.go` |
| A4 | Planner replies were saved only in debug mode | `saveReply` has its own wrapper (`planner/planner.go:249-257`) |
| A5 | The events test decoded into untagged fields | json tags added (`blackboard/fs_test.go:482-485`) |
| A-N3 | Call ids could repeat after a power loss | The next id is the higher of `calls.seq` and the highest logged id (`ledger/fs.go:108-145`). This depends on A-N2. |
| B1 | Executor and cmd fixtures were missing Enrich replies | Greeter fixtures in the round format (`14b3824`) |
| B3 / Q1 | Resuming a run planned before B repeated Clarify | `loadQStore` imports the old `answers.json` as a complete store (`planner/questions.go:84-114`) |
| B4 | `TestPlanEndToEnd` hung on the confirm gate | A `KindConfirm` case (`planner/e2e_test.go:90-91`) |
| B5 | Three compile errors in tests | All resolved |
| B6a | Root and nodes shared one record map | A copy per node (`planner/questions.go:517`) |
| B9 | An unattended fact question with no recommendation failed every resume | Facts need a recommendation too (`planner/questions.go:210,341-342`) |
| B10 | The approval did not bind the component and root groups | `stateEnriched` is hashed (`planner/approve.go:272`). `_state/clarify/questions.json` is not, but the derived `answers.json` is, which is enough. |
| B12 | An answer settled mid-Enrich could not be cited | The settled set is rebuilt in each call's parse (`planner/enrich.go:447-455`) |
| Privacy | The Test-writer could receive the whole draft; `<guidance>` was not escaped | Test-writer gets a projection (`planner/testwriter.go:506`), `sectionTagRE` includes `guidance` (`packer/packer.go:454`), and canary tests cover both |
| 4.4 gaps | Spec coverage gaps | Covered: trust boundaries per component, component and root assumptions, and named skipped go.mod checks, all in `planner/groupsummary.go`. Tests added: bytes-per-node (a 24 KiB ceiling rather than golden bytes), Enrich QUESTION pause and resume, truncated-reply halving, and approve refusing an unconfirmed understanding. |

## 4. Accepted or no longer applicable

- **A6, not applicable.** The no-SQL gate now parses imports, so a doc comment can't trip it.
- **Reply scrubbing, accepted.** Planner replies are not scrubbed. `planner/replies.go:12-17` records why: planner prompts carry no secrets.
- **Replies path layout, accepted.** Replies are saved flat as `_state/replies/<stage>-<n>.txt`, not under `<stage>/` as spec 4.3 says. Update the spec to match the code.
- **B13, accepted.** `uuid` is a real package in the pinned Go standard library (go1.27.1), and a test asserts it. The clash with third-party `uuid` only causes a warning.
- **Farewell parameter, not applicable.** The mismatch was in the plan text only; the fixture uses `name`.

## 5. Policy questions

1. **Runs planned before B.** Decided in code: an old `answers.json` is imported as a complete question store.
2. **Approvals made before B.** Decided in code: they are refused with a named cause, and there is no `schema_version` path for accepting them.
3. **Windows.** Still open (S5).

## 6. Suggested next steps

1. Fix A-N1 and A-N2 in `runfs` and the ledger, with tests for two contending waiters and a fused partial line. Do this before sub-project F.
2. Fix B8 and B7 in `planner/groups.go` and `planner/enrich.go`.
3. Decide Windows (S5), then gate or document it.
4. Pick up the low-severity items as cleanup: A-stale, B6b, B11, A-N4, A-N5, S3, and the 7.7 deviation note.
