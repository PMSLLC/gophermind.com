# Task Assignment Tree - Specification and Slice 1 Handoff (v4)

**Status:** Implementation specification. Build Slice 1 only.

**Supersedes:** `assignments-schema-spec-v3.md` as the implementation handoff. This is a standalone replacement; the implementer does not need the preceding conversation.

**Implementation language:** Go, following the actual repository's conventions and supported toolchain.

**Default operating mode:** Shadow. Do not change the live legacy assignments file, claim live work, or authorize production cutover.

## 1. Purpose, provenance, and decisions

Build a file-based planning tree that lets an agent work with a compact, current assignment packet instead of reconstructing an entire plan. The first release must make two-pass planning, questions, specification review, interruption, and resumption usable through an existing application entry point or a thin command adapter.

This document retains the original recursive hierarchy, 17 concrete leaf types, explicit acceptance requirements, and distinction between planning approval and post-execution review. It changes v3's delivery order and execution integration assumptions.

### 1.1 Source boundary

The supplied v2 and v3 documents establish the design baseline, not the current implementation. The subsequent feedback reports that the legacy executor claims whole tasks, uses task-level agent/model/wave settings, has `corrected` and task-level `contract_flagged` behavior, retains `revision_rounds`, and has seven flat-file consumers. Treat those as discovery leads until confirmed against the checkout. No checkout was inspected in preparing this document.

The choices below are normative v4 design decisions. In particular, the approval policy and the limited rollback protocol are new specifications, not claims that the repository already implements them.

### 1.2 Binding decisions

| Area | v4 decision |
| --- | --- |
| `reviewed` | Means the current specification has passed planning review. It never means merely read, and it is not unconditional permission to execute. |
| Planning progress | Track inspection, drafting, questions, review, and reconciliation separately from execution lifecycle status. |
| First useful release | Deliver a working planning/resume path, not only structs and storage interfaces. |
| Scheduling ownership | Retain task-level scheduling and ownership initially. Leaves are units of specification, context, verification, and internal progress, not independently dispatched jobs. |
| Routine specification approval | An authorized separate review-agent session, or an authorized human. An authoring agent session cannot approve its own specification. |
| Reserved authority | Governing product decisions, changes to approved scope, and explicitly human-controlled decisions require an authorized human. Human-gate completion remains a separate future operation. |
| Naming | Rename requirements-version fields to `spec_revision`. Keep legacy `is_contract` and `contract_flagged` meanings distinct. |
| Authority | Canonical planning documents are authoritative. Indexes, summaries, packets, and previews are derived. |
| Persistence | One local writer across processes, optimistic concurrency, atomic per-file replacement, a commit manifest, and one pending rollback slot. No general transaction-history service or durable receipt archive in Slice 1. |
| Performance | Rewrite changed canonical documents and affected derived views only. Benchmark during repository implementation, not at final handoff. |
| Legacy integration | Shadow mode only in Slice 1. Preserve the live file byte-for-byte and leave its executor path unchanged. |
| Scope discipline | Model later capabilities where required for planning, but do not quietly implement leaf dispatch, an executor rewrite, or the full v3 infrastructure. |

V2 already defined `reviewed` as specification authored and planning-reviewed. V4 preserves that meaning and fixes planning resumption by introducing its own progress records.

### 1.3 The two budgets

**Worker context budget:** Supply one focused packet containing the necessary requirements, current input contracts, applicable decisions, and resume information. Do not inject the whole plan or a previous long conversation by default.

**Coordinator storage budget:** Read the authoritative state required to construct and validate that packet. A compact agent packet does not require the coordinator to read only one file.

Do not truncate requirements to meet a context target. Report an oversized assignment and require decomposition or an explicitly larger budget.

## 2. Delivery slices and Slice 1 scope

### 2.1 Slice boundaries

| Slice | User-visible outcome | Included capabilities | Explicitly not implied |
| --- | --- | --- | --- |
| 1: Planning and resumption | Create, inspect, specify, question, review, stop, restart, and resume a tree using fresh packets. | Models, draft and approval validation, planning lifecycle, questions, effective spec freshness, minimal repository, packet preparation, a thin planner/command integration, compatibility discovery and shadow previews where mapping is verified. | No runtime claims, execution authorization, accepted execution results, human completion, live projection, or product-file edits by this library. |
| 2: Controlled task execution | A task owner processes its leaves with current packets and records attributable results and reviews. | Task-scoped claims, lease/fencing behavior, leaf progress under task ownership, result and acceptance provenance, execution review/rework, human authority integration. | No independent leaf dispatch. No weakened recovery guarantee because a journal was scheduled for a later slice. Live cutover remains separately gated. |
| 3: Advanced changes and live compatibility | Safe promotion and verified activation of the tree-backed legacy integration. | Contract-preserving leaf promotion, any additional receipt/journal capabilities justified by the runtime, verified adapters, cutover and rollback procedure. | No automatic production cutover; no new distributed coordination requirement. |

The limited rollback slot is a Slice 1 correctness requirement. A general prepared roll-forward journal and long-lived idempotency receipts are deferred. Slice 2 must extend persistence first if its new operations cannot be made safe with the existing protocol.

### 2.2 Required Slice 1 vertical flow

The implementation must demonstrate this through a real application adapter, not only mocks around a repository interface:

1. Create a root, a phase, a task, and at least two leaf skeletons.
2. Prepare a fresh planning packet for the first leaf, and persist that it was inspected without approving it.
3. Draft its specification, record a substantive question, and persist a useful checkpoint.
4. Exit the process. Start a new process without the old conversation context.
5. Identify the same unfinished planning obligation and prepare its current packet.
6. Answer the question through an authorized path. Reconcile the affected specification and refresh its context.
7. Request and record independent planning review. Preserve a rejected review and a corrected draft before approval in the test scenario.
8. Move on to the next unresolved obligation without discarding unrelated approved work.
9. Demonstrate that task execution is still disabled for the new tree and that the live legacy file has not changed.

### 2.3 Slice 1 exclusions

Do not build a new dashboard, a general agent orchestration engine, an identity provider, a distributed lock service, a generic event store, or a new executor. Reuse existing planner/reviewer/session facilities where available. Otherwise provide a thin command interface that can exchange packets, drafts, questions, and authorized decisions.

Do not create a separate PhaseFlow project, commit, push, deploy, or change production without separate authorization.

## 3. Focused repository verification and legacy contract

Reuse the reported legacy scan. Verify it against the current checkout rather than starting another broad architecture investigation.

Read applicable repository guidance and locate the actual planning root, model types, serialization, status transitions, planner entry point, question-round mechanism, reviewer/session identity source, task dispatcher, consumers, writers, filesystem helpers, and tests. Record the checkout revision and relevant paths/symbols in the verification report.

### 3.1 Required compatibility map

For every discovered consumer or writer, record:

- Its actual file path and entry point.
- Whether it displays, schedules, claims, mutates, resumes, or marks completion.
- Its assignment unit and identity rules.
- Exact field types, enum meanings, omitted/null/empty behavior, and ordering.
- The smallest future adapter needed, or why read-only compatibility suffices.

Do not assume that the reported count of seven consumers is exhaustive. Verify the named executor, TUI, serve pipeline, desktop application, and macOS breakdown panel, plus the other discovered paths.

### 3.2 Legacy values that cannot be guessed

| Legacy concept | Required treatment |
| --- | --- |
| Whole-task claim | Preserve task identity and ownership. Do not emit one legacy task per leaf as a shortcut. |
| Task agent/model/wave | Preserve exact current scheduling semantics, including the meaning of absent or null values. |
| `is_contract` | Verify its interface-task and wave behavior. It is not a specification revision or an approval flag. |
| `corrected` | Identify its actual entry/exit transitions and resume behavior. Never assume it means accepted completion or map it to `completed` without evidence. |
| Task `contract_flagged` | Preserve mismatch/blocking behavior at task scope. It is not restricted to `api_binding` leaves. |
| `revision_rounds` | Preserve the actual counter and its update rules. It is neither `spec_revision` nor the number of planning approvals. |

Keep a sanitized source fixture and a mapping ledger with verified, pending, and unsupported entries. The exact JSON types of these legacy fields are not established here; use the verified legacy Go types rather than inventing replacements.

A compatibility snapshot may retain source bytes for lossless comparison, but opaque preservation alone does not establish behavioral compatibility. Never create accepted tree evidence from a legacy status. Slice 1 need not import historical execution results.

Unknown mappings do not block independent planning features. They block the affected preview/adapter and all future live activation. Return an explicit diagnostic rather than a guessed projection.

## 4. Files, versions, IDs, and authority

### 4.1 Layout

Use the actual project-specific planning root if one exists. The following paths are relative to that root; `.planning/` is illustrative of the established default.

```text
.planning/
  assignments.json                       # Live legacy file. Untouched by Slice 1.
  plan/
    plan.json                            # Canonical root and planning policy.
    questions.json                       # Canonical question collection.
    overview.md                          # Optional non-authoritative prose.
    phases/
      _index.json                        # Derived child listing.
      phase-001/
        meta.json
        tasks/
          _index.json
          task-001/
            meta.json
            steps/
              _index.json
              step-001/
                meta.json                # Concrete leaf or composite.
              step-002/
                meta.json                # Composite example.
                substeps/
                  _index.json
                  substep-001/
                    meta.json
    _state/
      write.lock                         # Actual cross-process lock, not a presence flag.
      commit.json                        # Committed canonical membership and byte hashes.
      pending/                           # At most one recoverable mutation, not an archive.
      preview/
        assignments.json                 # Optional verified shadow projection.
        compatibility-report.json        # Derived report, never execution authority.
```

Do not create a live projection sidecar outside `plan/` during Slice 1.

### 4.2 Versions and topology

Every new canonical JSON document and internal generated JSON document uses `schema_version: 4`. Legacy JSON remains unchanged. The root declares `capabilities: ["planning"]` and `execution_mode: "shadow"`; other values are rejected by the Slice 1 implementation.

Readers reject unsupported versions. An existing v2/v3 tree must be converted by an explicit import operation, never silently rewritten on load. Historical source fixtures may retain their original field names and versions.

Use exactly three digits, `001` through `999`, per segment:

```text
plan
phase-NNN
phase-NNN.task-NNN
phase-NNN.task-NNN.step-NNN
phase-NNN.task-NNN.step-NNN.substep-NNN[.substep-NNN...]
```

IDs are immutable. Reject sibling overflow and never renumber surviving nodes after deletion. Phases contain tasks, tasks contain steps, and composites contain substeps. Concrete leaves have no child container. A composite and at least one child must be created in the same committed mutation.

Draft roots, phases, and tasks may temporarily have empty child containers. They cannot be declared fully planned until their decomposition is present. Their own governing requirements may be reviewed independently of descendant drafting.

### 4.3 Reference resolution

Resolve `parent_ref` relative to the directory containing the current canonical file:

| Current node | `parent_ref` |
| --- | --- |
| Root `plan.json` | `null` |
| `phases/phase-001/meta.json` | `../../plan.json` |
| Task, step, or substep `meta.json` | `../../meta.json` |

Resolve an index entry's `ref` relative to its index directory. Work and artifact paths use a separate repository-root-relative namespace.

Validate exact ID/path correspondence and immediate structural parenthood. Reject absolute metadata references, escaping traversal, symlinked managed metadata, and inappropriate target kinds. Reuse verified root-scoped filesystem facilities supported by the checkout; a path-prefix string comparison is not the containment contract.

Concurrent hostile manipulation of a trusted local checkout is not a security guarantee of this planning library.

### 4.4 Canonical and derived state

Canonical domain documents are `plan.json`, `questions.json`, and node `meta.json` files. The manifest and pending recovery metadata establish which canonical publication is committed; they do not replace domain requirements.

Filesystem topology determines child membership, and the committed manifest detects unexpected additions, omissions, or changes. Numeric segment order determines sibling order. An index cannot hide a real canonical child or invent one.

Indexes, structural status summaries, overview prose, packets, and legacy previews are never authority for approval or readiness. Structural summaries are derived in memory or in indexes; do not rewrite every ancestor's canonical metadata for a leaf inspection or approval.

Normal reads do not repair files. A stale or missing index can be reported and bypassed using canonical state. Explicit derived repair may write a corrected index. Canonical inconsistencies require recovery or explicit reconciliation, never reconstruction from a cache.

## 5. Canonical models

Examples below define field shapes, not ready-to-execute product assignments. Reject unknown canonical fields except within specifically declared JSON payload fields. Distinguish missing, null, empty, and invalid values deliberately in codecs and tests.

### 5.1 Common node envelope

The root, phases, tasks, composites, and concrete leaves share these fields:

| Field | Type and rule |
| --- | --- |
| `schema_version` | Integer `4`. |
| `id`, `title` | Non-empty strings; root ID is `plan`. |
| `node_revision` | Positive integer; increases for each canonical mutation to this node. |
| `spec_revision` | Positive integer identifying current effective requirements. See section 8. |
| `context_revision` | Positive integer; advances when the digest or its recorded basis changes. |
| `context_digest` | Non-empty concise explanation relative to the immediate parent's objective. |
| `context_basis` | Parent ID, parent spec revision, and parent context revision; `null` at root. |
| `parent_ref` | Immediate parent metadata path; `null` at root. |
| `depends_on` | Array of unique node IDs. |
| `status` | Leaf lifecycle value; `null` for structural nodes, whose aggregate status is derived. |
| `control` | Nullable explicit hold/skip record. Structural and leaf holds are distinct from derived summaries. |
| `planning` | Revision-aware progress, authoring provenance, review history, and reconciliation record. |
| `resume_note` | Nullable bounded text for the next planning session. |
| `contract_flagged` | Boolean indicating a known interface mismatch, independent of lifecycle status. |
| `contract_flag` | Nullable structured reason/actor/time/affected-ID record, required when flagged. |

Do not require dormant claim tokens, transaction receipts, attempt archives, or execution results on every Slice 1 node. Those fields are not writable in the planning-only schema. A later schema extension must explicitly version and migrate added runtime state.

Constructors populate required arrays, nullable fields, and revisions. Revisions begin at `1`. A leaf skeleton has `work: null`, `spec: null`, and `status: "untouched"`. A non-null malformed partial spec is an error; unfinished free-form drafting can live in the bounded `resume_note` checkpoint until a valid typed spec can be saved. Such a note is provisional, not approved requirements.

`contract_flagged` may be true on a task or `api_binding`, and on another scope only where the verified legacy mapping or an explicit interface relationship requires it. A task flag applies to its descendant work; unrelated tasks are not implicitly flagged. Clearing a flag records the resolution and trusted actor. Changing requirements while resolving it also advances `spec_revision`.

### 5.2 Planning record

```json
{
  "planning": {
    "stage": "drafting",
    "for_spec_revision": 3,
    "inspection": {
      "spec_revision": 3,
      "context_revision": 2,
      "actor": {"kind": "agent", "id": "planner", "session_id": "author-session-7"},
      "at": "2026-09-19T12:00:00Z"
    },
    "author_sessions": ["author-session-7"],
    "authored_spec_revision": 3,
    "reconciliation": {
      "required": false,
      "cause_refs": [],
      "resolved_spec_revision": 3,
      "resolved_by": {"kind": "agent", "id": "planner", "session_id": "author-session-7"},
      "resolved_at": "2026-09-19T12:05:00Z",
      "notes": "Reconciled the declared inputs and requirements."
    },
    "review_history": [],
    "updated_at": "2026-09-19T12:05:00Z"
  }
}
```

`inspection`, `authored_spec_revision`, and reconciliation resolution fields may be null in a skeleton. `author_sessions`, `cause_refs`, and `review_history` are arrays. Allowed stages are:

```text
skeleton | inspected | drafting | awaiting_answers | awaiting_review
changes_requested | approved | needs_reconciliation
```

Stages are consequences of the operations in section 7, not arbitrary caller-set values. `for_spec_revision` binds the checkpoint to an effective specification version. Inspection records do not count as authorship. Every agent session contributing substantive requirements since the last approved baseline is recorded as an author; reconciliation that retains unchanged text still records the responsible session.

A human edit records its actor in the mutation/review provenance without pretending it came from an agent session. Actor objects use `kind: human|agent`, a non-empty ID, and an agent session ID when the actor is an agent. Obtain actor identity and time from the trusted application boundary, not arbitrary agent-written JSON.

### 5.3 Planning review record

```json
{
  "decision": "approve",
  "spec_revision": 3,
  "reviewed_context_revision": 2,
  "author_sessions": ["author-session-7"],
  "approved_by": {"kind": "agent", "id": "spec-reviewer", "session_id": "review-session-11"},
  "at": "2026-09-19T12:10:00Z",
  "findings": [
    {"area": "verification", "outcome": "pass", "notes": "Each acceptance criterion has a declared check."}
  ],
  "governing_basis": [{"id": "phase-001.task-001", "spec_revision": 2}],
  "input_basis": [],
  "question_basis": []
}
```

Allowed decisions are `approve`, `request_changes`, and `inconclusive`. For non-approval decisions, use `reviewed_by` instead of `approved_by`; implement this as an explicit tagged record, not ambiguous optional actor fields. Findings use `pass|fail|not_assessed` and identify the reviewed obligation. A current approval requires complete applicable coverage, no failed or unassessed mandatory finding, and a still-current basis.

Review history is append-only through normal planning operations. Preserve prior rejections and obsolete approvals. Human-authorized removal of a draft node is a separate operation, not a mechanism for editing its history in place.

### 5.4 Work and verification contract

Every non-draft concrete leaf requires:

```json
{
  "work": {
    "target_paths": ["internal/example/classifier.go", "internal/example/classifier_test.go"],
    "package": "internal/example",
    "read_only": false,
    "constraints": ["Preserve the declared public interface."],
    "acceptance_criteria": ["Malformed input returns an error without a panic."],
    "verification": {
      "checks": [
        {
          "id": "check-001",
          "method": "automated",
          "description": "Exercise the malformed-input cases.",
          "covers": ["Malformed input returns an error without a panic."]
        }
      ],
      "commands": [
        {"argv": ["go", "test", "./internal/example"], "working_directory": "."}
      ]
    }
  }
}
```

The command is illustrative; derive actual commands from the checkout. Slice 1 stores verification plans but does not run product verification commands or fabricate evidence that they passed. The implementing agent still runs the implementation's own build and test suite; the restriction concerns executing assignments from the new planning tree.

Check IDs are unique. Every acceptance criterion has an explicit verification path. Methods are `automated`, `inspection`, `review`, `human`, or `aggregate`. A `review` check adds `review_node_id`. An `aggregate` check adds `child_ids` and explicitly identifies the child obligations relied on. Required references must resolve. An optional typed `coverage_exceptions` array inside `verification` records `{category: success|failure|edge, reason}` for genuinely non-applicable test categories; an empty array is the default. Do not hide coverage exceptions in an unknown field or count them as passing tests.

File-modifying work declares concrete destination paths and package/module context where applicable. Investigation and physical work can have no product target paths when their output or external target is explicit. `package` may be null. These fields describe permitted scope; they are not an operating-system sandbox.

### 5.5 Structural documents

**Root:** The envelope plus `objective`, `success_criteria`, `constraints`, `non_goals`, `key_decisions`, `verification`, `phases: "phases/_index.json"`, `capabilities`, `execution_mode`, and `approval_policy`.

**Phase:** The envelope plus `objective`, `acceptance_criteria`, `verification`, and `tasks: "tasks/_index.json"`.

**Task:** The envelope plus `objective`, `acceptance_criteria`, `verification`, `steps: "steps/_index.json"`, and an `execution` block containing `wave`, `is_contract`, `candidate_models`, and `agent`. Use verified legacy types for these scheduling fields. This is planning metadata only in Slice 1.

**Composite:** The envelope plus `type: "composite"`, `objective`, `acceptance_criteria`, `verification`, and `substeps: "substeps/_index.json"`. It has no executable `work` or `spec`. Slice 1 supports creating a composite with children, not promoting an already specified/executed leaf by overwriting its type.

Root success criteria and structural acceptance criteria use the same verification-coverage rule as leaf criteria. Governing requirements can be approved while unrelated descendants remain drafts. Approval of a task's own requirements is not approval of all its leaves.

An explicit `control` has `state: blocked|delayed|escalated|skipped`, non-empty reason, actor, and timestamp. A leaf with a local control stores that control state as its visible `status`; after release, status is recomputed as `reviewed` only for current approval, otherwise `untouched`. Planning history remains separate. Derived blockage from an unfinished child does not become a controlling hold on its siblings.

## 6. Concrete leaf types

Implement all 17 discriminated spec types. Use explicit Go structs and tested unions. Do not use an unrestricted `map[string]any` as the canonical specification model. Arbitrary JSON is allowed only for declared example/test input, output, and request/response-shape payload fields.

The following field lists are normative. Fields named as arrays must be arrays even when they have a single value. All required descriptive strings are non-empty. A skeleton may omit the entire spec; an authored spec must have the applicable complete shape.

### 6.1 `function_def`

- `signature`: `name`, `params` array of `{name, type, description}`, and `returns: {type, description}`.
- `logic`: algorithm and observable behavior.
- `edge_cases`: string array.
- `test_spec`: `strategy` and `cases` array.

Each case has `id`, `kind: success|failure|edge`, `input`, and the appropriate `expected_output` or `expected_error`. Destination files/package come from `work`, not from an assumed digest.

### 6.2 `schema_change`

`target`, `change`, `migration_direction: {up, down}`, and boolean `backfill_needed`.

Both directions must be addressed. An irreversible operation explicitly states that limitation and its recovery/approval procedure; do not invent a rollback. Execution of migrations is outside Slice 1.

### 6.3 `data_migration`

`source`, `transform`, `target`, boolean `idempotent`, and `rollback_plan`.

Verification covers the transformation and any claimed idempotency. A non-idempotent operation declares retry restrictions in `work.constraints`.

### 6.4 `file_artifact`

`path`, `purpose`, and `content_requirements` string array. The path must be included in writable `work.target_paths`.

### 6.5 `refactor`

`target_files`, `current_behavior`, `desired_behavior`, `must_not_change`, and `regression_checks`.

Use regression checks rather than a new behavior `test_spec`. An intentional external behavior change must be represented explicitly, not hidden in a refactor.

### 6.6 `bugfix`

`symptom`, `root_cause_hypothesis`, `fix_description`, and `regression_test` containing `id`, `kind: "failure_repro"`, `input`, and `expected_output` or `expected_error`.

A hypothesis is not an established finding. State how the incorrect behavior will be reproduced and the correction verified, with a reason where automation is not possible.

### 6.7 `integration`

`components` node-ID array, `wiring_description`, and `failure_modes_to_handle` string array.

Components are concrete non-review leaves or composites exposing the required deliverable. These references add ordinary prerequisites.

### 6.8 `investigation`

`question`, `method`, and `output_format`. Require `work.read_only: true` for the system under investigation. Recording findings or a declared report artifact is allowed; unrelated product modification is not.

The future investigation result is distinct from a Slice 1 planning note. Do not manufacture execution results while specifying an investigation.

### 6.9 `review`

`validates` node-ID array and `criteria` string array.

This is post-execution review, not the planning approval operation. Its targets are concrete non-review leaves or composites. It may not validate itself, another review, or a containing ancestor. A required review consumes a target candidate before that target is accepted; see section 10.

### 6.10 `human_gate`

`instruction`, boolean `requires_physical_action`, `verification_method`, `safety_notes` string array, and `estimated_duration` string.

This represents human work, including physical work. `estimated_duration` is planning data, not a promised completion time. Planning approval never counts as completion or attestation. Machine dispatch and human confirmation are not available in Slice 1.

### 6.11 `proof_harness`

`target_function`, `properties` string array, `proof_strategy`, `harness_description`, and a typed `bounds` object supporting positive integer `loop_unwind` where applicable.

Identify file/package and explicit assumptions. Do not claim that a proposed bounded proof establishes an unbounded property. A proof strategy with other bounds requires a deliberate typed schema extension, not an unchecked property bag.

### 6.12 `fuzz_target`

`target_function`, `input_generator_description`, `invariants` string array, `corpus_seed` array, and `budget` with optional positive `iterations` and `duration_seconds`.

At least one budget limit is required. With both specified, the future executor stops at the first limit. Planned cases remain distinct from later discovered cases.

### 6.13 `ui_component`

`component_name`; `props` array of `{name, type, required, description}`; `emits` array of `{event, payload, when}`; `internal_state` array of `{name, type, purpose}`; `visual_description`; `states_to_render`; and `test_spec: {strategy, cases}`.

Render cases have `id`, `kind: "render"`, `props`, and `expected`. Interaction cases have `id`, `kind: "interaction"`, `action`, and `expected`. Edge cases declare the relevant inputs and `expected`. Accessibility and loading/error/empty behavior belong in the explicit requirements when applicable.

### 6.14 `ui_page`

`route`, `layout_description`, `data_dependencies` array of `{source, loading_state_required}`, and `test_spec: {cases}`. Navigation cases contain `id`, `kind: "navigation"`, `action`, and `expected`.

Each data `source` refers to an `api_binding` and creates an ordinary prerequisite. Component composition uses explicit `depends_on`.

### 6.15 `ui_style`

`scope`, `design_tokens_used`, `visual_requirements`, and `responsive_behavior: {breakpoints, behavior}`.

`scope` is the literal `"global"` or a non-empty array of `ui_component` IDs; implement that union explicitly. Component references create ordinary prerequisites. At least one required `review` validates the style and is named in its work verification. Automated checks may supplement but do not silently replace that review.

### 6.16 `ui_interaction`

`flow_description`, `trigger`, `steps` string array, and `test_spec: {cases}`. Cases contain `id`, `kind: "interaction"`, `action`, and `expected`. Involved components use explicit `depends_on` IDs.

### 6.17 `api_binding`

`endpoint: {method, path}`, `backend_step_ref`, `request_shape`, `response_shape: {success, error}`, `loading_error_states_ui_must_handle`, and `test_spec: {cases}`.

Contract cases contain `id`, `kind: "contract"`, `given`, and `expected`. `backend_step_ref` identifies a `function_def`, `integration`, or a composite whose approved boundary explicitly exposes that deliverable; it creates an ordinary prerequisite.

A flagged mismatch prevents planning approval of the affected interface until resolved. Task-level flags remain meaningful independently of this leaf type.

For every test-bearing type, specify applicable success, failure, and edge coverage, or an explicit justification for a non-applicable category. A list of generic example cases is not a complete product specification.

## 7. Planning lifecycle and approval authority

### 7.1 Keep three concepts separate

1. **Planning progress:** What has been inspected, drafted, questioned, or reviewed for the current requirements.
2. **Planning approval:** An attributable decision accepting a particular effective specification and its basis.
3. **Execution/acceptance:** Whether work ran, produced a current candidate, passed required gates, and was accepted. Slice 1 does not implement this third concept.

Retain the known lifecycle vocabulary in compatibility documentation:

```text
untouched | reviewed | in_progress | needs_revision | completed
blocked | delayed | skipped | failed | escalated
```

Slice 1 can persist unexecuted leaf states `untouched` and `reviewed`, plus deliberate `blocked`, `delayed`, `skipped`, or `escalated` controls with a non-empty reason. It must not create `in_progress`, `needs_revision`, `completed`, or `failed` as new execution facts. Legacy values are preserved in their verified compatibility representation, not converted into invented tree results.

A rejected planning review is `planning.stage: changes_requested`; it is not failed product execution and does not use `needs_revision`. That execution state remains reserved for later rework.

### 7.2 State operations

| Operation/event | Required effect |
| --- | --- |
| Create leaf skeleton | `status: untouched`, stage `skeleton`, no approval or spec. |
| Record inspection | Record actor/time/current revisions. Advance `skeleton` to `inspected`; never downgrade a later stage or approve work. |
| Save draft specification | Validate its typed shape, record authorship, apply semantic revision rules when requirements change, and set stage `drafting`. |
| Record a partial drafting note | Update a bounded checkpoint/note without pretending the typed spec is complete. |
| Add a blocking question | Set affected planning work to `awaiting_answers` and invalidate its prior approval conservatively as described in section 9. |
| Request planning review | Require approval-level completeness and current basis; set `awaiting_review`. This request is not approval. |
| Request changes | Append the independent review, preserve findings, leave leaf `untouched` unless held, set `changes_requested`. |
| Inconclusive review | Preserve findings and return an actionable reason; do not approve. Keep `awaiting_review` or an explicit hold. |
| Approve specification | Append an authorized current-basis approval; set stage `approved` and unheld leaf `reviewed`. |
| Requirements change | Advance affected effective spec revisions, preserve review history, invalidate old approval, and require drafting/reconciliation. |
| Explicit hold/skip | Record reason and authority without erasing draft, question, or review history. |
| Release hold | Recompute the appropriate planning state from the current spec/review/question basis; never manufacture execution completion. |

All of these operations use repository concurrency checks. There is no public unrestricted `SetStatus`, `SetStage`, or `ClearStaleFlags` operation.

### 7.3 Structural summaries

A structural node's own planning approval and its descendants' planning completeness are separate fields in the read model. Derive counts for skeleton, drafting, awaiting-answer, awaiting-review, approved, reconciliation-needed, held, and skipped descendants.

For an aggregate planning status:

- An explicit local control supplies its visible held/skipped state.
- Use `reviewed` only when the structural node's own specification and all required descendant planning obligations are current and approved.
- Otherwise use `untouched` with structured blockers and counts, unless an explicit control supplies another state.
- Never emit `completed` from planning approval or from an empty child set.

A skipped child is not an approved or completed child. Slice 1 reports the omission and does not invent a waiver or accepted outcome. Explicit scope revision and human authorization are required to remove an approved obligation. Later execution-scope waivers are deferred.

A derived blocked/unplanned summary is not an execution or planning hold on independent siblings. Only explicit controls and actual requirements/dependencies impose that restriction.

### 7.4 Approval policy

The root carries this explicit policy shape:

```json
{
  "approval_policy": {
    "routine_specification": "independent_agent_or_human",
    "governing_decisions": "human",
    "approved_scope_changes": "human",
    "human_gate_completion": "human",
    "allow_agent_self_approval": false
  }
}
```

Slice 1 accepts only the stated policy values. Do not add a configurable policy language.

Initial root objectives, non-goals, success criteria, and governing choices need human authorization. Routine decomposition and implementation specifications within that scope may be approved by an independent reviewer session. Changes that broaden or remove already approved scope require human authorization; uncertain scope classifications default to the human-controlled path.

The reviewer must receive the current specification, governing requirements, relevant input specifications and answers, verification plan, and prior relevant findings. For agent approval, its session ID must differ from every contributing author session for the current draft lineage. Using another model is not required. A separate session is a process control, not a correctness guarantee.

The application supplies trusted actor/session identity and authorization. A worker cannot become a reviewer or a human by putting another name into JSON. Reject agent self-approval, forged human approval, stale review submissions, and unapproved governing scope.

Do not build a new identity provider or orchestration engine. Reuse the repository's session/role integration. Where no separate reviewer session can be established, allow an actual authorized human review through the command adapter; do not silently fall back to author self-approval.

Approval covers the specification, not runtime evidence. Approving a `human_gate` specification does not confirm its physical action.

## 8. Revisions, freshness, and impact

### 8.1 Revision meanings

| Revision | Advances when | Does not mean |
| --- | --- | --- |
| `node_revision` | Any canonical field on that node changes. | Work is stale merely because it was inspected or reviewed. |
| `spec_revision` | Local semantic requirements change, or a governing/input-spec change affects this node. | A legacy interface-task flag or count of correction attempts. |
| `context_revision` | The digest or the recorded basis used to write it changes. | Approval or acceptance of the specification. |
| Question collection `revision` | Any persisted question record changes. | Every node's requirements changed. |
| Question `answer_revision` | A substantive decision is answered, changed, or reopened. | A recommendation has been accepted. |

`spec_revision` is an effective requirements version, including inherited obligations and changed input specifications. It is not merely a counter of edits to the local `spec` JSON.

Use `spec_revision`, `parent_spec_revision`, `approved_spec_revision` where an explicit approval-version field is needed, `source_spec_revision`, and corresponding spec-based provenance names in new code and data. Do not introduce new requirements-version fields named `contract_revision`. Old source fixtures and explicit conversion code may retain that original name for migration purposes.

### 8.2 Edit classification

Semantic edits include changes to objectives, scope, criteria, verification obligations, governing decisions, constraints, work specifications, ordinary input relationships, required review obligations, and substantive question decisions.

Titles, inspection timestamps, recovery notes, review findings, scheduling preferences that do not change work restrictions, and regenerated indexes are not themselves semantic edits. Editorial digest changes are non-semantic only when they introduce no requirements. Record the classification and rationale through a typed mutation request. Default uncertain changes to semantic.

A changed local specification records its authoring session. An unchanged specification reconciled after an external input change records who performed that reconciliation.

### 8.3 Impact closure

For a semantic edit, determine the affected set from authoritative graph state before and after the proposed edit:

- The directly edited node and its governed descendants.
- Ordinary consumers of affected input specifications, and their governed descendants.
- Review nodes whose target requirements changed.
- Targets whose required review obligations changed.
- Further affected consumers through those relationships.

Use the union of old and new relationships where a reference is added or removed, and a visited set. Increment each affected node's `spec_revision` at most once in the mutation. Preserve prior review records; mark them obsolete by basis rather than rewriting their history.

Changing a child does not automatically rewrite the parent's requirements. Recompute the parent's planning summary; revise the parent spec only when its own boundary obligations change. A parent's ordinary consumers need reassessment when that published boundary changes, not just because a child was inspected.

A broad root decision or answer can legitimately touch many nodes. This is not a reason for every inspection, review decision, or unrelated status update to rewrite the entire tree.

### 8.4 Reconciliation and context

Affected nodes whose existing text was not explicitly reconciled by the semantic edit receive `planning.reconciliation.required: true` with cause references. A controlled reconciliation operation must either update the spec/work or explicitly explain why the retained requirements still satisfy the changed basis. Clearing the flag without that record is forbidden.

Reconciliation does not grant approval. It prepares the current specification for renewed independent review.

A context refresh records the current parent's spec/context revisions and is performed top-down. Context freshness is evaluated against the actual authoritative ancestor chain. A parent context change can make descendants stale without rewriting a `digest_stale` boolean into every file; v4 exposes `context_current` as a derived read result.

A purely editorial digest refresh does not invalidate unchanged semantic approval. It may still require refreshing descendant context before their next fresh packet is used. Required constraints cannot be introduced only in a digest or `overview.md`.

### 8.5 Freshness results

Read responses expose:

```text
context_current
planning_approval_current
result_current
acceptance_current
```

In Slice 1, the last two are `null` with reason `execution_not_implemented`. They are not true, false success, or inferred from legacy display statuses.

Current planning approval requires a matching effective spec revision, approved governing scope, matching relevant input-spec/answer basis, no unresolved reconciliation, and no blocking question or applicable unresolved contract flag. Context freshness is reported separately. An approval's observed context revision records what was reviewed; an editorial refresh alone does not change its semantic basis.

Result freshness and acceptance freshness remain separate future questions. Refreshing context or approving a specification must never synthesize either.

## 9. Questions and question rounds

### 9.1 Canonical shape

```json
{
  "schema_version": 4,
  "revision": 1,
  "questions": [
    {
      "id": "q-001",
      "question_revision": 1,
      "answer_revision": 0,
      "question": "Which response shape should the binding implement?",
      "options": [
        {"id": "option-a", "label": "Existing interface", "description": "Keep the current published response shape."},
        {"id": "option-b", "label": "Revised interface", "description": "Adopt the separately specified replacement."}
      ],
      "multi_select": false,
      "allow_free_text": true,
      "recommended": {
        "option_ids": ["option-a"],
        "rationale": "Avoid an unapproved interface change."
      },
      "decision_class": "governing",
      "required_authority": "human",
      "status": "open",
      "answer": null,
      "affects": ["phase-001.task-001.step-001"],
      "no_execution_impact_reason": null,
      "answered_by": null,
      "answered_at": null,
      "history": []
    }
  ]
}
```

An answered record uses:

```json
{
  "answer": {
    "option_ids": ["option-a"],
    "text": "Keep the published shape for this release."
  }
}
```

`recommended` may be null. `status` is `open|answered`. `decision_class` is `governing|implementation|informational`; `required_authority` is `human|authorized_planner`. Governing choices and approved-scope changes always require `human`.

Option IDs are stable and unique within the question. Selected and recommended IDs must exist and contain no duplicates. With `multi_select: false`, at most one ID may be selected or recommended. With free text disabled, an answer must select at least one option. With free text enabled, a non-empty text-only answer is allowed. A non-empty free-text response is not permitted when `allow_free_text` is false.

`history` preserves prior answers/reopen events with actor, timestamp, prior answer revision, and reason. Do not overwrite the only record of a governing decision.

### 9.2 Questions are decisions, not defaults

A recommendation is never an answer or permission to proceed. The question round displays recommendations separately and submits stable option IDs, not display indexes or labels. Reading or displaying a question must not select a recommended answer automatically.

The trusted integration resolves the respondent's authority. An implementation question may be answered by an authorized planner within the already approved scope. Ambiguous product/scope changes go to a human.

### 9.3 Impact and editing rules

Validate every `affects` ID; `plan` is allowed for plan-wide impact. An informational question with empty `affects` requires `no_execution_impact_reason` and does not hold the plan.

A non-informational open question blocks affected planning approval, not unrelated branches. V4 deliberately takes a conservative approach: adding or reopening a blocking question invalidates approval in its impact closure and requires reconciliation after resolution. Answering or changing the substantive decision applies semantic revision propagation again as appropriate. Record the cause so the resume path explains the required work.

Changing the wording, available options, selection rules, or affected scope of an answered substantive question requires reopening it. Changing only a recommendation does not rewrite an existing answer or automatically invalidate a specification; record it as a non-semantic question update.

Use expected collection/question revisions for writes. For semantic approval provenance, bind to each relevant question ID and answer revision, not the global collection revision, so an unrelated question does not invalidate every approval.

Answer persistence and the corresponding affected-node invalidation are one repository mutation. A crash must not publish an answer while leaving its obsolete affected approvals authoritative.

### 9.4 Integration requirement

Reuse the existing question-round UI or command flow if present. Otherwise provide a thin structured command input/output adapter supporting `recommended`, `multi_select`, stable option IDs, and free text. Creating a new UI framework is not in scope.

At least one integration test must display a multi-select question, record two selected IDs, restart, and recover the same answer without losing its meaning or converting the recommendation into consent.

## 10. Validation, dependencies, and task compatibility

### 10.1 Validation levels

| Level | What it establishes |
| --- | --- |
| Document/structure | Supported version/capability, typed fields, IDs, paths, immediate parents, permitted topology, revisions, and actor/time shapes. |
| Draft graph | All recorded IDs resolve, reference kinds are correct, effective graph has no prohibited cycles, composites are non-empty, leaves have no children. Null draft specs are allowed. |
| Planning approval | Complete typed spec and work contract, criterion/check coverage, authorized/current governing basis, reconciled context and input specs, resolved affected questions, appropriate independent reviewer. |
| Task-profile compatibility | The planned relationships can be represented by the initial whole-task execution profile, or specific future integration blockers are reported. |
| Committed integrity | Canonical membership/bytes match the committed manifest and no unresolved mutation is present. |

Missing specs in an unrelated branch do not invalidate otherwise complete planning work. Missing governing requirements or required input specifications do affect the dependent node. References cannot point to imaginary future nodes: create their skeletons in the same mutation or add the reference later.

Apply documented configurable limits to canonical file size, node count, nesting depth, history, and note size using existing repository limits where available. Reject oversized input rather than silently truncating it; future history compaction needs an explicit preservation policy. Return stable diagnostic codes, severity, node/question ID, field path, and actionable explanation. Distinguish an incomplete draft from corruption and from an unsupported runtime capability.

### 10.2 Effective relationships

Keep `depends_on` as an ID array. Derive, do not independently persist, the effective graph from:

- Explicit ordinary dependencies.
- Governing structural prerequisites inherited by descendants.
- `integration.components`, `ui_page.data_dependencies`, `api_binding.backend_step_ref`, and component-scoped `ui_style.scope`.
- Separate `review.validates` relationships.
- Structural containment and verification references.

Validate all reference-bearing fields, not only `depends_on`. For an allowed composite input, validate the structural kind and require an attributable planning-review finding identifying the matching boundary requirements; a prose scan is not proof of interface compatibility. Deduplicate identical ordinary prerequisites. A review of a target is not an ordinary dependency on that target's already accepted completion.

A planning packet may be prepared before an input is executed. It needs the input's relevant specification and must clearly distinguish a planned input from an available accepted artifact. Planning approval requires sufficiently complete/current input requirements, not fabricated execution outputs.

### 10.3 Production and acceptance graph

Implement pure graph validation sufficient to reject future deadlocks while planning. Use conceptual milestones, not new persisted runtime records:

```text
Produce(node) -> Accept(node)
Accept(ordinary dependency) -> Produce(consumer)
Accept(child) -> Produce(structural parent)
Produce(review target) -> Produce(review)
Accept(required review) -> Accept(review target)
```

Reject self-dependencies, dependencies on a containing ancestor, and cycles in the expanded graph. Return the actual cycle path and relation types.

A reviewer must not also list its own target in `depends_on`; doing so can require acceptance before the review needed to permit acceptance. A review of a composite boundary must be outside that composite's subtree.

Verification references to a required reviewer must match its `validates` relationship. Every declared validation gate matters even if the author forgets to repeat it in the target's verification block; report the missing documentation at approval time.

### 10.4 Initial task execution profile

The future scheduler selects and owns a task. That task's worker requests compact packets for individual leaves and records their progress under the task's ownership. It must not mark the task done because one leaf finished, and it must not concatenate all leaf specifications into the initial prompt as a substitute for packet preparation.

Within a task, the future integration may sequence leaves and invoke a separate reviewer session without creating independent scheduler-level leaf claims. The task remains incomplete while mandatory review or human work is outstanding.

For initial inter-task scheduling, require prerequisites that can be settled at whole-task boundaries. Check both the fine-grained graph and the derived task-level prerequisite graph. A fine-grained acyclic graph can still require alternating work across tasks that the current executor cannot represent.

Report `UNSUPPORTED_TASK_INTERLEAVING` for such arrangements, including a cross-task review gate requiring partial output exchange that the verified executor cannot support. Do not fix it silently by removing dependencies, treating candidates as accepted outputs, or dispatching leaves independently.

This compatibility result is separate from draft validity. A plan may be stored and discussed while its task profile has an identified blocker; it must not be described as executable. All new-tree execution remains disabled in Slice 1 regardless.

## 11. Compact packets and deterministic planning resumption

### 11.1 Packet purposes

`PrepareAssignment` is a side-effect-free read operation with `purpose: planning|planning_review|execution`.

- `planning` permits skeletons and incomplete specifications, and reports what is missing.
- `planning_review` requires a complete reviewable specification and returns the independent-review packet.
- `execution` returns `CAPABILITY_NOT_ENABLED` in Slice 1. Do not return a plausible executable packet with an easily ignored warning.

A successful Slice 1 packet explicitly has `execution_authorized: false`. Preparing, inspecting, or approving it does not claim work.

### 11.2 Packet contents

A packet contains:

- Purpose, node/task identity, type, plan generation, and the target node/spec/context revisions.
- The current context digest and full current work/spec, or explicit missing-field diagnostics for drafting.
- Applicable governing objectives, constraints, scope decisions, and task execution metadata.
- Relevant input specifications and declared artifact references, labeled as planned inputs where no accepted output exists.
- Relevant questions, answers, recommendations, and answer-revision provenance.
- Current planning checkpoint, resume note, and relevant unresolved review findings.
- Explicit verification obligations and required review/human relationships.
- Applicable interface mismatch flags and structural controls.
- Parent reference for deliberate escalation and the exact source-basis tokens needed to validate a subsequent write.

Do not include the full tree, every sibling spec, every question, or a full history archive by default. Root/phase/task planning packets cover that node's own requirements and a compact child summary; they do not dump all descendant specifications.

### 11.3 Basis checking

Include target `node_revision`, effective `spec_revision`, context revision, and relevant governing/input/question basis. The write or review operation rechecks these under the repository lock. A plan-wide generation is a publication identifier, not the sole freshness predicate: unrelated work elsewhere should not automatically invalidate an otherwise current packet.

A semantic change to a relevant source invalidates the packet's proposed write/review. A changed target node revision produces a conflict even when the semantic revision did not change; the caller rereads rather than overwriting a newer checkpoint. Return changed IDs and reasons.

The coordinator may inspect many authoritative files to construct the packet. Index status and legacy display status are not substitutes for those checks.

### 11.4 Budget handling

Use a configured context budget and the actual available tokenizer where integrated. If only a byte/character estimate is available, label it as an estimate rather than reporting invented token counts.

Required contracts and constraints are never silently truncated. Optional historical detail may be summarized with references. When the required packet is too large, return `PACKET_TOO_LARGE` with measured/estimated size, the contributing sections, and a recommendation to decompose or explicitly raise the budget.

Slice 1 can create a new composite during initial planning. Automatic promotion of an existing leaf is deferred; an oversize diagnostic must not silently rewrite the hierarchy.

### 11.5 Resume selection

Provide `NextPlanningAction` and a way to list outstanding actions. A persisted cursor is only a hint and never proof of completeness.

Derive actions from the current committed state using this precedence:

1. Resolve integrity/recovery problems before any planning action.
2. Surface human-controlled questions or governing approvals that block the selected branch.
3. Reconcile changed governing/input requirements and refresh relevant context top-down.
4. Resume a current partial draft/checkpoint.
5. Surface requested revisions and independent review requests.
6. Select the next unplanned node in deterministic numeric tree order, respecting prerequisite specifications.

Do not repeatedly select an action that is blocked on unavailable human input while runnable independent planning work exists. Return runnable actions plus a separate list of blocked obligations and their responsible authority. A specific branch selection may constrain traversal, but must state that scope.

Recording inspection does not advance the cursor past required drafting or approval. Reading an approved node again does not turn it back into an untouched node. A stale checkpoint cannot cause changed work to be skipped.

Expose separate operations to select/prepare and to record progress. A read must not silently mark a node inspected or approved.

## 12. Minimal repository and crash consistency

### 12.1 Scope of the guarantee

Slice 1 supports one cooperating local writer per plan across processes. Use one exclusive repository lock for authoritative reads, mutations, and recovery initially; a more elaborate reader/writer snapshot design is not required. Keep lock hold times limited to local storage/validation work. Never hold the lock across an LLM call, a human question, or product execution.

Use actual supported macOS/Linux locking and filesystem helpers verified in the checkout. A lockfile's presence is not sufficient ownership. Do not promise the same durability on an untested network filesystem. Document the supported local filesystem/platform boundary and test it.

Atomic replacement of individual files does not make an answer plus several invalidations atomic. The following one-operation rollback slot supplies that missing boundary without requiring v3's prepared roll-forward journal or an archive of receipts.

### 12.2 Commit manifest

`_state/commit.json` records:

- `schema_version: 4` and a monotonically increasing committed `generation`.
- `hash_algorithm: "sha256"`.
- Canonical relative file paths and their committed byte hashes.
- The last committed operation ID, actor, and a compact outcome summary sufficient to resolve its immediate retry.

Hashes describe the serialized bytes actually published. Compute new hashes for changed canonical files only; reuse the committed entries for unchanged files. Rewriting the one manifest can be proportional to file count, but must not force rereading, hashing, rewriting, or syncing every canonical file.

The manifest records canonical membership, not an alternative editable graph. Indexes and preview files do not need to share its generation if their own source revisions identify what they reflect. Missing/stale derived files are not failed canonical commits.

### 12.3 Mutation protocol

Every mutation has an operation ID, trusted caller context, expected revisions for directly edited records, and an explicit typed operation. Apply this protocol:

1. Acquire the repository lock. Refuse to proceed through unresolved pending state; run the explicit recovery path first when requested by the coordinator.
2. Read the committed generation and verify relevant source files, expected revisions, and integrity. Calculate the proposed changes and affected graph closure.
3. Validate the proposed coherent state before publishing anything. Calculate the changed canonical paths and the proposed next manifest. No agent or human interaction occurs under the lock.
4. Build a temporary pending directory containing the base manifest, proposed manifest, operation identity, changed-path list, and before-images for existing canonical files. Record which paths did not previously exist. Store checksums for the before-images and proposed after-state. Flush this recovery material, then atomically publish it as `_state/pending/` and flush the containing directory.
5. Only after the pending slot is durable, apply the changed canonical files using same-filesystem write/flush/rename and the required directory durability steps. Deletions and newly created paths must be listed in the pending operation. Unchanged canonical files are not rewritten.
6. Publish and durably flush `commit.json` last. This manifest replacement is the commit point, unlike v3's earlier prepared-journal commit decision.
7. Return the committed outcome and remove the pending slot safely. A leftover slot after commit is harmless only after recovery verifies it. Refresh affected derived outputs separately or return an explicit `derived_refresh_pending` diagnostic without reporting the domain mutation as rolled back.

The slot retains data only for the current operation. It is recovery metadata, not a general audit/event stream. Operations that create several skeleton nodes or answer a question and invalidate its impact closure use the same bounded changed-set protocol.

Never clean up recovery data before the commit outcome is durably determined. On an uncertain I/O outcome, return `COMMIT_OUTCOME_UNKNOWN` with the operation ID and require recovery/inspection. Do not report an unqualified failure and invite a blind retry.

### 12.4 Recovery

Recovery takes the same lock and is explicit, including at coordinator startup:

| Observed state | Required behavior |
| --- | --- |
| Staging exists but no durable pending slot was published | No canonical publication was permitted yet. Preserve the base generation and clean abandoned staging safely. |
| Pending slot exists; committed manifest is the recorded base | Restore every before-image, remove paths created by the interrupted mutation, durably restore the old coherent state, then remove the slot. |
| Pending slot exists; committed manifest matches the recorded next manifest | Verify committed canonical hashes and operation identity; keep the new generation and finish cleanup. |
| Manifest is neither base nor next, recovery material is damaged, or committed bytes are inconsistent | Stop with a specific corruption/reconciliation diagnostic. Do not guess, adopt indexes, or fabricate a successful operation. |

Rollback is idempotent. A second crash during rollback must still permit restoring the complete base state from the retained before-images. Delete created directories only when they are known managed paths and empty; never recursively remove unrelated content.

Initialization is special only in that the base is a valid empty repository manifest. Publish that baseline before the first canonical batch. A rollback may return an uninitialized/empty repository, not a half-created approved plan. An existing unmanifested tree requires explicit import, not automatic adoption as generation zero.

Ordinary reads encountering a pending slot return `RECOVERY_REQUIRED`. They do not silently perform filesystem repair. The application startup coordinator may deliberately invoke recovery before serving planning requests.

### 12.5 Optimistic concurrency and retries

Check expected node/question revisions inside the lock. Reject stale writes with actual versions and changed IDs; never silently use last-write-wins.

The last-operation manifest outcome supports immediate retry inspection. It is not an indefinite idempotency receipt. If another operation has committed since an uncertain request, reconcile using the current records and expected revisions rather than resubmitting an append blindly. Record durable review/answer history through version-checked operations so a lost response cannot quietly append duplicate decisions.

Long-lived deduplication guarantees and retained receipts are deferred. Add them before any later external-side-effect operation requires such a guarantee.

### 12.6 Integrity, caches, and manual edits

On a fresh repository open or explicit full-integrity check, verify canonical membership and hashes. A warm process can retain a validated snapshot and update changed documents by comparing the new manifest with its prior manifest. Readiness/approval-critical source reads verify the bytes they use against the committed manifest.

All supported writes go through the repository API. Out-of-band canonical edits are unsupported and must not be silently adopted. Detect observed mismatches and require an explicit import/reconciliation operation that applies revision and approval rules. Do not claim immediate detection of arbitrary hostile changes to every unused file while an open process is using its cache.

Approval and packet preparation cannot proceed through a known integrity error. A manual reconciliation must never preserve stale approval merely because edited JSON parses.

### 12.7 Derived views

Indexes carry child identity, title, type where applicable, source `node_revision`, and relative ref. Include a source-membership fingerprint or equivalent source-revision description; a global generation alone is not their freshness contract.

Update only changed child entries and containers whose membership/summary changed. A leaf inspection normally changes one canonical node, one manifest, the recovery slot, and at most the affected small index/summary path. It must not rewrite every index solely to stamp a new generation.

Generate a legacy preview only on an explicit preview operation or a debounced integration request. Do not regenerate a whole legacy file on every planning checkpoint. An unverified mapping returns a compatibility diagnostic instead of guessed output.

## 13. Repository operations and application integration

### 13.1 Required operation families

Names below identify capabilities, not verified symbols in the current checkout. Place them using actual repository conventions.

| Family | Slice 1 capabilities |
| --- | --- |
| Read | Open/inspect committed snapshot, resolve node, validate, inspect freshness, list planning obligations. |
| Structure | Initialize plan, create skeleton batch, create composite with children, remove an unreferenced draft under explicit rules. |
| Planning | Record inspection/checkpoint, save typed draft, revise requirements, reconcile changed basis, refresh context, request review, record review. |
| Questions | Create/update open question, list a round, answer with authority, reopen with reason. |
| Controls | Apply/release hold; record/resolve interface mismatch without inventing legacy semantics. |
| Packets/resume | Prepare planning/review packet, validate packet basis, choose/list next planning actions. |
| Maintenance | Recover pending mutation, validate full integrity, explicitly repair derived views, produce verified shadow preview. |

There is no Slice 1 operation that claims a task/leaf, marks execution complete, confirms a human action, accepts an output, or enables live mode. Attempts to request those capabilities return a specific unsupported-capability result; do not implement no-op success stubs.

### 13.2 Thin application adapter

Connect the operations to the existing planner/session and question/review flow when available. Where that integration is absent, implement a thin command adapter using the existing command framework.

It must support the real equivalents of:

```text
initialize or open a plan
list next planning actions
prepare a planning packet for a node
record inspection/checkpoint
apply a version-checked typed draft
list and answer a question round
prepare and submit independent planning review
validate state and inspect freshness
restart and resume
```

Do not invent unrelated package layouts or present hypothetical command names as already installed commands. Phase 0 selects the concrete entry points; the handoff report provides actual runnable examples after implementation.

For a fresh-context planning turn, the adapter prepares the packet, starts or addresses a fresh session using existing facilities, and accepts a version-checked proposal. It does not pass the entire earlier planning transcript by default. Starting a new session must not erase durable checkpoints.

For review, use a separate actual reviewer session or authorized human input. Tests may use deterministic fake model responses at that boundary, but must still exercise the real adapter, repository, persistence, and authorization checks.

### 13.3 Shadow compatibility

Maintain a read-only link between a tree task and its verified legacy task identity in the compatibility mapping, not one legacy task per leaf. Keep unknown legacy statuses/counters in verified source fixtures until a supported mapping exists.

No Slice 1 request may overwrite the live `.planning/assignments.json`, create a live sidecar, route a legacy write into the tree, or change the live dispatch path. Test this with a sentinel legacy file and real adapter calls, not merely by omitting a projector unit test.

Read-only consumers can be tested against a generated preview when the mapping is established. A preview remains a comparison artifact, never an authoritative live scheduling source.

## 14. Performance requirements and early measurements

Benchmark during the repository milestone, before expanding execution features or designing additional caches. Do not defer measurement to the final verification phase.

Use deterministic generated trees at approximately 100, 500, and 1,000 leaves, including nested composites, cross-branch input relationships, reviews, and a broad-impact question. Record actual shape and edge counts rather than assuming every plan has the same workload. A reported few-hundred-leaf BraiNIX plan is a useful target scale, not a measured fixture supplied here.

Measure at least:

| Operation | Measurements |
| --- | --- |
| Cold open/full integrity | Elapsed time, canonical bytes/files read and hashed. |
| Warm node lookup and planning packet | Elapsed time, authoritative read set, packet bytes and actual/estimated tokens. |
| Inspection or resume-note update | Changed canonical count, files/bytes written, synchronization calls, lock duration. |
| Independent planning approval | Same write metrics, plus validation/review-basis work. |
| Local semantic specification edit | Actual impact closure, files rewritten, elapsed time. |
| Broad question answer | Actual affected node count, invalidation cost, rollback-slot size, recovery cost. |
| Restart/resume and derived repair | Elapsed time, selected action correctness, files regenerated. |

Record hardware, OS/filesystem, toolchain, cold versus warm conditions, and sample count. Use enough repeated warm samples to report useful median and tail observations; do not invent percentiles from a single run. Absolute latency targets are not established by the source documents and must not be claimed as measured guarantees.

Binding structural performance checks:

- A non-semantic leaf checkpoint changes only that leaf's canonical document, not every descendant/ancestor canonical record.
- A warm unrelated edit does not rehash all unchanged canonical files solely to update the manifest.
- A new generation does not force every index to be rewritten.
- Semantic invalidation cost may scale with its real impact closure.
- A cold integrity check may intentionally scan the tree.
- Packet context size is independent of full-plan serialization; an oversized necessary closure produces a diagnostic, not silent truncation.

If the minimal implementation fails these checks, fix its write/read amplification before proceeding. Do not substitute a database, distributed service, or generalized event engine without a separate explicit design decision.

## 15. Slice 1 implementation milestones

Implement test-first using the real checkout's build/test commands. Each milestone produces reviewable code, focused tests, and an evidence note. These are development milestones, not instructions to create another workflow project.

### S1-0: Revalidate integration facts

**Work:** Reuse and verify the reported scan; inspect actual legacy fields/statuses, root resolution, callers/writers, planner/question/review entry points, session authority, filesystem primitives, and tests.

**Deliver:** Compatibility ledger, sanitized fixtures, concrete package/adapter placement, supported local durability boundary, commands, and named unknowns.

**Exit:** Task ownership and all known mutation/dispatch paths are documented. Unknown legacy mappings remain explicit preview/live blockers, not blockers to independent planning work.

### S1-1: Models and pure planning rules

**Work:** Implement schema/version checks, all 17 typed specs, envelope constructors, structural documents, questions, approval policy, reference/graph validation, effective spec impact, planning stages, and derived structural summaries.

**Deliver:** Complete valid/invalid fixtures, round-trip tests, planning transition tests, review authorization tests, and graph/task-profile diagnostics.

**Exit:** Skeletons persist without becoming ready; inspection differs from approval; semantic and editorial changes have distinct effects; source-type mistakes and impossible references fail clearly.

### S1-2: Minimal repository and early performance gate

**Work:** Implement cross-process locking, expected revisions, changed-set publication, one pending rollback slot, commit manifest, restart recovery, explicit derived repair, and snapshot loading.

**Deliver:** Fault-injection tests at every boundary, multi-process conflict test, canonical-integrity checks, and the first performance report from section 14.

**Exit:** Interrupted mutations recover to a complete old or complete committed new state. A question answer cannot leave obsolete approval authoritative. Non-semantic writes do not scale with total canonical file count. Investigate amplification now, not after the next slice.

### S1-3: Packets, questions, review, and actual resume path

**Work:** Implement packet purposes/basis/budget handling, next-action selection, thin planner/command integration, structured question rounds, and independent planning-review submission.

**Deliver:** The full vertical scenario in section 2.2 through a real adapter and separate processes; actual command/API examples; deterministic integration fixtures with trusted actor boundaries.

**Exit:** A new process/session resumes the correct work from committed state, unrelated approved branches remain intact, and no new-tree execution path exists.

### S1-4: Compatibility preview, regression, and handoff

**Work:** Complete supported shadow mappings against verified fixtures, exercise actual legacy decoders where applicable, run regression/build/race checks supported by the checkout, and repeat representative performance cases.

**Deliver:** Source changes, fixtures, actual verification results, performance report, compatibility gaps, user-facing planning/resume instructions, and explicit deferred work.

**Exit:** All required Slice 1 acceptance tests pass. A missing optional legacy preview is reported precisely if the source mapping is unavailable; live mode remains impossible. Do not call a required planning/resume, authorization, or recovery test optional.

## 16. Mandatory Slice 1 acceptance tests

These tests add to the existing regression suite. They do not claim execution functionality from later slices has been implemented.

| ID | Scenario | Required result |
| --- | --- | --- |
| S1-AT-01 | Create and round-trip a leaf skeleton with null work/spec. | Valid draft; unapproved; no execution authorization. |
| S1-AT-02 | Decode every concrete leaf type and wrong-type/malformed alternatives. | All 17 correct discriminants pass; malformed or unknown contract fields fail. |
| S1-AT-03 | Use an unsupported schema version/capability or runtime field. | Actionable rejection; no silent migration or dropped execution data. |
| S1-AT-04 | Parent ref reaches a real but incorrect sibling/ancestor, or managed path escapes/is symlinked. | Structural/containment failure at the actual field. |
| S1-AT-05 | Composite has no child, leaf has children, or ID disagrees with its path. | Draft validation rejects the shape. |
| S1-AT-06 | Index omits a child or reports obsolete status. | Canonical state determines the read; stale index cannot authorize approval; explicit repair restores the derived view. |
| S1-AT-07 | Inspect a leaf and restart. | Inspection survives; drafting/approval obligation remains outstanding. |
| S1-AT-08 | Read or inspect an already approved leaf again. | Execution status is not reset and semantic approval is not lost solely because of inspection. |
| S1-AT-09 | Author session attempts to approve its own draft or impersonate a human. | Rejected by the trusted caller/session boundary. |
| S1-AT-10 | Independent reviewer approves a complete current spec; human authorizes reserved scope. | Correct authority and exact basis are recorded; the leaf becomes planning-reviewed, not executed. |
| S1-AT-11 | Review requests changes, corrected draft is submitted, new review approves. | Both verdicts remain attributable; corrected drafting is not falsely labeled completed execution. |
| S1-AT-12 | Submit draft/review from an obsolete packet. | Conflict identifies changed node or basis; newer state is not overwritten. |
| S1-AT-13 | Change governing or input requirements. | Affected descendants/cross-branch consumers require reconciliation and renewed review; unrelated approvals remain current. |
| S1-AT-14 | Refresh only the digest after a semantic change. | Obsolete approval is not restored. |
| S1-AT-15 | Make an editorial digest-only change. | Context propagation is reported without unnecessarily rewriting semantic approval or every descendant file. |
| S1-AT-16 | Display a recommendation without a response. | Question stays open; no answer or authority is synthesized. |
| S1-AT-17 | Answer a multi-select question, then restart. | Stable selected IDs, free-text rules, authority, and history round-trip correctly. |
| S1-AT-18 | Select unknown/duplicate options, multiple options on single-select, or prohibited text. | Answer validation fails. |
| S1-AT-19 | Add/reopen/change a blocking question versus edit an unrelated recommendation. | Correct impact closure invalidates approval only for substantive affected decisions; unrelated recommendation does not rewrite requirements. |
| S1-AT-20 | An informational question has empty affects, or a governing question is answered by an unauthorized planner. | Informational question requires rationale and does not hold the plan; unauthorized governing answer is rejected. |
| S1-AT-21 | Ordinary, containment, implicit-reference, or review relationship creates a cycle. | Actual expanded cycle and participating relations are reported. |
| S1-AT-22 | Fine-grained plan is valid but whole-task projection requires unsupported interleaving. | Distinct task-profile blocker; no silent per-leaf dispatch or dependency removal. |
| S1-AT-23 | Task and API-binding interface flags are set/resolved. | Correct scope/reason/authority is preserved; unresolved affected flags prevent approval. |
| S1-AT-24 | Approved branch coexists with unfinished or human-blocked independent branch. | Resume can select runnable independent planning work; it does not spin on the blocked branch. |
| S1-AT-25 | Two processes save incompatible drafts from the same expected revision. | One succeeds; the other receives a conflict without losing the first draft. |
| S1-AT-26 | Crash before a durable pending slot. | Base state remains authoritative; abandoned staging is safe to remove. |
| S1-AT-27 | Crash after any canonical replacement but before manifest commit. | Recovery restores all before-images, including question/approval consistency. |
| S1-AT-28 | Crash after manifest commit but before cleanup, or during recovery itself. | New committed state is verified or rollback resumes idempotently as appropriate; no mixed generation is served. |
| S1-AT-29 | Recovery metadata or canonical committed bytes are inconsistent. | Fail closed with a diagnostic; no reconstruction from an index or silent acceptance of hand edits. |
| S1-AT-30 | Immediate retry follows a lost mutation response. | Outcome can be inspected without a duplicate review/answer; no indefinite receipt guarantee is falsely claimed. |
| S1-AT-31 | Prepare a draft packet, a review packet, and request an execution packet. | Correct planning payloads; execution request returns capability-not-enabled. |
| S1-AT-32 | Required packet content exceeds budget. | Oversize diagnostic; required constraints/specification are not truncated. |
| S1-AT-33 | Cold restart without the earlier conversation or in-memory cursor. | Correct current planning action and checkpoint are reconstructed from committed records. |
| S1-AT-34 | Thin application adapter runs the full section 2.2 scenario. | Real persistence, packet, question, review, and resume path work end-to-end, not only repository unit tests. |
| S1-AT-35 | Representative 100/500/1,000-leaf benchmark fixtures are exercised. | Actual metrics and amplification checks are reported during S1-2. |
| S1-AT-36 | Inspect one leaf or save a non-semantic resume note. | Only changed canonical state is written; whole-tree index regeneration/hash rescanning is not caused by generation stamping. |
| S1-AT-37 | Preview encounters legacy corrected/revision_rounds/flag mappings. | Verified semantics are retained; unknown meanings produce a blocker rather than a guessed completion state. |
| S1-AT-38 | Whole-task preview spans several leaves. | Where mapping is verified, identity/metadata/summary do not collapse to the first leaf; an unsupported mapping returns an explicit preview blocker, not a fabricated row. |
| S1-AT-39 | All Slice 1 adapter operations run with a sentinel live assignments file. | Live bytes, live sidecar state, and executor path remain unchanged. |
| S1-AT-40 | Request claim, completed result, human confirmation, promotion, or live activation. | Explicit unsupported capability; planning approval is never presented as physical action or accepted execution. |

## 17. Deferred contracts that the next slices must preserve

These are constraints on later work, not hidden Slice 1 deliverables.

### 17.1 Slice 2: Task ownership and execution evidence

A task claim owns its leaf execution and progress. Fencing and current-spec checks must protect late submissions at task and affected-leaf boundaries; a task token alone must not allow a result for a leaf whose specification changed.

Successful work produces a candidate, not necessarily accepted completion. Ordinary dependents require current accepted output. Required reviewers consume an exact candidate and bind their verdict to its spec/result revision. A failed review requests rework; a reviewer tool crash is not a finding that the product failed. Re-review after a changed candidate must retain old verdicts.

Task completion requires all required leaf outcomes and task-boundary acceptance evidence. A skipped leaf is not success. Human completion requires an actual authorized human workflow and attributable evidence. A task owner may wait for human work but cannot attest to it merely by being an agent.

Result/acceptance changes must invalidate actual downstream inputs independently of specification changes. Keep `result_current` distinct from `acceptance_current`. Pure context refresh cannot repair either.

Define exact task claim/lease/result schemas in the Slice 2 handoff and version the data explicitly. Do not reuse legacy `revision_rounds` as a new execution attempt or requirements counter without a verified mapping.

Any requirement for external-side-effect idempotency, durable receipts, or stronger recovery must be implemented before the operation depending on it is enabled. A future slice label is not a safety exemption.

### 17.2 Slice 3: Promotion and live mode

Promotion must retain the original node ID/path and preserve the former leaf's type, work, specification, source spec revision, and verification obligations as a composite boundary contract. Existing references continue to refer to that boundary, not an arbitrary child. The operation must not discard old evidence or convert it into a new aggregate success.

Review the need for a general roll-forward journal and retained receipts using measured runtime behavior. They are not required merely to imitate v3, but any claimed guarantees must have implementation and recovery tests.

Live activation requires exact legacy mappings, all writers routed or disabled, all dispatch paths guarded, verified task ownership, capability/version compatibility, a consistent import, and a coordinated rollback plan. Preserve a recoverable pre-cutover legacy snapshot. Two independently writable sources of truth are never a rollback strategy.

Read-only display compatibility does not prove dispatch compatibility. Explicit user authorization is required for production cutover even after every technical gate passes.

## 18. Definition of done and handoff instructions

Slice 1 is complete when the implementer delivers:

- Strict versioned models for the retained hierarchy and all 17 leaf types, plus questions and independent planning-review records.
- A tested distinction between inspection, drafted specification, planning approval, and unavailable execution evidence.
- Effective specification/context freshness, scoped question impact, and explicit legacy compatibility findings.
- The minimal serialized repository with tested rollback, integrity, conflicts, and changed-set I/O.
- Compact revision-bound planning/review packets and a deterministic, restart-safe planning resume path through a real adapter.
- Early performance evidence, regression evidence, actual usage examples, and a verified unchanged live legacy path.

Report actual commands and outcomes using passed, failed, blocked, skipped, and not-run distinctions. Do not claim runtime execution, claims, accepted results, live compatibility, physical verification, or production readiness from planning tests.

A compatibility gap may remain explicitly disabled in shadow mode. A missing required planning/resume, authorization, or persistence guarantee means Slice 1 is incomplete and must be reported as such.

### Instruction to the implementing agent

```text
Implement assignments-schema-spec-v4.md, Slice 1 only.

Read this specification fully. Revalidate the existing legacy scan against the
current checkout, record the exact compatibility facts, and follow the selected
repository conventions. Preserve unrelated changes.

Build test-first through S1-0 to S1-4. Keep reviewed as planning-approved and use
separate revision-aware planning progress. Preserve task-level scheduling and
ownership; do not introduce per-leaf dispatch. Implement the question-round
fields, independent planning-review authority, effective spec_revision rules,
compact packets, and a real fresh-context planning/restart/resume path.

Use the minimal changed-set repository and its one pending rollback slot.
Benchmark at S1-2. Do not substitute the full v3 journal/receipt system or defer
correctness needed by the current slice.

Keep the live assignments file and executor unchanged. Do not implement live
cutover, product execution, physical confirmation, leaf promotion, or a new
PhaseFlow project. Return the implementation, actual test/performance evidence,
usage examples, compatibility blockers, and clearly deferred capabilities.
Do not commit, push, deploy, or cut over production without authorization.
```

### Source and change notes

This handoff derives its compact-context objective, recursive layout, original lifecycle terminology, and leaf taxonomy from `assignments-schema-spec.md` (v2), especially its Context, sections 1-4, and structural schemas. It retains v3's separation of authoritative and derived data, planning versus execution validation, explicit work/verification, and freshness/provenance concerns.

V4 replaces v3's first-release scope, per-leaf claim decision, requirements-version naming, eager whole-tree view publication, undefined planning-approver policy, and question shape. It adds explicit planning-progress records and a working resume slice. It deliberately uses a limited rollback publication protocol rather than claiming that write-then-rename alone is a multi-file transaction.

The reported legacy task statuses, counters, consumers, and scheduling behavior remain checkout-verification requirements. No execution results, performance measurements, repository paths/symbols beyond the supplied planning layout, or production compatibility are claimed to have been verified in this document.
