# Brief workflow roadmap (M1 to M6)

Design: `docs/superpowers/specs/2026-09-19-brief-workflow-design.md`.
Each milestone gets its own detailed plan when the previous one has landed,
because it consumes that milestone's real interfaces. Only M1 is detailed now:
`2026-09-19-plantree-m1-store.md`.

## Milestones

| # | Deliverable | Package / files | Depends on |
|---|---|---|---|
| M1 | Tree store: nodes, ids, statuses, atomic locked saves, verify, summaries, `NextActions` | `gophermind-lib/plantree` | none |
| M2 | Pass 1 (skeleton): chunk the brief by heading, one fresh-context pass per chunk, merge nodes and overview into the tree | `gophermind-lib/plantree/plan` (chunker, pass runner, overview) | M1 |
| M3 | Pass 2 (spec): one fresh-context pass per task fills each step's work, acceptance criteria and test command | `plantree/plan` | M1, M2 |
| M4 | Questions: `questions.json` store; passes add questions with options, multi-select, recommended, free text | `plantree/questions` | M1 |
| M5 | Question-round UI in the TUI, growing textarea, answers re-plan only affected nodes (`needs_reconciliation`) | `gophermind-lib/tui` | M3, M4 |
| M6 | Approval, export to legacy `assignments.json` (task-level rows), `/project <name> <brief>` and resume wiring | `plantree/export`, `tui/project.go` | M1 to M5 |

## Contracts M2 to M6 rely on (produced by M1)

```go
package plantree

const SchemaVersion = 4
const RootID = "plan"

type Kind string   // KindPlan, KindPhase, KindTask, KindStep
type Status string // StatusUntouched ... StatusEscalated
type Stage string  // StageSkeleton, StageInspected, StageDrafted,
                   // StageAwaitingAnswers, StageNeedsReconciliation, StageApproved

type Work struct {
	Description        string   `json:"description"`
	TargetPaths        []string `json:"target_paths"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TestCommand        []string `json:"test_command"`
}

type Node struct { /* see M1 plan, node.go */ }

func ParseID(id string) (Kind, error)
func ParentID(id string) (string, error)
func ChildID(parent string, n int) (string, error)

func Open(planningDir string) *Repo
func (r *Repo) Init(root Node) error
func (r *Repo) Create(n Node) error
func (r *Repo) Get(id string) (Node, error)
func (r *Repo) Children(id string) ([]Node, error)
func (r *Repo) Update(id string, expectedRevision int, mutate func(*Node) error) (Node, error)
func (r *Repo) Walk(fn func(Node) error) error
func (r *Repo) Verify() error
func (r *Repo) Summarize(id string) (Summary, error)
func (r *Repo) NextActions() (Actions, error)
```

`plantree` must not import `phaseflow` (M6's export package imports both, so
the dependency only points one way). Callers pass
`phaseflow.PlanningDir(root)` to `Open`.

## Sizing rules every pass obeys (from the overflow incident)

- A pass input is the instruction, the overview (capped), and one bounded
  slice. If the slice exceeds the budget it is split, never truncated.
- `read_file` is capped at 32 KB per call; passes are told to read by range.
- A pass returns strict JSON that is validated before anything is written.

## Later layers (not in M1 to M6)

Independent-reviewer authority and task-batched review (v4 addendum section
2), rollback slot and manifest, typed leaf specs, promotion, fenced claims.

## M1 outcome (landed on branch `feat/plantree-m1`, 2026-09-19)

Package `gophermind-lib/plantree`, commits `adfed48`, `0f827f1`, `f0e07e7`,
`cb3cd41`, `7c77722`, `1b5b0ba`. Tests, race run, gofmt, vet and the whole
module build are clean. Deviations from the contracts listed above:

- `NextActions` is total: an empty result means the plan is complete. Steps
  with status blocked, delayed, escalated, failed, needs_revision or skipped
  appear in `Blocked` as `ActionHeld` with `<status>: <reason>`.
- `Counts` also carries `Failed` and `NeedsRevision`.
- `approve` is offered only when nothing is runnable or blocked.
- `Children` skips directories whose id is not valid at that position.

### Carry-forward decisions for the M2 and later plans

1. **Consistent multi-node access.** Reads take no lock, and the write lock is
   private. M2 writes many nodes per pass, so its plan must decide whether to
   export a lock (which needs unlocked internal variants of `Create` and
   `Update`) or accept that a second process can read a skewed tree. A callback
   passed to `Update` must never call the repo (the lock is not reentrant).
2. **Child id allocation.** `ChildID(parent, n)` needs the caller to know `n`;
   `len(Children)+1` races. `Create` fails safely with `ErrExists`. M2 needs a
   retry loop or a `NextChildID` computed under the lock.
3. **`context_digest` is required on every node.** Every pass must supply one.
4. **No node or subtree removal.** A skipped step blocks approval forever
   (v4 spec 7.3). M5's re-planning needs either a `Remove` operation or a
   distinction between "skipped, still counts" and "removed from scope".
5. **`Summarize` derives only `reviewed` or `untouched`.** M6's export needs
   task-level rollups over execution statuses, and a `completed` step currently
   makes the plan look unreviewed again.
6. **`Verify` allows a dependency on an ancestor or structural node.**
7. **Durability.** `lockfile.WriteAtomic` does not fsync the directory. A power
   loss can lose the last committed write but cannot corrupt one.
8. Deferred minors, none blocking M2:
   - No test that a corrupt member file still makes `Children` return an error.
   - A step with status `reviewed` but stage `drafted` still leads to
     `approve` (correct: approving moves it to `approved`).
   - No `Repo.Dir()` accessor; `Init`/`Create` misuse returns plain errors, not
     sentinels.
   - A failed `Update` is tested for an unchanged revision, not byte-identical
     content; the concurrency test cannot prove true parallel contention.
   - No diamond or self-dependency test for `Verify` (traced correct by hand).
   - Commit `adfed48` carries a `Claude Haiku 4.5` co-author trailer instead of
     `Claude Sonnet 5`; it names the true author, so the history is left as is.
