# The grilling skill and the question model

Date: 2026-10-07. Status: review and proposal for John. No code was changed.

## 1. What was reviewed

Three skills in `~/.claude/skills/`:

| Skill | What it is |
|---|---|
| `grilling` | The real skill (1987 bytes). Everything below describes this one |
| `grill-me` | An alias with `disable-model-invocation`. Its whole body is "Call the Skill tool with grilling" |
| `grill-with-docs` | An alias that calls `grilling` and `domain-modeling`, so the interview also writes ADRs and a glossary |

Compared against two question models: v1 (`plantree/plan/questions*.go`, the TUI question round) and v2 (the planner's Clarify stage, the `QUESTION:` mid-stage protocol, and `human.Gate` in `briefv2/human`). Source for the v1 and v2 behaviour is the review in `2026-10-07-planner-executor-v1-v2-review.md`, plus a direct read of `human/human.go` and `planner/prompts/clarify.md`.

## 2. What the skill says

1. The decisions form a **design tree**: every decision branches into the decisions that hang off it.
2. Work in **rounds**. The **frontier** is every decision whose prerequisites are settled. Ask the whole frontier at once, numbered, each with a recommended answer. Wait.
3. Each round's answers reshape the tree. Recompute the frontier. A question that depends on another open question in the same round belongs to a later round.
4. **Facts are the agent's job.** If a question needs something from the environment, look it up (a sub-agent in the skill). Never ask the user for what can be looked up. Only questions downstream of a running lookup wait.
5. **Decisions are the user's.** Put each and wait.
6. **Done** means the frontier is empty: every branch visited, nothing silently assumed. Do not act until the user confirms shared understanding.

## 3. Where each model stands

| Grilling rule | v1 | v2 | Gap |
|---|---|---|---|
| Design tree with dependencies | Question `affects` links a question to the steps it holds. No question-to-question dependency | None. Clarify returns one flat JSON array | Both lack the tree |
| Ask the frontier in rounds | All open questions shown at once in one round | One round at Clarify. Later, `QUESTION:` pauses a single call, at most 3 | Neither recomputes the frontier after answers |
| Recommended answer on every question | Options 2 to 8 with a recommendation, validated against the option labels, never preselected | `default_if_unanswered` per question; `Options` on the gate type. Used automatically in assume mode | Different meanings: v1's is advice, v2's is an automatic fallback |
| Facts are looked up, not asked | None (and repository facts are never written in production) | Clarify prompt says "do not ask what the brief states" but no code answers a fact question | Neither separates fact from decision |
| Done when the frontier is empty | Approval blocks on open questions | Clarify ends after one round or the model's `[]` | No test that the tree is exhausted |
| Confirm shared understanding | Approval of the plan | Approval of the plan (hash) | Understanding is never confirmed on its own, only the finished plan |
| Answers change the plan | `ChangeAnswer` re-plans only affected steps and spares started ones | Answers stored in `answers.json`; not tied to nodes | v2 cannot say which nodes an answer shaped |

Keep from v1: the option model, recommendation validation, dedup, bounded answer history, `ChangeAnswer`. Keep from v2: the `QUESTION:` pause-and-resume, cached answers, `on_ambiguity` modes, the `Assumed` flag, the gate adapters (terminal, file, programmatic), and the rule that Clarify has brief scope and so never reaches a public provider.

## 4. Proposed integration

### 4.1 Question record (one type for Clarify, mid-stage, and the gate)

| Field | Meaning |
|---|---|
| `id`, `text`, `why_it_matters` | as v2 today |
| `kind` | `decision` or `fact` |
| `depends_on[]` | ids of questions that must be settled first (model-written, code-validated: ids exist, acyclic, only earlier ids) |
| `options[]`, `recommended`, `recommended_why` | one recommendation field replaces v1's recommendation and v2's `default_if_unanswered` |
| `if_unanswered` | `require` (a person must answer) or `take_recommended` (unattended or assume mode) |
| `affects[]` | node ids and requirement ids the answer shapes, filled when the answer lands |
| `raised_by` | `clarify`, or the stage and node that asked mid-stage |

A `fact` question is answered by code, never shown to a person, when a probe can answer it (section 4.3). A `decision` question is always the person's.

### 4.2 Clarify becomes rounds

1. Round 1: one model call with the brief and the fact probe output. The reply is the full question list with `depends_on`. Code computes the frontier (questions whose prerequisites are all settled) and shows only those, numbered, each with its recommendation.
2. After the answers, code marks them settled and recomputes the frontier. Questions that were waiting on them now show. If the frontier is empty but the model may have new questions, make one more call: "given these settled decisions, what new questions are unblocked or raised?" It returns `[]` when there are none.
3. Stop when the frontier is empty and the model's last call returned `[]`, or when a cap is reached (default 4 model calls and 30 questions, both settings). A cap stop lists the unvisited branches as `open_questions`, never silently drops them.
4. Unattended mode (`assume_and_document`): every question takes its recommendation, marked `Assumed`, recorded as an assumption on each node it affects. The rounds still run, so dependent questions still appear.

The `QUESTION:` mid-stage protocol keeps its pause-and-resume. Its questions join the same tree as late branches with `raised_by` set, instead of living in `_state/question.json` alone. The limit of 3 per call stays.

### 4.3 Facts are probed by code

The grilling skill dispatches a sub-agent. The Go planner has no sub-agents, so the equivalent is a deterministic **fact probe** run before round 1: whether the target repo exists and is a git repo, `go.mod` module path and Go version, existing packages and exported names, toolchain on `PATH`, secrets declared in the vault, hosts in the brief. The probe output goes into the Clarify prompt as a "Known facts" block, and the prompt rule becomes "do not ask what the brief or the facts state". A model question tagged `fact` that the probe can answer is answered by code and logged. The probe is private to the machine and sends nothing to a provider that Clarify's brief scope would not already reach.

### 4.4 Confirm shared understanding

After the frontier is empty, render an **Understanding** document: settled decisions with answers, assumptions taken, open questions (must be none to proceed), and the sentence "Frontier empty: every branch visited". A new gate method `ConfirmUnderstanding` asks the person to confirm it. Contract does not start without it, and its hash goes into `approval.json` next to the plan hash, so a plan approval proves the understanding it was built from. `--yes` records `approved_by: flag` as it does now.

### 4.5 Answers flow into nodes

Note (2026-10-08): refined by the B spec. Changing an answer restarts the planner from the stage that used it, and the full question and answer records are also written into the node tree (root: the whole conversation; each node: the records it cites). See `docs/superpowers/specs/2026-10-07-node-widening-and-question-model-design.md`, sections 7.7 and 7.8.

Each node gets `decision_ids[]` (the answers that shaped it), alongside the new `assumptions[]` and `open_questions[]` fields in the widened node schema. This is the v1 `ChangeAnswer` semantics made precise: changing an answer re-plans only the nodes that cite it, spares verified leaves unless the answer changes a signature (which is `contract_flagged`, as in the executor), and adds a new revision to the affected nodes only.

### 4.6 Files

Everything is browsable and goes in the run folder, consistent with the file-state decision:

```text
.gophermind/<run>/
  _state/clarify/questions.json     the tree: all questions, depends_on, status, answers
  _state/clarify/rounds.jsonl       one line per round: asked ids, answered ids, who answered, when
  _state/clarify/facts.json         the fact probe output
  UNDERSTANDING.md                  the rendered document the person confirms
  decisions/<question-id>.md        one ADR-style record per settled decision (question, options, answer, why, nodes affected)
```

`decisions/` is the part of `grill-with-docs` worth keeping: the grilling skill alone gives the interview, and the decision records give the audit trail the node's `decision_ids[]` points to.

## 5. How the terminal shows a round

The skill formats a round with emoji markers. The product's terminal and file adapters should not, to match the project's no-emoji rule. A round renders as plain labelled blocks:

```text
Q1  <title>
    <body, options listed 1..n>
    Recommended: <option> (<why>)

Q2  <title>
    ...
```

The file gate writes the same blocks to `questions-round-N.md` and reads `answers-round-N.md`; the programmatic gate sends the same structure as data. A blank answer accepts nothing: the person must type an option number, the word `accept` (take the recommendation), or free text, which keeps v1's rule that a recommendation is never preselected.

## 6. Costs and risks

- More model calls on a slow local model. Bounded by the round cap; the common brief should need 2 to 3 calls.
- The dependency graph is model-written. Code validates it (ids exist, acyclic, earlier only) but cannot judge whether a dependency is real. A wrong dependency delays a question by a round; it cannot drop one, because the stop rule requires an empty frontier.
- `kind: fact` misuse: a model can label a real decision as a fact to avoid asking. Mitigation: the probe answers only what it can actually compute, and an unanswered `fact` question is shown to the person as a decision.
- Changes to existing code: `human.Question` and `Answer` gain fields (the gate adapters change), `planner/clarify.go` becomes a loop, the `QUESTION:` store moves into the tree, `approve.go` hashes the understanding, and the node schema gains `decision_ids`. The executor is unaffected.

## 7. Decisions for John

1. Adopt rounds with a dependency tree for Clarify (section 4.2), or keep one round and only add recommendations and the fact probe. Recommended: rounds.
2. Add the confirm-understanding gate (section 4.4). Recommended: yes, and hash it into approval.
3. Keep `decisions/*.md` records (section 4.6). Recommended: yes.
4. Round caps: 4 model calls and 30 questions. Adjust?

Placement in the order agreed on 2026-10-07: this is sub-project B (node widening and the question model) in the unification spec, built before `/project` on v2.

Interaction with `/project` as the single entry point (`2026-09-30-project-single-entry-design.md`, section 8, on the branch): the attended form runs the rounds with a person at the gate. The unattended form takes every question's recommendation, marks it `Assumed`, records it on each affected node, and records `ConfirmUnderstanding` as `approved_by: unattended` with the understanding hash. The rounds still run unattended so that dependent questions still appear. That spec's rule that a graded attempt fails if GopherMind stops to ask a human is unchanged: no round may block in the unattended form.

Status: decisions 1 to 4 in section 7 were answered yes by John on 2026-10-07 (rounds with a dependency tree, the confirm-understanding gate, the decision records, the caps of 4 calls and 30 questions). The design is now `docs/superpowers/specs/2026-10-07-node-widening-and-question-model-design.md`, which refines section 4.5 above: changing an answer restarts the planner from the stage that used it, and `decision_ids` on nodes serve traceability, not selective re-planning, in the first version.
