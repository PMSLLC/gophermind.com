# Node Widening and the Question Model: Design (sub-project B)

Status: draft for review. Date: 2026-10-07.
Scope: sub-project B of `2026-10-07-planner-phaseflow-unification-design.md`. Two parts: (1) every plan node carries everything needed to build, debug and analyse it; (2) the planner asks its questions the way the `grilling` skill does, as a tree answered in rounds.
Builds on: `docs/superpowers/specs/2026-09-29-v2-planner-core-design.md` (planner), `docs/superpowers/specs/2026-09-30-v2-executor-design.md` (executor), `docs/superpowers/specs/2026-09-30-project-single-entry-design.md` (the unattended policy, section 8), and the two reviews in `docs/reviews/2026-10-07-*.md`. All three specs live on branch `feat/briefv2-planner-core` (worktree `~/OtherProjects/PMSLLC/gophermind-planner-core`).
Sequencing: written, not started. Decision 11 of the unification spec holds: nothing here changes the planner, the executor or the node schema until the GOAL.md graded run has finished. B is built after sub-project A (file state), because it writes decision records and attempt data into the run folder.

## 1. Why this exists

John's goal is a brief fully decomposed into granular nodes, each carrying construction, error handling, model-performance tracking, performance options, portability, security, success and failing tests, typed inputs and outputs, alternate approaches, logging, refactoring opportunities and anything else that helps profiling, debugging and analysis. The review found that v2 captures typed inputs, error conditions, tests and dependencies well, and captures security, performance, portability, logging, alternatives and refactoring not at all. The node schema is `additionalProperties: false`, so none of them can even be stored today.

The same review found that neither planner asks questions well: v1 shows everything at once, v2 asks one flat round, and neither separates facts from decisions, orders questions by dependency, or confirms a shared understanding before it builds. The `grilling` skill describes the protocol that fixes this.

## 2. Decisions

| # | Question | Decision | Source |
|---|---|---|---|
| 1 | Clarify as rounds with a dependency tree | Yes | John, 2026-10-07 |
| 2 | A confirm-understanding gate whose hash goes into approval | Yes | John, 2026-10-07 |
| 3 | `decisions/<id>.md` records | Yes | John, 2026-10-07 |
| 4 | Round caps | 4 model calls and 30 questions per run (settings), as proposed | John, 2026-10-07 |
| 5 | Which node groups are required | All groups on every node (unification spec, decision 8). Read as: every group is present on every node, and a group that truly does not apply is an explicit, validated "not applicable" answer. Open item 1 | John, 2026-10-07 |
| 6 | Attempt artifacts | Saved, scrubbed (sub-project A) | John, 2026-10-07 |
| 7 | Where the new groups are written | A new planner stage, Enrich, between Decompose and Coverage (section 4) | this document |
| 8 | Changing an answer | Restarts the planner from the stage that first used it; no selective node re-plan in this version (section 7.7) | this document |
| 9 | Questions and answers in the tree | Every question and its answer is also recorded in the node tree itself: the root node carries the whole conversation, and each component and function node carries the records that shaped it (section 7.8) | John, 2026-10-08 |
| 10 | Not-applicable answers; node-tier routing | Both yes | John, 2026-10-08 |

## 3. Goals and non-goals

Goals:

- Every function node carries every group in section 5, each either filled and validated or marked not applicable with a stated reason that code checks.
- Code derives or checks everything it can (types from the signature, secret names from the brief, decision ids from the question store, requirement ids from the coverage map), and a model writes only what only a model can write.
- Clarify is a loop of rounds ordered by a dependency tree; facts are answered by code; decisions are answered by a person or, unattended, by their recorded recommendation.
- No stage runs on an understanding the owner has not confirmed (or the unattended rule has not confirmed, recorded as such).
- Every settled question leaves a browsable record and a trace to the nodes it shaped.
- The planner still runs offline in tests against canned replies, and an unattended run still never blocks (project-single-entry spec, section 8).

Non-goals:

- Selective re-planning of only the nodes an answer touched (section 7.7).
- Making the executor verify log events, security mitigations or performance budgets in code. The executor receives these groups in its prompt (section 6); verification of them is a later change.
- Report changes (node-class dimension, first-pass rate by class): sub-project C.
- Changing the Contract stage's output or `contracts.json`.
- Free-form answers that are not text: an answer is text.

## 4. Pipeline change

```text
before:  load, clarify, contract, decompose, coverage, approve, testwriter
after:   load, clarify (rounds), confirm, contract, decompose, enrich, coverage, approve, testwriter
```

`confirm` and `enrich` are new stages, each with a `done` predicate like every other stage so a resume repeats only unfinished work.

**Why Enrich is its own stage and not part of Decompose.** Decompose writes 8 function nodes per call today. The groups multiply each node's size several times; one call that writes all of it would hit the output limit on a slow local model and would give one repair loop for two different kinds of defect. A separate stage lets the batch size differ (default 4, a setting), lets a truncated reply halve its batch and retry instead of failing, keeps Decompose's checks and prompt untouched, and lets the two stages be compared in the ledger by task type (`decompose` and `enrich`).

**Scope and tier.** Enrich carries the brief section, so its scope is `component`: private providers only under `need_to_know`, as for Decompose. Today every planner call uses the strong tier (`planner/planner.go:203`) and ignores the node's `model_tier`. This document changes that for stages that work on one node: Enrich and Test-writer use the node's `model_tier` (`strong`, `standard` or `any`) to pick the chain; Clarify, Contract, Decompose and Coverage stay strong. Open item 2 covers the risk.

## 5. Part 1: the widened node

The schema is `gophermind-lib/briefv2/schema/task-node.schema.json`. Every addition below is added to the schema (as optional properties with `additionalProperties: false` kept), to the Enrich or Test-writer prompt output list, and to the checks of section 5.3. Plan fields live in the node file and are covered by the plan hash (Enrich writes into `_state/decomposed.json`, which the hash already includes). Fields written after approval (test polarity and coverage, the error-to-test link) are written by Test-writer, as tests are today.

### 5.1 Function node groups

| Group | Shape | Written by | Required |
|---|---|---|---|
| `node_class` | enum: pure, validation, handler, client, storage, concurrency, wiring, other | Decompose (model), moved from `_state/classes.json` onto the node | always |
| `rationale` | string, 20 to 600 characters: why this function exists | Enrich | always, never N/A |
| `requirement_ids` | array of `C#`, `A#`, `F#` ids | code, from `coverage.json` after the Coverage stage | always, at least one |
| `construction` | `{approach_chosen: string, steps: [string]}`, at least 2 steps, each one sentence | Enrich | always, never N/A |
| `alternatives` | array of `{approach, rejected_because}` | Enrich | always; N/A allowed |
| `contract.inputs[]`, `outputs[]` | existing `{name, type, description, constraints}` plus `go_type` | `go_type` derived by code from the signature | always |
| `contract.errors[]` | existing `{when, returns}` plus `kind` (enum: sentinel, wrapped, typed, panic, http_status, exit_code, other) and `test` (name of the test that covers it) | Decompose writes `when`, `returns`, `kind`; Test-writer writes `test` | always when the function can fail |
| `tests[]` | existing fields plus `polarity` (enum: success, negative, boundary, property) and `covers` (`happy`, `error:<n>`, `input:<name>`) | Test-writer | always |
| `portability` | `{os: [string], arch: [string], go_min: string, cgo: bool, build_tags: [string], deps: [string]}` | Enrich, then checked against `go.mod` and the imports | always; N/A allowed |
| `security` | `{trust_boundary: none, internal, external_input, external_output or both; untrusted_inputs: [input name]; authz: string; secret_use: [secret name]; threats: [{threat, mitigation}]}` | Enrich | always; N/A not allowed for handler, client, storage, concurrency |
| `performance` | `{complexity: string, max_latency_ms: integer or null, alloc_budget: string, concurrency: none, safe_for_concurrent_use or single_goroutine; hot_path: bool}` | Enrich | always; N/A allowed for pure |
| `observability` | `{log_events: [{level, msg, fields}], metrics: [{name, kind}], trace_span: string or null}` | Enrich | always; N/A not allowed for any class except pure |
| `refactor_notes` | array of `{what, why, when}` | Enrich | always; N/A allowed |
| `profile_hooks` | array of enum bench, pprof, trace | Enrich | always; N/A allowed |
| `assumptions` | array of strings | Enrich (and, in assume mode, Decompose, as the prompt already says) | always (an empty array is a valid answer) |
| `open_questions` | array of strings | Enrich | always; must be empty at approval |
| `decision_ids` | array of question ids | Enrich, validated against the question store | always (an empty array is a valid answer) |
| `decisions` | array of full question and answer records (section 7.8) | code, copied from the question store, never model-written | always on the root (the whole conversation); on components and functions, exactly the records named in `decision_ids` |

Existing fields stay as they are: `title`, `description`, `brief_ref`, `status`, `wave`, `model_tier`, `revision`, `depends_on`, `context`, `network`, `secrets`, `budget`, plus the run fields `claim`, `attempts`, `result` (sub-project A writes those into `.runtime.json`).

**Not applicable.** A group that allows it accepts `{"not_applicable": "<reason>"}` instead of its normal shape. The reason must be at least 20 characters and may not be one of a fixed list of non-answers (`none`, `n/a`, `not applicable`, `no`, `nothing`, `pure function`, compared case-insensitively after trimming). Code rejects a not-applicable answer in a place the table forbids. This is how decision 5 stays enforceable: a missing group is a defect, a not-applicable group is a visible claim that appears in the approval summary.

### 5.2 Component and root nodes

Components and the root get `rationale`, `requirement_ids`, `assumptions`, `open_questions`, `decision_ids` and `decisions`, with the same rules. The component's `description` is today copied from the brief's first paragraph (`decompose.go:741-748`); Enrich writes a real `rationale` for each component in one extra call per component (batch of one, small output). The leaf-defect checks of the next section run on them as well.

### 5.3 What code checks (defect kinds)

Each is a fixed word in the same vocabulary the Decompose repair loop already uses, and none quotes the reply.

| Kind | Check |
|---|---|
| `group_missing` | a required group is absent |
| `na_forbidden` | not applicable given where the table forbids it |
| `na_reason` | the reason is under 20 characters or is a listed non-answer |
| `enum` | any enum value outside its set |
| `construction_short` | fewer than 2 steps, or an empty step |
| `go_type_unresolved` | a type named in a signature's parameters or results is neither a Go builtin, nor a standard library type, nor a type declared in `contracts.json` (checked on `go_type`, which code derives). A warning in the approval summary, not a failure: the Enrich model cannot repair the Contract |
| `security_secret` | `secret_use` names something that is not a declared secret of the brief, or `untrusted_inputs` names something that is not a parameter |
| `security_threats` | `trust_boundary` is anything but `none` and `threats` is empty |
| `portability_go` | `go_min` is newer than the repo's `go.mod` Go version |
| `portability_deps` | `deps` lists a module that is not in `dependencies.json` and not the standard library |
| `observability_fields` | a log field name is a declared secret name, or `level` is not debug, info, warn or error, or `msg` contains a format verb |
| `decision_unknown` | `decision_ids` names a question that is not in the question store |
| `open_questions_left` | `open_questions` is not empty (checked at approve) |
| `hook_bench` | `profile_hooks` has `bench` and the Test-writer's file has no `Benchmark` function (checked in `checkTestSource`) |
| `error_test_missing` | an `errors[].test` names a test that is not in `tests` |
| `polarity_missing` | `errors` has n entries and `tests` has fewer than n tests with polarity `negative`, or no `success` test |
| `requirement_missing` | a function node has no `requirement_ids` after Coverage. A warning in the approval summary, not a failure: a shared helper that no requirement names is legitimate |
| `decisions_embed` | a node's embedded `decisions` ids differ from its `decision_ids`, or the root's `decisions` differ from the question store (checked at tree finish; code writes them, so this guards a bug or a hand edit) |

`go_min` and the dependency list are read from the target repository's `go.mod`, which the Load stage already resolves; if the repo has no `go.mod` yet, the checks that need it are skipped and the skipped check is named in the approval summary.

### 5.4 Test-writer changes

The prompt asks for `polarity` and `covers` on every test. Code checks that every `errors[]` entry is covered by a `negative` test whose `covers` is `error:<n>` and then writes `errors[n].test` itself from that mapping; the model does not write the link. A `bench` hook adds "include `Benchmark<TestFuncName>` in the file" to the prompt, and `checkTestSource` checks for it.

## 6. Consumers of the new groups

- **Approval summary.** `RenderPlan` gains, per component, a count of nodes by security trust boundary, the number of not-applicable answers per group, every assumption, and the open questions (which must be none). A person approving sees where the plan says "not applicable" and why.
- **Implement prompt.** The packer's leaf prompt gains a section built from `construction`, `security` (untrusted inputs and threats with their mitigations), `observability.log_events` (the leaf must emit these), `performance` and `portability`. `decisions`, `rationale`, `alternatives`, `refactor_notes`, `assumptions`, `open_questions` and `decision_ids` are never put in the prompt (they carry brief-derived reasoning; node scope may be shown to a public provider). The Test-writer prompt receives the same kind of projection: the contract, the dependency signatures and constraints, and `profile_hooks`. This is the only executor-side change, it stays inside the leaf's token budget (`budget.max_context_tokens`), and sections are dropped from the bottom (refactor notes, alternatives, performance) when the budget is tight. Verification that the implementation honours them is a non-goal.
- **Analysis.** Nothing consumes `alternatives`, `refactor_notes`, `profile_hooks` or `rationale` in code; they exist so a person (and a later tool) can read a run folder and see why a node is what it is.

## 7. Part 2: the question model

### 7.1 One question type

`human.Question` and the planner's stored record gain fields. The planner stores each question in `_state/clarify/questions.json`:

| Field | Meaning |
|---|---|
| `id` | `q1`, `q2`, ... unique for the run; mid-stage questions get `<stage>-q<n>` as today |
| `text`, `why_it_matters` | as v2 today |
| `kind`, `fact_key` | `decision` or `fact`; a fact carries a key from the closed list of 7.2 |
| `depends_on[]` | ids of questions that must be settled first |
| `options[]` | for a decision: 2 to 8 option labels, or empty for free text |
| `recommended`, `recommended_why` | the recommended answer, which must equal an option label when options exist; required when the run is unattended (replaces v2's `default_if_unanswered`, which the parser still accepts as an alias for saved states) |
| `raised_by` | `clarify`, or the stage and node that asked (`decompose:comp`, `enrich:fn-a`) |
| `round` | the round it was asked in (0 for a mid-stage question) |
| `status` | `open`, `settled` |
| `answer`, `answered_by`, `settled_at` | filled when it settles; `answered_by` is `human`, `accepted` (typed `accept` to take the recommendation), `unattended-default`, or `probe` (a fact answered by code) |

### 7.2 The fact probe

A deterministic step before round 1, `planner/factprobe.go`, writes `_state/clarify/facts.json` and puts it in the Clarify prompt as a "Known facts" block. It reads: whether the target repo exists and is a git repository, the `go.mod` module path and Go version, the package directories that exist and their exported identifier names, the operating system and architecture, the names of declared secrets (never a value; Load has already refused a run whose secrets are missing from the vault), and the hosts the brief lists. It runs no command and no network call (the planner makes model calls only), reads `go.mod`, directory listings and the repo's Go source files (the names of exported declarations only, never bodies, comments or string literals), and never reads a secret value. The host operating system and architecture come from the Go runtime; the target toolchain version is not probed. A `fact` question must carry a `fact_key` from a closed list (`go_module`, `go_version`, `repo_is_git`, `os`, `arch`, `packages`, `secret_names`, `hosts`); the probe answers it from `facts.json` with `answered_by: probe` and logs it. A fact question with no valid key, or one the probe cannot answer, is shown to the person as a decision.

### 7.3 Rounds

1. Round 1 is one model call with the brief and the facts. The reply is the full question list (JSON array) with `depends_on`.
2. Code validates it (section 7.4) and computes the **frontier**: open questions whose prerequisites are all settled. Only the frontier is put to the gate, numbered, each with its recommendation.
3. After the answers, code marks them settled, writes their decision records, and recomputes the frontier. Questions that were waiting on them now show.
4. When the frontier is empty and every question is settled, one more model call asks: "given these settled decisions, what new questions are unblocked or raised?" It returns `[]` when there are none. Stop when it does, or when a cap is reached.
5. Caps (settings in `defaults`): `clarify_max_calls` (default 4, counting round 1) and `clarify_max_questions` (default 30, counting every question ever asked). At a cap, every still-open question is treated as the frontier of one last round, ignoring `depends_on`, so no question is ever left open. A reply that would take the total past the cap is rejected as malformed with the instruction to ask only the most important questions, and goes through the router's normal retry.

Mid-stage `QUESTION:` (Contract, Decompose, Enrich, Test-writer) keeps its pause-and-resume and its limit of 3 per call. Its question is added to the store with `raised_by` set and `depends_on` empty, answered through the same gate, and gets a decision record. The planner's `answers.json` stays, rewritten by code from the store after every settle (it holds every settled question, mid-stage ones included), so the prompts that read it (`answersText`) and the approval hash that includes it are unchanged.

### 7.4 What code validates in a reply

Ids are unique and match `q<number>`; `depends_on` entries exist, appear earlier in the array, and form no cycle; `kind` is `decision` or `fact`; options (when present) number 2 to 8 and are distinct; `recommended` equals an option label when options exist; the total is within the cap. Question and recommendation text is bounded (1000 bytes). A violation is a `malformed` reply and takes the router's retry; the error text names the rule and quotes nothing from the reply.

### 7.5 Answering

`human.Gate.Ask` already takes a list of questions and returns one answer per question. The terminal adapter renders a round as plain labelled blocks (no emoji):

```text
Q1  <title>
    <body, options listed 1..n>
    Recommended: <option> (<why>)
```

An answer is an option number, the word `accept` (take the recommendation; recorded `answered_by: accepted`), or free text. A blank answer is refused. The file adapter writes the round's questions to `QUESTIONS.md` (its existing file, with the round number in the header) and reads `QUESTIONS.answered.md`; the exit-3-and-resume behaviour is unchanged. The programmatic adapter takes the same structure as data. `human.Answer` gains `Accepted bool`; `Assumed` keeps meaning "no person was involved".

### 7.6 Decision records and the Understanding

When a question settles, code writes `decisions/<id>.md` in the run folder: the question, why it matters, options, the recommendation, the answer, who answered, the round, and the nodes whose `decision_ids` cite it (filled at approve time and refreshed whenever nodes change). Browsing `decisions/` shows the whole conversation.

The `confirm` stage renders `UNDERSTANDING.md` from the store: settled decisions with answers, assumptions taken (every `accepted` or `unattended-default` answer, labelled), the facts the probe established, and the sentence "Frontier empty: every branch visited". A new gate method, `Confirm(ctx, Understanding{Markdown, Hash}) (Decision, error)`, asks the person; every adapter and test fake gains it. The result is `_state/understanding.json` (`confirmed_at`, `confirmed_by`, `understanding_hash`; it lives in `_state/` because a root-level JSON file would be read as a node). Contract does not start without a matching confirmation, `approval.json` records `understanding_hash`, and the file is part of the plan hash, so an answer edited after confirmation invalidates the confirmation and the approval. The Understanding lists only Clarify-phase decisions: a question raised later by a stage is bound by the plan hash through `answers.json`, so answering one never makes the confirmation stale.

Unattended (project-single-entry spec, section 8.2): every question takes its `recommended`; a decision with no `recommended` makes the reply malformed, as that spec already requires; the rounds still run so dependent questions still appear and are answered by their recommendations; `Confirm` returns `Approved: true, By: "unattended"` only when no question is open and the last model call returned `[]` or a cap forced the final round. `--yes` records `flag`, as for approval. `on_ambiguity: assume_and_document` behaves as unattended for Clarify; `halt` asks the gate.

### 7.7 Changing an answer

`gophermind brief answer <run-id> <question-id> <text>` changes a settled answer. Because the first version cannot tell which parts of the contract an answer shaped, it is deliberately coarse and honest about it:

- A **Clarify** answer (round 1 or later) resets the run to the Contract stage: `understanding.json`, `contracts.json`, the drafts, the enrichment, `coverage.json` and `approval.json` are removed, the decision record is rewritten with the change noted, and the run resumes. Tests already written to the target repo make this refused (see below).
- A **mid-stage** answer redoes the one stage call that raised it (the component's Decompose call, the node's Enrich or Test-writer call), then Coverage and Approve again.
- Once Test-writer has written files into the repo, or the executor has started (`_state/executor.json` exists), the command refuses and says to start a new run.

Selective re-planning (only the nodes whose `decision_ids` cite the answer) needs each question's effect to be known by component, which Clarify cannot do before the Contract stage has named the components. `decision_ids` on nodes therefore serves traceability and analysis in this version, not re-planning. This refines section 4.5 of the grilling review.

### 7.8 Questions and answers in the tree

The question store and `decisions/` are not the only record: every question and its answer is also written into the node tree, so reading the nodes alone tells the whole story.

- The **root node** carries `decisions`: one record for every question of the run, in order, settled or not at the time the tree is written (at tree finish all are settled). A record holds the id, the question, why it matters, kind (and `fact_key`), `depends_on`, options, the recommendation and its reason, the answer, who answered (`human`, `accepted`, `unattended-default` or `probe`), the round, what raised it (`clarify` or a stage), when it settled, and `history` (earlier answers, newest last, from `brief answer`).
- Each **component and function node** carries `decisions` too: copies of exactly the records its `decision_ids` name. A function that cites q2 and q5 shows those two questions and answers on the node, so the reason for a choice is on the node it shaped.
- Code writes these records, copying them from the store at the moment the tree is written (skeleton, rewrite and tree finish); a model never writes them. A check at tree finish fails the stage when a node's `decisions` ids differ from its `decision_ids`, or the root's `decisions` differ from the store.
- Node-scope calls never receive `decisions` (they carry brief-derived reasoning, like `rationale`).
- The records are plan data, covered by the plan hash like the rest of the node. Because `brief answer` is refused once tests are written, the embedded records cannot go stale after the tree is final.

## 8. Settings

Added to `defaults` in `gophermind.yaml` (the section that already holds `max_coverage_rounds`), validated like its neighbours (positive, bounded):

| Key | Default | Meaning |
|---|---|---|
| `clarify_max_calls` | 4 | model calls in the Clarify loop |
| `clarify_max_questions` | 30 | questions asked in a run |
| `enrich_batch_size` | 4 | function nodes per Enrich call; halves on a truncated reply down to 1 |
| `route_by_node_tier` | true | Enrich and Test-writer use the node's `model_tier` chain |

## 9. Files changed

```text
briefv2/schema/task-node.schema.json     the groups of 5.1 (optional properties; additionalProperties:false kept)
briefv2/planner/enrich.go, prompts/enrich.md, enrich_test.go     the Enrich stage, its repair loop, state _state/enriched.json
briefv2/planner/checks_groups.go          the defect kinds of 5.3 (pure functions, table-driven tests)
briefv2/planner/clarify.go                rewritten as the round loop; parse, frontier, caps
briefv2/planner/factprobe.go              the fact probe
briefv2/planner/questions.go              the store (_state/clarify/questions.json), settle, answers.json rewrite, decision records, embedding of decisions into nodes
briefv2/planner/confirm.go                the confirm stage, UNDERSTANDING.md, understanding.json
briefv2/planner/planner.go                stages list, tier routing for node stages, callAsking writes to the store
briefv2/planner/decompose.go              node_class onto the node; leaf checks also on component and root
briefv2/planner/coverage.go, testwriter.go requirement_ids written from the map; polarity, covers, errors[].test; bench check
briefv2/planner/approve.go                RenderPlan additions; understanding_hash in approval.json and VerifyApproval
briefv2/human/*.go                        Question and Answer fields; Gate.Confirm; terminal, file, programmatic adapters and fakes
briefv2/packer/packer.go                  the implement-prompt section of 6
briefv2/settings/settings.go              the four keys of 8
cmd/gophermind/brief_plan.go              `brief answer`; usage text
docs/briefv2/README.md                    new stages, questions, decisions/, settings
```

## 10. Tests (all offline, canned replies)

- **Schema:** each new group accepts a valid value and a valid not-applicable value and rejects an invalid enum, a short reason, and an unknown extra property.
- **Group checks** (table-driven, one row per defect kind of 5.3, plus the happy row), including `go_type` derivation from a signature with a pointer, a slice, a map, a generic and a package-qualified type.
- **Enrich:** a reply with all groups passes; a reply missing `security` on a handler goes to the repair loop with kind `group_missing` and only that node is asked again; a truncated reply halves the batch; a `QUESTION:` reply pauses and resumes one call; the stage is skipped on resume when `_state/enriched.json` covers every function.
- **Clarify rounds:** a three-level dependency chain asks level 1, then 2, then 3, in that order, with the right calls counted; the cap forces a final round of the remaining questions; a cycle, a forward reference, a duplicate id and an over-cap reply are each `malformed` and retried; a `fact` question the probe can answer is never shown to the gate.
- **Gate:** `accept` records `accepted`; a blank answer is refused; the file gate writes a round and resumes on the answered file; `Confirm` blocks Contract until confirmed; editing `answers.json` after confirmation invalidates `understanding.json` and `approval.json`.
- **Unattended:** a decision with no `recommended` is malformed; every question takes its recommendation, marked assumed, with dependent rounds still run; `Confirm` returns `unattended`; no call to the gate happens.
- **Decision records:** one file per settled question, rewritten on a changed answer; node `decision_ids` populate the "cited by" list.
- **Questions and answers in the tree:** after tree finish the root's `decisions` equal the store (every question, answer, who answered, round, history); a function citing q2 and q5 carries exactly those two records; a hand edit of a node's `decisions` makes tree finish fail with `decisions_embed`; a node-scope prompt never contains a `decisions` record (canary test).
- **`brief answer`:** a Clarify answer resets to Contract; a mid-stage answer redoes one call; both refuse after Test-writer or after the executor started.
- **Fact probe:** reads only what 7.2 lists (a canary secret in the vault and a canary string in a source file never appear in `facts.json`).
- **Packer:** the implement prompt includes the new section within the token budget and drops sections from the bottom when tight.
- **Golden:** the offline fixture's expected nodes (greeter) are updated to carry every group, and a size test records bytes per node before and after, so the cost is measured and visible.

## 11. Risks and cost

- **Planning gets slower and larger.** Nodes grow several times and each function costs an extra model call set (Enrich) on a slow local model. This is measured on the csvstat brief before the real one; `enrich_batch_size` is the lever. If Enrich on the real brief exceeds an acceptable time, the not-applicable path and tier routing are the other levers, and dropping a group is a schema decision for John.
- **"All groups" invites filler.** A model required to write seven groups per node will write plausible text. Code can check shape, references and consistency (section 5.3), not truth. The approval summary shows not-applicable counts and security counts so a person can sample.
- **Dependency graphs are model-written.** Code validates them (exist, earlier, acyclic) but cannot judge whether a dependency is real; a wrong one delays a question by a round and cannot drop it, because the stop rule needs an empty frontier.
- **Tier routing for node stages** could send security text to a weaker model than Decompose used. The checks bound the damage; the setting turns it off.
- **A coarse answer change** discards work (everything from Contract on). That is the cost of not guessing.

## 12. Deviations from earlier documents

| Document | Said | Here |
|---|---|---|
| grilling review, 4.5 | changing an answer re-plans only the nodes that cite it | coarse reset in this version (7.7); `decision_ids` for traceability |
| review, 5.1 | `contract.inputs[].type_ref` | no new field; `go_type` is derived by code from the signature |
| review, 5.1 | `requirement_ids` filled by Coverage | filled by code from `coverage.json` after Coverage, written into the nodes at tree finish |
| planner spec, 5 | planner stage list | two stages added, `confirm` and `enrich` |
| planner spec, 6 | all planner calls strong tier | node stages follow the node's tier (setting) |
| executor spec R15 | replies never stored | sub-project A |

## 13. Open items

1. **Meaning of "all groups everywhere" (decided, John 2026-10-08).** Explicit, validated not-applicable answers are allowed where 5.1 says so.
2. **Tier routing (decided, John 2026-10-08).** Enrich and Test-writer follow each node's `model_tier`; the setting `route_by_node_tier` turns it off.
3. **File gate for rounds.** The file adapter's exact file names and the header that carries the round number are settled in the implementation plan against `human/file.go`.
4. **Selective re-plan.** Revisit once components are known before Clarify's questions are asked (for example, a second Clarify pass after the Contract outline).
5. **Executor verification of the groups** (log events emitted, mitigations present): a later change with its own spec.
6. **Cost on the real brief.** The number of function nodes (136 in the rehearsal) times the Enrich cost has not been measured.
