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

## M2 outcome (landed on `main`, 2026-09-19)

Package `gophermind-lib/plantree/plan`, ten commits: `d9fe347`, `18f0128`, `33387b9`, `b87c238`, `3d39701`, `35ad350`, `e43ebb7`, `545fcf9`, `1dd891a`, `715781e`. Tests, race run (short), gofmt, vet and the whole-module build are clean. `RunPass1(ctx, repo, brief, completer, opts)` reads a brief in bounded chunks, one fresh-context pass per chunk, and builds the skeleton tree and `overview.md`. It resumes after any error from the first unprocessed chunk, refuses a changed brief or chunk size, and an end-to-end test drives it through the real `ClientCompleter` over HTTP with streamed replies.

Worst-case prompt is about 24 KB (roughly 6k to 8k tokens with the default caps: chunk 12,000 bytes, overview 6,000, outline 4,000). That fits a 32k window easily and is not safe on an 8k window. `Options.ChunkBytes` documents the budget, and a server context-limit error now says to lower it. Nothing derives the size from the model yet.

### Carry-forward decisions for M3 to M6

**M3 (pass 2, fill each step's work spec)**
1. **Provenance from node to brief chunk is not recorded.** This is the largest gap. Pass 2 sees only a node's title, digest, objective and the overview. Store a `plan`-owned sidecar in `_state/` mapping chunk index to created node ids (written by the caller of `Merge`), rather than extending `plantree.Node` (its decoder rejects unknown fields and pins schema version 4).
2. **Export `ReadBrief`.** `brief.md` is the resume input (a resume must re-supply an identical brief) but its filename is private.
3. Parameterize `loadState`, `saveState` and `statePath` by filename (pass 2 needs a second cursor), and extract the ask, parse, one correction retry loop into a shared helper.
4. The pass-2 validator must guarantee a non-empty `Work.Description` and at least one acceptance criterion before it calls `Repo.Update`, because `Validate` enforces them for drafted steps and a failure would come after the model call is paid for.
5. `ExtractJSON` returns the first valid JSON object even if it is not the plan (for example a stray `{}` in prose); try the next valid candidate when `ParsePass1` fails.
6. Group `ActionDraft` actions by task (M3 runs one pass per task).
7. `Pass1Prompt` takes five positional arguments; convert to an options struct before M3 and M4 add more.
8. Smaller items: wrap `Children` and `Create` errors with context, pin `Merge`'s discard-on-title-match behavior with a test and document that its input must come from `ParsePass1`, `Outline` truncation is gappy at scale, the prompt says "characters" while the cap is bytes, propagate context-cancel from the compress call.

**M4 (questions):** `Pass1Output` rejects unknown fields, so adding `questions` to the prompt and the parser must ship together.

**M6 (wire into `/project`, approve, export)**
1. Take the run lock: `lockfile.Acquire` on `_state/pass1.lock` around `RunPass1` (about six lines). A pass-2 runner must share the same lock.
2. Validate `Options` (reject an `OverviewCap` under about 200; `FitOverview` returns more than `cap` bytes below 22) and read the project name from the tree, not from `Options`.
3. Export progress: `Result` reports chunks processed by one call only; the cursor is private.
4. Derive `ChunkBytes` from the model's context window (`llm.Capabilities`), leaving room for the reply.
5. Brief text sits between literal `<<<BRIEF PART` markers; harden against a brief that contains the closing marker before accepting an untrusted argument.
6. Smaller items: CRLF blank lines are not paragraph breaks in `cutPoint`, section text is built with `+=` (quadratic for a huge heading-free section), `ensureChild` re-reads every sibling per node, `Result.Created` undercounts a chunk that fails after a partial merge.
7. Still open from M1: `Summarize` derives only `reviewed` or `untouched`, there is no node removal (a skipped step blocks approval forever), and `lockfile.WriteAtomic` does not fsync the directory.

## M3 outcome (landed on `main`, 2026-09-19)

Package `gophermind-lib/plantree/plan`, ten commits: `5ba3cd4`, `4765b9b`, `134c549`, `36529ad`, `e486737`, `53e326c`, `fc69768` (the seven tasks) and `5988ec4`, `fa5275e`, `3b7cca5` (final-review fixes). Tests, race run (short), gofmt, vet and the whole-module build are clean. `RunPass2(ctx, repo, completer, opts)` writes the work specification (description, target paths, acceptance criteria, test command, dependencies) of every step that lacks one: one fresh-context pass per batch of at most 6 steps of one task, in tree order. It keeps no cursor (the work list is the steps still at stage skeleton or inspected and not on hold), so calling it again after any error continues with what is left, and a step reaches stage drafted only when its whole reply is valid. Pass 1 now records which brief chunk produced each node in `_state/provenance.json`, so a task's pass sees only the brief text that gave rise to it. An end-to-end test drives pass 1 then pass 2 through the real `ClientCompleter` over HTTP and ends with `NextActions` offering only approval.

Worst-case pass-2 prompt with the defaults: 25,553 bytes (ASCII) and 25,658 (4-byte runes), pinned below 26,000 by tests; safe on a 32k window and not on an 8k one. Resolved from the M2 carry-forward list: `ReadBrief` and `BriefChunks`, node-to-chunk provenance, the shared `askJSON`, candidate-based JSON extraction (`parseFirst`, longest candidate's error), and the pass-2 validator guaranteeing a work description and an acceptance criterion before `Update`.

### Carry-forward decisions for M4 to M6

**M4 (questions)**
1. **First item: a bounded "project facts" block in the pass-2 prompt** (language, build and test commands, file layout), from `Options2` or probed once and cached in `_state/`. Nothing tells the model what the repository is, so every `test_command` and `target_paths` is currently a guess. This is the largest product risk after M3.
2. The shared helpers (`askJSON`, `parseFirst`, `candidates`, `clip`, `oneLine`) are package-private: put the questions pass in `plan`, or move them to a shared internal package, or M4 will re-implement them.
3. `Pass1Output` and `Pass2Output` reject unknown fields, so adding `questions` to a reply and its prompt must ship in one commit, and the prompt's byte budget must be re-measured (the test threshold has about 450 bytes of headroom).
4. No code moves a step out of `StageAwaitingAnswers`; whoever applies answers owns that transition (back to inspected, or to needs_reconciliation).
5. Provenance is unreadable outside the package; export something like `ExcerptsFor(repo, ids, budget)` for "show the brief behind this question".
6. Smaller items: widen the pass-2 end-to-end fixture to two steps per task and assert excerpt isolation for every pass-2 request; a mid-batch failure test (`Result2.Steps` undercounts a partial batch); `Pass1Prompt` and `Pass2Prompt` take positional arguments, convert to option structs before more are added.

**M5 (question-round UI, re-planning)**
1. `StageNeedsReconciliation` is a dead end: `NextActions` offers reconcile for it, but `needsSpec` excludes it, so nothing re-specifies it. Add an explicit re-plan path (an option, so an ordinary resume never silently redoes paid work) and an exported way to specify a named set of steps.
2. Re-planning overwrites `Work` with no history; `Node.ResumeNote` exists and is unused.
3. A re-planned step is specified with siblings shown as titles and stage tags only, not their specifications.
4. Provenance replay replaces a chunk's list, which can orphan nodes created by a crashed attempt with different titles; use a union.
5. `needsSpec` does not exclude in-progress or completed steps (parity with `NextActions`), reachable once a tree is partly executed. There is still no node removal, so a skipped step blocks approval forever.

**M6 (approve, export, wire into `/project`)**
1. Agent, model and wave have no home: the node schema is closed (unknown fields rejected, version pinned at 4). Use a sidecar in `_state/` or bump to schema 5 with a migration. There are no cross-task dependencies, so state a rule for wave (phase index).
2. A task has no description to export: make `objective` required on tasks, or synthesize one from its steps.
3. One run lock over both passes (`RunPass1`, `RunPass2` and `recordProvenance`'s load-modify-write); export progress (`Result` and `Result2` count one call only); validate `Options` and `Options2` (`OverviewCap`, project name); derive `ChunkBytes`, `BriefBytes` and `StepsPerPass` from the model's context window.
4. `Summarize` still derives only reviewed or untouched. A task with no steps blocks approval until something decomposes it: `Result2.EmptyTasks` and `EmptyTasks(repo)` report them, but nothing performs the decompose.
5. `ExtractJSON` is unused by production code (kept exported, with its own tests); decide on deletion when the public surface is fixed.
