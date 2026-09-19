# Assignment tree v4: owner decisions and amendments

Applies to `2026-09-19-assignments-tree-v4-spec.md` (copied unchanged from
`assignments-schema-spec-v4.md`). Where this addendum and the spec differ, this
addendum wins. Decided 2026-09-19 by the project owner.

## 1. Status semantics (explicit)

| Concept | Meaning |
| --- | --- |
| `planning.stage: inspected` | The planning material was read or assessed at a recorded revision. It does not approve the specification. |
| `status: reviewed` | The current specification passed authorized planning review. It does not mean execution started or completed. |

- The Slice 1 adapter output exposes both separately, including whether the
  approval is current or stale. Graphical presentation belongs to the later UI
  deliverable.
- Opening or inspecting an already approved node never downgrades its stage.
- This overrides the owner's earlier working definition of `reviewed` as
  "read but not started". `inspected` carries that meaning.

## 2. Task-batched planning review (new requirement)

Independence means independent of the author, not a new reviewer session per
leaf.

> A reviewer session may review multiple leaves belonging to one task. Shared
> governing requirements and input specifications are supplied once where
> practical. Each leaf receives its own decision, findings, and exact
> specification-basis reference.

- Batch the model work, not the approval semantics. A task-wide "looks good"
  never approves unexamined leaves.
- A reviewer never approves a leaf it helped author.
- A large task is split into context-bounded batches without truncating any
  requirement.
- Slice 1 persists each decision through the existing per-node review
  operation. No new all-or-nothing batch transaction. A stale submission for
  one leaf is rejected without discarding approvals already recorded for
  others.

## 3. Slice 1 scope statement

Slice 1 delivers planning storage, packets, questions, review recording,
resumption, and a thin adapter. Its acceptance demonstration is persisted
planning and correct resumption. It does not deliver automatic decomposition
from a brief or the growing-textarea question UI, and must not be described as
doing so.

The next deliverable, under its own small specification, is the
**brief-to-plan decomposer and question-round UI**: turn a brief into the
phase/task/step hierarchy, the growing editable answer text, question rounds,
and answers applied through the existing revision and reconciliation rules,
with inspection and approval shown separately.

## 4. Locking

Keep the single exclusive lock for Slice 1. Add lock-wait and lock-hold
measurements to the S1-2 benchmarks, including repeated reads while planning
mutations occur. Revisit shared snapshots or reader/writer locking in the
execution integration only if those measurements justify it.

## 5. Root fields

The root values are defined in v4 section 4.2:
`capabilities: ["planning"]`, `execution_mode: "shadow"`. Slice 1 rejects any
other value. Section 5.5 cross-references section 4.2, and the root-schema tests
cover these restrictions.
