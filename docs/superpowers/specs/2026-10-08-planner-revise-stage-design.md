# The Planner Revise Stage: Design (sub-project E)

Status: draft for review. Date: 2026-10-08.
Scope: sub-project E of `2026-10-07-planner-phaseflow-unification-design.md` (GOAL-UNIFICATION task 5). When a leaf has used its ladder or its model reports a contract problem, the planner rewrites the node (description, contract text, tests, groups) inside fixed rules, the executor builds from the rewritten node, and every rewrite is recorded and checkable. Code paths are in the worktree `/Users/jbrahy/OtherProjects/PMSLLC/gophermind-planner-core` (branch `feat/briefv2-planner-core`), under `gophermind-lib/briefv2/`.
Builds on: A (`2026-10-07-filesystem-run-state.md`: runtime files, `events.jsonl`, `attempts/`), B (`2026-10-07-node-widening-and-question-model-design.md`: widened nodes, `groupDefects`, decisions in the tree), the executor spec (`2026-09-30-v2-executor-design.md`, section 7.3 and R16) and the prompt `docs/briefv2/handoff/prompts/06-revise.md`.
Sequencing: built on the branch after A, B and C (GOAL-UNIFICATION task 5), by `docs/superpowers/plans/2026-10-09-planner-revise-stage.md`. Section 12 lists what in A, B and G this spec touches.

## 1. Why this exists

Neither system rewrites a node after a failure (`docs/reviews/2026-10-07-planner-executor-v1-v2-review.md`, item 9).

- v1 (`gophermind-lib/phaseflow/revise.go`): `ApplyRevision` recorded a new definition and reset the task to pending, but the new `Deliverable` and `Test` were never fed to the next attempt. The reviser's work was discarded (review item 1). The lesson: a revision is only real if the next model call is built from the revised text. This spec makes that an acceptance check (section 14, test R1): the prompt of the first attempt after a revision must contain the revised sentence.
- v2 (`executor/revise.go`, `leafRun.revise`): rung 5 makes one strong-tier call that answers with up to 5 hint notes (`packer.ParseRevise`), stored in `_state/notes.json`, and raises the row's revision. Node files and `contracts.json` are immutable during a run (R16, checked by `Plan.Hashes` in `planIntact`). Hints cannot fix a test that contradicts its contract, and the executor spec lists the planner revise stage as out of scope (its section 17, X6).
- `contract_flagged` has no real producer anywhere (review item 6).

## 2. Decisions

| # | Question | Decision | Why |
|---|---|---|---|
| E1 | Who rewrites | New planner code, `planner.ReviseNode`, called by the executor through a `Reviser` seam. The executor never edits a node file | Rule 3 of the decomposer rules and R16 keep the executor read-only on the plan |
| E2 | What the model returns | A patch of allowed fields, not a whole node (changes 06-revise.md, section 5) | Fields that must not change cannot be expressed; the reply is smaller |
| E3 | Approval | `approval.json` keeps binding revision 0. A revised body is bound by a per-node hash chain in `_state/revisions/`, verified by the executor at load and before every commit (section 8) | The plan hash does not cover `tree/` today (`hashedFiles` in `planner/approve.go`), and re-approval per revision would stop unattended runs |
| E4 | Signature, `depends_on`, split | Never a revision. Outcome `contract_flagged` (or `split_required`): the leaf escalates to the human gate with the model's paragraph (section 7) | GOAL-UNIFICATION task 5; contracts are fixed by the approved plan |
| E5 | Where a human decides | The existing escalation gate, plus a decision record in a revision decision store (section 9) | `answers.json` is in the plan hash, so revision decisions cannot go into the question store |
| E6 | Bound | `max_revisions` rewrites per leaf (node `budget.max_revisions`, else `defaults.max_revisions`, plus human grants), then escalate | Same counter the executor already uses (`rc.maxRevisions`) |
| E7 | Tests | Rewritten by the existing Test-writer call on the revised node, never by the revise model, and checked red against the stub | The test stays derived from the contract alone, and a revision cannot weaken a test to pass |
| E8 | Privacy | The revise call is node scope by default and sees no reasoning group; a component-scope variant exists under the Enrich rule (section 5) | Router eligibility (`router/privacy.go`) |
| E9 | Notes mode | The old hint-only rung stays as `revise.mode: notes` | Regression escape hatch and a fallback for configurations without a Test-writer tier |

## 3. Goals and non-goals

Goals:

- A leaf whose ladder is used up (or whose model says `CONTRACT_PROBLEM`) gets its node rewritten, and the next attempt builds from the rewritten node.
- Every rewrite is bounded, validated by the same checks that built the node, recorded, resumable after a kill, and visible in the report.
- A rewrite can never change a signature, a package, a file, `depends_on`, a wave or a model tier.

Non-goals:

- Changing `contracts.json`, splitting a node into several, or adding or removing nodes. These are flagged to a human (E4). A later sub-project can add a contract-change flow.
- Re-approval by a person for each rewrite.
- Revising component or root nodes, or the acceptance tests.
- Changing the planner stages before Test-writer, or `brief answer` (`planner/change.go`): `changeAnswerIn` refuses once `_state/executor.json` exists, so a revision never races a reset.

## 4. Triggers and flow

Today `leafRun.afterRevision` (`executor/leaf.go`) decides after each ladder pass: `revTooLong` escalates at once; `revContractProblem` goes to the revise rung once per revision (`lr.cp[lr.rev] >= 2` escalates); ordinary exhaustion goes to the revise rung while `lr.rev < rc.maxRevisions(l)`, else escalates with `lastClass`. In `revise.mode: rewrite` the revise rung (`lr.revise`) becomes:

1. Trigger `ladder_exhausted` (every entry failed, `revExhausted`) or `contract_problem` (`revContractProblem`). `revTooLong` and `revInterrupted` do not trigger a revision, as today.
2. Build the request (section 5) and call `Reviser.Revise`.
3. Outcome `rewritten`: the ladder restarts at rung 2 from the new node (section 10). `lr.rev` is the new revision.
4. Outcome `contract_flagged`, `split_required`, `unusable` (reply invalid after the router's repair, or a no-op rewrite) or a fixed-class refusal: the leaf goes to `toHuman` with that class as the reason. A flagged or unusable outcome does not consume a revision.
5. Outcome `interrupted` (context ended, provider cooling): claim released, nothing charged, as `reviseStop{Interrupted: true}` does today.

The existing guards stay: a second `CONTRACT_PROBLEM` in one revision escalates at once, `identical_reply` and the per-revision charge counting are unchanged.

## 5. The revise call

Stage `revise:<node-id>`, task type `revise`, scope `router.ScopeNode`, tier from `revise.model_tier` (default `strong`). The call goes through `router.CallParsed` with a parse closure that runs the whole validation of section 6, so a reply that fails validation gets the router's one repair retry, and then the outcome is `unusable`.

Exact inputs, built by code from files and memory (never from a model reply):

| Input | Source | Notes |
|---|---|---|
| Node projection | node file | `id`, `title`, `description`, `node_class`, `model_tier`, `contract` (package, file, signature, inputs, outputs, errors, side_effects), `context` (dependency_signatures, constraints, notes), `tests` (name, given, expect, polarity, covers), `profile_hooks`, and the groups `construction`, `security`, `observability`, `performance`, `portability` |
| Contract slice | `contracts.json` | `contract.Slice`, as the implement prompt gets it |
| Test file source | repo, via `testSource` | checked against `TestSHA256` first |
| Attempt history | `lr.history` (`historyLine`) | provider/model, verdict, class and failing test names; scrubbed by `rc.scrubHistory` |
| Last failure text | `lr.prev` | condensed to 2048 bytes (`packer.Failure`), scrubbed by `rc.scrubText`; memory only, so after a resume the first check rebuilds it at no model cost |
| Trigger | executor | `ladder_exhausted` or `contract_problem`, plus the model's CONTRACT_PROBLEM sentence (`lr.problem`, 300 bytes at most, scrubbed, never persisted) |
| Revision | executor | the current revision and how many remain |

Never sent at node scope: `rationale`, `alternatives`, `refactor_notes`, `assumptions`, `open_questions`, `decision_ids`, `decisions`, `brief_ref`, the brief section, any reply text from an implement call, any raw check output beyond the last failure, any secret value. These are the groups B already keeps out of every node-scope call.

Component-scope variant: `revise.context: component` adds the brief section, `rationale`, `assumptions` and the records named by `decision_ids`, and sets the scope to `ScopeComponent`. The router then admits public providers only when `privacy.mode` allows it or the run was started with `--allow-public`, exactly as Enrich (`planner/enrich.go`, `ScopeComponent`) is routed. Under `private_only` (the GOAL configuration) both variants run on the mini. If no provider is eligible the call fails with the router's privacy reason, the outcome is `interrupted` with a configuration class, as `reviseFailure` does today for `ReasonPrivacy`.

The prompt is `planner/prompts/revise.md`, derived from 06-revise.md with these changes: the reply is the patch of section 6; `{{.BriefSection}}` appears only in the component variant; the pattern list is kept (same one or two failing tests, different failing tests, CONTRACT_PROBLEM, node too large); the `SPLIT_REQUIRED` answer is a line plus a one-paragraph reason (the JSON list of new functions is not parsed in E). No prompt text is saved; the reply is saved by `Planner.saveReply` as `_state/replies/revise_<node>-<n>.txt` after the secret check of section 6.

## 6. Output contract and validation

The reply is exactly one of:

1. A JSON object (fences allowed), parsed with `DisallowUnknownFields`, with these optional keys and no others: `description` (string, 1 to 2000 bytes), `contract` (`inputs`, `outputs`, `errors`, `side_effects`; same item shapes as the node schema), `context` (`constraints`, `notes`), `groups` (any of `construction`, `security`, `observability`, `performance`, `portability`, `profile_hooks`), `rewrite_tests` (bool), `assumptions` (1 to 5 strings of at most 300 bytes: why each change was made). At least one change key must be present.
2. The line `CONTRACT_CHANGE_REQUIRED` and one paragraph (at most 600 bytes) naming what has to change.
3. The line `SPLIT_REQUIRED` and one paragraph.
4. `CONTRACT_PROBLEM: <sentence>` is not accepted from the revise model (it is the answer the implement model gives); it counts as malformed.

Code merges the patch into a copy of the node and runs, in this order, stopping at the first failure. Messages name a rule and at most a count, never quote the reply (the B convention; each check has a canary test).

1. Frozen fields equal the node's: `id`, `kind`, `parent`, `children`, `depends_on`, `wave`, `model_tier`, `title`, `node_class`, `budget`, `network`, `secrets`, `requirement_ids`, `decision_ids`, `decisions`, `brief_ref`, `contract.package`, `contract.file`, `contract.signature`. The patch grammar cannot express most of these; the check guards the merge code.
2. Input names: the set of `contract.inputs[].name` equals `paramNames(signature)` (`planner/enrich.go`). A rewrite whose inputs no longer match the signature is a signature change in disguise and gets outcome `contract_flagged`.
3. `groupDefects(merged, class, env)` (`planner/groups.go`) is empty, with `env` from `groupEnvFor`. Not-applicable answers follow the same table as at planning.
4. `open_questions` is still empty, and `assumptions` gains the reply's items prefixed `revision <n>:` (the only append the code makes).
5. Secrets: no string of the patch contains a value from the executor's secret list (`rc.secretValues()`, passed in the request). A hit rejects the reply with class `secret_in_reply` and nothing is saved from it.
6. Schema: the merged document passes `tree.ParseNode`.
7. Not a no-op: the merged body differs from the current one (otherwise `unusable`, class `revise_noop`).

Tests: if `rewrite_tests` is true, or the patch changes `contract.inputs`, `outputs` or `errors`, the planner calls the Test-writer for the revised node: the existing prompt with `testwriterView(revised)`, stage `testwrite:<node>-r<n>`, scope node, tier `p.tierFor(model_tier)`, validated by `parseTestwrite` (polarity and covers via `checkPolarity`, error-to-test link via `linkErrorTests`, source rules via `checkTestSource`, the `bench` hook check). The revise model never writes test source. With `revise.rewrite_tests: off`, a patch that needs tests is `contract_flagged` with class `tests_locked`.

The new test file must then be red against the stub: the executor runs `rc.leafCheck(ctx, l, []string{l.StubFile})` (the check `redCheck` uses). A pass is rejected (class `revise_vacuous_test`); a build or vet failure is rejected (class `revise_red_build`). A rejected test leaves node, test file and logs untouched.

## 7. What may and may not change

| May change | May not change |
|---|---|
| `description`; `contract.inputs`, `outputs`, `errors`, `side_effects` (names of inputs fixed by the signature); `context.constraints`, `context.notes`; `tests` and the test file body; `construction`, `security`, `observability`, `performance`, `portability`, `profile_hooks`; `assumptions` (append only); `revision` (code) | `signature`, `package`, `file`, `depends_on`, `id`, `kind`, `parent`, `children`, `wave`, `model_tier`, `title`, `node_class`, `budget`, `network`, `secrets`, `requirement_ids`, `decision_ids`, `decisions`, `rationale`, `alternatives`, `refactor_notes`, `open_questions`; test file path and function name; `contracts.json`; every file in `hashedFiles` |

`contract_flagged` and `split_required` are results, not blackboard statuses (the statuses of `blackboard/transitions.go` are unchanged). The leaf walks `in_progress -> needs_revision -> escalated` through `rc.escalate`, its `_state/leaf-results.json` reason is the class, the report lists the class under that leaf, and a `flag` record is appended to the revision log (section 9). Dependents are `blocked` as for any escalated leaf. Dependents of a revised node are unaffected: they consume only its signature, which cannot change.

## 8. The approval rule for revised bodies

Today the approval binds more than the summary. `RenderPlan` hashes the markdown plus the bytes of `hashedFiles` (`contracts.json`, `_state/decomposed.json`, `classes.json`, `coverage.json`, `answers.json`, `dependencies.json`, `understanding.json`, `enriched.json`). The finished tree under `<component>/<fn>.json` is not in that list; the executor binds it separately by hashing every node file at load (`hashNodes`, `Plan.Hashes["tree/<path>"]`) and re-checking before every commit (`planIntact`). `LoadPlan` and `planIntact` also call `planner.VerifyApproval`.

The rule (E3):

1. `approval.json` and `RenderPlan` are unchanged. A revision writes no file in `hashedFiles`, so `VerifyApproval` keeps passing and keeps meaning "this is the plan a person approved". Revision 0 is that plan.
2. Each revised node has `_state/revisions/<fn>/` with `body-<k>.json` (the node file as it was at revision k, `body-0.json` written before the first rewrite) and `log.jsonl` (section 9). Hashes are SHA-256 of the file bytes, the same function as `hashHex`.
3. The chain: record 1 has `base_sha256` equal to the hash of `body-0.json`; record k has `base_sha256` equal to record k-1's `body_sha256`; every `body-<k>.json` hashes to its record's `body_sha256`; the node file on disk hashes to the last record's `body_sha256`; `leaf_tests.json` and the test file on disk hash to the last record's `test_sha256_after`; the records are consecutive from 1.
4. Invariants across the chain: `body-0.json` and the tip agree on every frozen field of section 7, and the tip still matches `contracts.json` (the existing check in `Plan.leaf`).
5. Executor changes: `LoadPlan` first rolls forward an unfinished revision (section 10), then verifies the chain for each node that has a log, then sets `Plan.Hashes["tree/<path>"]` to the hash of `body-0.json` (revision 0) and records the chain tip in a new `Plan.Revised` map. `State.PlanHashes` in `_state/executor.json` therefore never changes in a run and the resume comparison (`reflect.DeepEqual(rc.state.PlanHashes, rc.plan.Hashes)`) still holds. `planIntact` compares a revised node's file to the tip hash and an unrevised node's file to `Plan.Hashes`. The revise stage updates the tip in memory only after it has appended the commit record.
6. A failed chain check is the existing `plan_changed` stop (failed run, fixed message, no file content in the message).

Rejected alternatives. Putting `tree/` into the plan hash makes every revision invalidate the approval, so each one needs a person and an unattended run could never revise. Re-approving each revised body at a gate is safe but has the same effect and trains approvers to click through. Trusting `Plan.Hashes` while allowing the executor to rewrite it loses tamper evidence: a hand edit during a run would be accepted as a revision. The chain keeps R16's guarantee (an edit that is not a recorded, valid revision stops the run) and still lets a bounded, rule-checked rewrite through. Its limit: someone who can write the run folder can forge a whole chain, as they could forge `approval.json`; the chain protects against accidents, drift and a confused harness, not against the user.

## 9. Recording

Run folder additions (A's layout otherwise unchanged; node files and runtime files sit beside each other as in A):

```
<run>/
  <component>/<fn>.json                    plan body, rewritten in place at each revision; "revision": n
  <component>/<fn>.runtime.json            "revision": n (Board.SetRevision), attempts carry their revision
  _state/revisions/<fn>/body-<k>.json      the node file at revision k (0 = as approved)
  _state/revisions/<fn>/test-<k>.txt       the test file source at revision k (.txt so no tool compiles it)
  _state/revisions/<fn>/log.jsonl          one JSON line per event, below
  _state/revisions/decisions.json          revision decision store (human decisions about revised or flagged leaves)
  decisions/rv-<fn>-<m>.md                 browsable decision record, same format as decisionMarkdown
  _state/replies/revise_<fn>-<n>.txt       the revise reply (saveReply; the Test-writer reply saves as testwrite_<fn>_r<n>-<n>.txt)
```

`log.jsonl` line kinds: `intent` and `commit` (both carry `n`, `trigger`, `base_sha256`, `body_sha256`, `test_sha256_before`, `test_sha256_after`, `changed` as a list of field names, `assumptions`, `model`, `at`), `flag` (`class`, `at`; no body change and no revision), `refused` (`class`, `at`; a rejected reply or test, so the report can count them). `trigger` is `ladder_exhausted` or `contract_problem`; the CONTRACT_PROBLEM sentence is not written.

The node's `revision` counter is the same number in three places: the node file's `revision` (definition revision, schema field), the runtime file's `revision` (`blackboard.Row.Revision`), and the `n` of the last `commit` record. The invariant in `rewrite` mode is that all three are equal after the stage finishes; the order of writes (section 10) makes the record the authority, and recovery raises the other two to it. In `notes` mode the runtime counter alone moves, as today.

Decisions where a human decides (E5). After `Gate.Escalate` answers a leaf whose reason is `contract_flagged`, `split_required`, a `revise_*` class, or any escalation after at least one rewrite, the executor calls `Reviser.RecordDecision`. The planner appends a record to `_state/revisions/decisions.json` using the shape of `recordOf` (id `rv-<fn>-<m>`, `question`: a fixed sentence naming the leaf, the revision and the class, `kind: decision`, `answer`: action plus note, `answered_by` from `human.ValidAnsweredBy`, `round`, `raised_by: revise:<fn>`, `settled_at`) and writes `decisions/rv-<fn>-<m>.md` with `decisionMarkdown`. The note is scrubbed first (`rc.scrubText`). The root's `decisions` and each node's embedded `decisions` stay equal to the question store, so the B check `checkEmbedded` and the GOAL check "the root's `decisions` equals the question store" keep passing; the node views in the run view (G) list revision decisions from this store.

Events. The blackboard FS gains event kind `revision` (emitted by `SetRevision`, a line in `_state/events.jsonl`). The executor's sink gets `revise_started`, `node_revised` (message `revision <n>: <changed fields>`), `contract_flagged`, `revise_refused` (message: the class). Report: `RevisionEscalations` already counts `Escalation{Kind: "revision"}` records (`report.go`); E adds to the leaf section per node `revisions`, the `changed` fields, the flag class, the refusal classes, and a line `Rewritten leaves: N` in the summary. The existing escalation append (`AppendEscalation`) is called once per committed rewrite and is idempotent by its key.

## 10. Order of writes, resume and idempotence

A revision is one transaction made of idempotent steps, each keyed by hashes, so a kill at any point either leaves the old node or can be rolled forward from staged files. Steps for revision n of leaf `fn`:

1. Validate (section 6). Run the Test-writer if needed, still in memory. The red check needs the real test path, so it runs in step 4 after the new test is placed.
2. Stage, create-only: `body-<n-1>.json` and `test-<n-1>.txt` if absent (for n = 1 these are revision 0); `body-<n>.json`, `test-<n>.txt`.
3. Append the `intent` record (after this point recovery rolls forward, never back).
4. Place the new test file: write to a temp file in the same directory (O_EXCL, no-follow, `pathsafe.ResolveTest` before and after, `pathsafe.InsideRepo` on the directory, as `placeTestFile` does), proving the current file hashes to `test_sha256_before` (a changed file is `test_file_changed`, the existing stop) and renaming over it. Run the red check; on a rejection restore the old file from `test-<n-1>.txt`, append a `refused` record and return the outcome.
5. Rewrite the `leaf_tests.json` entry (`LeafTest.SHA256`) atomically (`runfs.WriteFileAtomic`).
6. Rename `body-<n>.json` over the node file (via `tree.Store.Write`, which validates and writes atomically).
7. Append the `commit` record. Raise the runtime revision (`Board.SetRevision`). Commit the repo change (the test file) on the work branch with message `gm(<run>): revise <fn> test (revision n)`, using the same git layer and dirty-path allowance as Wave 0 (`wave0Paths`, `allowDirty`); in `diff_only` mode the commit is skipped like every other (`swap.NoCommit`).
8. Reload the leaf: `Plan.ReloadLeaf(id)` re-reads the node, `leaf_tests.json` and rebuilds the `Leaf` (description, contract text, constraints, `TestSHA256`, the guidance groups); `lr` gets the new `Leaf`, clears `lr.cp`, `lr.lastSHA`, `lr.charged` and `lr.prev` for the new revision.

Resume (`executor.resume` and `LoadPlan`): `RecoverRevisions` scans `_state/revisions/*/log.jsonl`; an `intent` with no matching `commit` is rolled forward through steps 4 to 7, each step skipped if its target already has the "after" hash. If a staged file is missing, the run stops `plan_changed` (harness fault, nothing modified). A `commit` record with a stale runtime revision only needs `SetRevision`. The repo commit of step 7 is re-derived: if the test file is dirty with the recorded "after" hash it is committed; if already committed nothing is done. No model call happens during recovery. A revise call made before step 3 and lost in a kill is simply made again; it costs one call and the revision counter did not move, so `max_revisions` is not consumed.

## 11. Bounds and cost

- Rewrites per leaf: `rc.maxRevisions(l)`: node `budget.max_revisions` if set, else `defaults.max_revisions` (default 2), plus `State.ExtraRevisions[id]` granted by a human retry. After that the leaf escalates with the last attempt's class (`lastClass`).
- Model calls for one leaf, worst case: the executor spec's `entries * (1 + fix_attempts) * (max_revisions + 1)` implement calls, plus per revision one revise call, at most one router repair for it, and (when tests change) one Test-writer call with its repair. With the GOAL configuration (one entry, 2 fixes, 2 revisions) that is 9 implement calls, 2 revise calls and 2 Test-writer calls, plus repairs.
- Revise and Test-writer calls are bounded by `defaults.call_timeout` and `revise.max_tokens`; they appear in the ledger (`calls.jsonl`) under task types `revise` and `testwrite`, so the report's per-model table counts them.
- A refused reply (validation, red check) does not consume a revision, but a second refusal for one leaf in one revision escalates (`revise_invalid`), so the loop cannot spin: at most 2 revise calls per revision.

## 12. Settings

New top-level section `revise:` of `gophermind.yaml` in `settings` (a new `ReviseConfig` beside `ExecutorConfig`; an absent section gets the defaults, as `applyExecutorDefaults` does). `defaults.max_revisions` is unchanged.

| Key | Default | Validation (message names the key only) |
|---|---|---|
| `revise.mode` | `rewrite` | `rewrite` or `notes` |
| `revise.model_tier` | `strong` | `strong` or `standard` |
| `revise.context` | `node` | `node` or `component` |
| `revise.rewrite_tests` | `on` | `on` or `off` |
| `revise.max_tokens` | 4096 | 256 to 32768 |
| `revise.max_refusals` | 2 | 1 to 5 |

Touched elsewhere: A (the FS backend emits the `revision` event and its `Watch` forwards it); B (`packer.NodeView` gains `Description` and `ContractText`, and the implement prompt a `behaviour` section, see below); G (`runview` adds the `node-revised` event, the `revisions` field of `NodeSummary`, and lists revision decisions in `/pipeline/node`); `human.Escalation` gains `Revisions int` and `Proposal string` (the scrubbed CONTRACT_CHANGE_REQUIRED paragraph) for the gate text.

The executor consumes the rewrite only if the implement prompt carries it. Today `packer.NodeView` holds the signature, dependency signatures, constraints, test and, after B Task 12, guidance sections; it does not hold `description` or the contract's inputs, outputs and errors. E adds a `behaviour` section (description, input and output descriptions, error conditions, side effects) to the implement prompt for a node whose revision is 1 or more, dropped before the guidance sections when the budget is tight. For unrevised nodes the prompt is byte-identical to B's, so the graded run's baseline does not move (open question 1).

## 13. Failure modes and the report

| Condition | Outcome | Reason class | Report shows |
|---|---|---|---|
| Reply is not valid JSON, has an unknown key, or fails a section 6 check after the router repair | leaf to human gate | `revise_invalid` | refusal class counts per leaf |
| Rewrite equals the current node | same | `revise_noop` | same |
| Reply holds a secret value | same, reply not saved | `secret_in_reply` | same |
| Model asks for a signature or `depends_on` change, or inputs no longer match the signature | leaf to human gate with the proposal | `contract_flagged` | class, plus the decision record when a person answers |
| Model asks to split | same | `split_required` | same |
| Tests need rewriting and `revise.rewrite_tests` is off | same | `tests_locked` | same |
| New test passes the stub, or fails to build | rewrite refused, files restored | `revise_vacuous_test`, `revise_red_build` | same |
| Test file changed on disk since planning | run stops failed | `test_file_changed` (existing) | stop reason |
| Chain, hash or staged file mismatch | run stops failed | `plan_changed` (existing) | stop reason |
| No eligible provider, cooling, auth | claim released, nothing charged | `provider_unavailable` or the configuration class | interrupted, as now |
| `max_revisions` used up and still failing | leaf to human gate | last attempt class | `revisions: n` on the leaf |

A leaf that verifies after a rewrite is reported verified with `revisions: n`. The report never prints a prompt, a reply, a node body or a test body, only ids, classes, counts, field names and hashes.

## 14. Tests

Offline, with the executor test rig (`executor/rig_test.go`) and the planner harness fakes. A scripted fake `Caller` answers by stage name, in order.

| ID | Test | Proves |
|---|---|---|
| R0 (acceptance) | `TestReviseRewritesAndLeafVerifies`: every implement call on every ladder entry returns code that fails the test; the first `revise:<fn>` call returns a valid patch that fixes the contradiction (and `rewrite_tests`); the Test-writer returns a consistent test; the next implement call returns correct code | the leaf ends `verified` at revision 1; ladder restarted; the runtime and node `revision` are 1; the log has `intent` and `commit` |
| R1 | Same run: the first implement prompt after the revision contains the revised description sentence and the new test source | the v1 failure (a revision that changes nothing that runs) cannot return |
| R0b (acceptance) | `TestLeafNeverVerifiesEscalatesAfterMaxRevisions`: all calls fail, the revise stage rewrites each time; `max_revisions: 2` | exactly 2 revise calls, then `escalate`, gate called once, leaf `escalated`, reason is the last class |
| R2 | Approval: after a revision `planner.VerifyApproval` still passes and no file of `hashedFiles` changed; `RenderPlan` hash identical before and after | E3 point 1 |
| R3 | Approval rule: edit a revised node by hand; edit `body-0.json`; delete a log line; swap `log.jsonl` order; change `leaf_tests.json` | each gives `plan_changed` at the next load and at `planIntact` |
| R4 | Frozen fields: a table of patches that try to change each field of section 7's "may not" column (via a hand-built merge input) | refused, files untouched |
| R5 | Inputs not matching the signature | outcome `contract_flagged`, no revision consumed, escalation carries the proposal |
| R6 | Validation: a patch that makes `groupDefects` non-empty; a `not_applicable` where forbidden; open question added | refused with the defect word, no reply text in the message (canary) |
| R7 | Test rewrite: weaker tests (no negative test for an error); test passing the stub; test that does not build | refused, old test file restored byte for byte, runtime and node unchanged |
| R8 | Kill after each of the 8 steps (a hook in the stage): rerun | converges to the same final files and hashes as an uninterrupted run; no second revise call after step 3; revision count not doubled |
| R9 | Bounds: refusals twice in one revision; `max_revisions: 0`; human retry grants one more | `revise_invalid` escalation; no revise call at 0; one extra revision after retry |
| R10 | Privacy: a recording `Caller` shows the node-scope prompt holds none of `rationale`, `alternatives`, `refactor_notes`, `assumptions`, `decisions` text (canaries in each) and no brief text; the component variant under `need_to_know` with only a public provider fails with the privacy reason and no call | E8 |
| R11 | Secrets: a canary secret in a failing test name and in the last failure text is absent from the prompt, the saved reply, the log, the decision record, the report; a canary in the reply is refused as `secret_in_reply` and not saved | section 5 and 6 |
| R12 | Paths: a patch cannot name a path; a symlinked test file, a test directory swapped for a link, and a node id with a separator are refused (`pathsafe`, `idPattern`) | path safety |
| R13 | Human decision: gate answers retry with a note after `contract_flagged`; skip; stop | `rv-<fn>-1.md` and the store record exist with `answered_by`; root and node `decisions` unchanged; `checkEmbedded` clean |
| R14 | `notes` mode | today's `TestLadderOrderAndEscalation` and the notes tests pass unchanged |
| R15 | Report: counts of rewrites, flags, refusals; `revision_escalations` equals the number of committed rewrites | section 9 |

Static gates: `go vet`, `gofmt -l`, no SQL, no `rm -rf` of a computed path, no model call in `RecoverRevisions`.

## 15. Risks

- A rewrite that makes the test easier. Mitigations: the Test-writer writes from the revised contract only, the coverage and polarity rules still apply, and the report names every leaf with a rewritten test so a person can read `test-0.txt` against `test-n.txt`. A stricter option (a human approves any test rewrite) is the setting `revise.rewrite_tests: off`.
- The revise model drifts from the brief because it cannot see it at node scope. Mitigation: `revise.context: component` under a private provider.
- A bad revision wastes mini time. Bounded by section 11.
- `testwriter.json` keeps the revision 0 tests. It is not read after `PlannedAt` is set, and the revision log is the authority for later tests.

## 16. Open questions

1. Should the `behaviour` prompt section be added for every node, not only revised ones? Recommended answer: no, not until the graded run has produced a clean baseline; revisit afterwards with measured token cost.
2. Should `SPLIT_REQUIRED` with a proposed list of functions be turned into new nodes? Recommended answer: no, not in E; it changes waves, coverage and the approval, and belongs to a later contract-change sub-project.
3. Should the human be told at approval that revisions are allowed? Recommended answer: yes in the approval prompt of `/project`, outside `RenderPlan` (changing its text would invalidate existing approvals).

## 17. Order of work

1. Pure pieces with tests: patch parser and merge, frozen-field check, input-name check, chain writer and verifier (R3, R4, R5, R6).
2. Settings and `ReviseConfig` (validation tests).
3. `planner.ReviseNode` and the Test-writer reuse with fakes (R7, R10, R11, R12).
4. Executor: `Reviser` seam, `Plan.ReloadLeaf`, `Plan.Revised`, `RecoverRevisions`, `planIntact` (R2, R8).
5. `leafRun.revise` rewrite mode and the `behaviour` prompt section (R0, R0b, R1, R9, R14).
6. Decisions store, events, report (R13, R15).
7. Docs: executor spec "Not in scope" and R16 amended, umbrella spec E row marked built.
