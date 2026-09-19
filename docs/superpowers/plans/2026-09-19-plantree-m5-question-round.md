# plantree M5: the question round Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the project owner answer every question the passes asked in one round, see the part of the brief each question came from, change an answer later, and have exactly the steps that answer invalidated planned again, with approval refused until none is left.

**Architecture:** Part A extends the package `gophermind-lib/plantree/plan` with three things the round needs and one it leaves behind: `ExcerptsFor` turns the node ids a question carries back into brief text, `ChangeAnswer` replaces an answer under the questions lock, keeps the old one in a bounded history and moves every step specified from it to stage `needs_reconciliation`, and `RunPass2` with `Options2.Reconcile` re-specifies those steps, showing each its previous specification and the decision that changed. `plan.NextActions` explains each reconcile action from the step's resume note. Part B adds a bubbletea component in `gophermind-lib/tui`: `questionRound` (`question_round.go`) is a pure list-with-a-cursor over the questions, owning no repository, agent or goroutine, and `questions.go` hosts it behind a thin `/questions` command that reads `<cwd>/.planning/plan`, writes the answers and runs pass 2 on a goroutine.

**Tech Stack:** Go (module `gophermind/gophermind-lib`), standard library, bubbletea and bubbles (textarea), the existing `plantree`, `plan`, `lockfile`, `phaseflow` and `llm` packages.

**Spec:** `docs/superpowers/specs/2026-09-19-brief-workflow-design.md` (approved design: questions are collected during the passes and asked in one round, with options, multi-select, a recommendation and free text), `docs/superpowers/plans/2026-09-19-brief-workflow-roadmap.md` (the M3 and M4 outcomes and the M5 carry-forward list this plan implements).

## Global Constraints

- All commands run from `/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com/gophermind-lib`.
- Test command: `go test ./plantree/... ./tui/... -count=1` (about 20 seconds). Race check: `go test -race ./plantree/... ./tui/... -short -count=1`.
- Run `gofmt -w plantree tui` before every check, then `gofmt -l plantree tui` must print nothing. `go vet ./...` and `go build ./...` must be clean.
- Package `plantree/plan` may import `plantree`, `lockfile` and `llm` only. Nothing in `plantree` may import `phaseflow` or `tui`. Package `tui` may import `plantree` and `plantree/plan`; that edge is new in this milestone.
- Strict decoding of untrusted model output stays strict: no reply shape gains a permissive decoder here.
- Prompts stay bounded. The worst-case pass-2 prompt is pinned under 27,000 bytes by tests, for an ordinary pass and for a re-planning pass.
- `questions.json` growth stays bounded: at most `maxAnswerHistory` previous answers per question, each cut to `historyTextBytes`.
- Every new persisted field is additive and optional, and every one is validated before it is written.
- The code and tests in this plan were written and run green, task by task in this order, in a scratch copy of `gophermind-lib` before the plan was generated. Copy them exactly. Where a task says "Replace the whole of" a file, the file's complete new content is given: write it exactly. Where a task gives a **find** and **replace with** pair, the find text appears exactly once in the file: replace that occurrence and nothing else. If a real compile or vet error appears, fix it minimally and disclose the fix in your report. If a test fails, report BLOCKED with specifics instead of editing the test.
- Do NOT run `git switch`, `git checkout`, `git branch`, `git reset` or `git stash`: a git wrapper blocks them. Only `git add`, `git commit`, `git status`, `git diff` and `git log` are needed.
- No em dashes and no emojis in code, comments or commit messages.
- Commit messages end with these two lines:
  `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr`

## Decisions made for M5

1. **Where the round lives.** The owner ruled that the round belongs in the `/project` flow, and that M6 wires `/project` onto plantree. So in M5 the round is a self-contained TUI component plus a thin standalone command, `/questions`, operating on `<cwd>/.planning/plan`. It is usable and testable now, and M6 makes `/project` enter the same component rather than building a second one. Nothing in `question_round.go` knows about the command; everything that touches the repository is in `questions.go`.
2. **One round, everything visible.** All questions are listed at once with a cursor, not one modal prompt after another. Each question shows its options with single or multi select, the recommendation marked and never selected, the reason it was asked, the brief excerpt behind it, and a free-text note that is always available. Progress reads "3 of 7 answered". Skipping leaves a question open, so the owner is never trapped by one they cannot decide, and the round can always be submitted once every question is answered or skipped.
3. **The owner can change an answer.** `ChangeAnswer` replaces an answered question's answer under the questions lock, validated by the same `checkAnswer` a first answer goes through. The previous answer moves into a bounded history on the record: `prior_answers`, an additive optional field, at most `maxAnswerHistory` (5) entries of at most `historyTextBytes` (500) each. `questions.json` goes to `schema_version` 2; a schema-1 file still loads (it simply has no history) and the next save rewrites it as 2. A future version is still refused.
4. **A changed answer flags, it does not redo.** `ChangeAnswer` moves every step the question affects that already carries a specification (stage drafted or approved, not on hold) to stage `needs_reconciliation` with a resume note naming the question, and holds nothing else: a step still waiting for its first specification is left alone, because the new answer reaches it through the prompt anyway. A step that is in progress or completed is reported in `Reconciled.Executed` and left exactly as it is, because re-planning work that is already running is the owner's call, not this function's. Changing an answer to what it already says does nothing at all.
5. **Re-planning is opt-in and bounded.** `Options2.Reconcile` (default false, so an ordinary resume never silently redoes paid work) makes pass 2 also select `needs_reconciliation` steps. Batches are never mixed: a re-planning batch holds `reconcileStepsPerPass` (3) steps instead of 6, because each also carries its previous specification (500 bytes) and the reason it is being redone (200 bytes). That is what keeps the worst case inside the same budget. After the pass, the step's resume note holds a summary of the specification that was replaced, so the history is not simply lost, and only the most recent replacement is kept.
6. **Holding and releasing ignore a step being re-planned.** `needsSpec` still means "waiting for a first specification", so `holdSteps`, `HoldOpen` and `ReleaseAnswered` do not touch a `needs_reconciliation` step. Such a step already has a specification, it is not waiting for one, and it keeps approval away on its own. A question asked about a step being re-planned therefore holds nothing; the step stays where it is and the next reconciling pass sees the new decision.
7. **`plan.NextActions` explains itself.** Each reconcile action's reason becomes the step's resume note, which names the question whose answer changed, so the owner reads why a step must be planned again rather than only that it must. Approve is still refused while any step needs reconciliation (`plantree.NextActions` already reports one as runnable work) and while any question is open (M4's wrapper).
8. **Budget, measured.** Worst-case pass-2 prompt, unchanged for an ordinary pass: 26,481 bytes ASCII and 26,572 with 4-byte runes. A re-planning pass: 26,728 and 26,800. Both are pinned below 27,000 by tests. Pass-1 instruction overhead is untouched at 1,984 bytes.
9. **Nil-agent safety.** The model carries a `completer plan.Completer` field. A test injects a fake and drives the whole round with no agent at all; in a session it is nil and the round builds `plan.ClientCompleter{Client: m.agent.LLM()}`. With neither, the answers are still written and the transcript says nothing was re-planned.
10. **Out of scope:** approval, export to `assignments.json`, `/project` entering the round, the run lock, a home for agent, model and wave, and deriving prompt sizes from the model's context window. All of that is M6.

---

## File structure

| File | Responsibility |
|---|---|
| `plantree/plan/excerptsfor.go` | `ExcerptsFor`, `ExcerptsForCapBytes`, `withAncestors`: the brief text behind a set of nodes |
| `plantree/plan/questions_change.go` | `ChangeAnswer`, `Reconciled`, `appendHistory`, `flagForReconciliation` |
| `plantree/plan/questions.go` (modify) | schema 2, `PriorAnswer`, `Question.PriorAnswers`, `ErrNotAnswered`, history caps |
| `plantree/plan/prompt2.go` (modify) | a re-planned step shows its previous specification and why; `reconcileStepsPerPass` |
| `plantree/plan/runner2.go` (modify) | `Options2.Reconcile`, `Result2.Reconciled`, `needsRespec`, `batchesOf`, `respecNote` |
| `plantree/plan/next.go` (modify) | reconcile actions carry the step's resume note |
| `tui/question_round.go` | the `questionRound` component: list, options, note, skip, submit |
| `tui/questions.go` | `/questions`, the writes, and the pass-2 goroutine |
| `tui/input.go` (modify) | `wrappedRows` extracted so the round's note box grows the same way |
| `tui/model.go`, `update.go`, `view.go`, `commands_registry.go` (modify) | hosting: state, key routing, panel, registry entry |

---

### Task 1: The brief text behind a question

**Files:**
- Create: `gophermind-lib/plantree/plan/excerptsfor.go`, `excerptsfor_test.go`

**Interfaces:**
- Consumes: `loadProvenance`, `provenance.chunksFor`, `BriefChunks`, `Excerpts` (existing, package-private except the last two), `plantree.ParentID`.
- Produces:
  - `const ExcerptsForCapBytes = 4000`
  - `func ExcerptsFor(repo *plantree.Repo, nodeIDs []string, budget int) (string, error)`

This is M4 carry-forward item 5 of the M3 outcome: provenance is unreadable outside the package, so the round cannot show "why was this asked". A node with no provenance of its own falls back to its ancestors, because pass 1 records the nodes a chunk created and a later merge can add a step under a recorded task. No brief and no pass-1 state is not an error: excerpts are context, and a tree built without pass 1 simply has none.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/excerptsfor_test.go`:

```go
package plan

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

func TestExcerptsForShowsTheBriefBehindANode(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	got, err := ExcerptsFor(r, []string{"phase-001.task-002"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "second part text") {
		t.Errorf("ExcerptsFor = %q, want the chunk that produced the task", got)
	}
	if strings.Contains(got, "third part text") {
		t.Errorf("ExcerptsFor leaked another task's brief: %q", got)
	}
}

func TestExcerptsForClimbsToAnAncestor(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	// A step added after the chunk was recorded has no provenance of its own,
	// so the excerpt has to come from the task above it.
	step, err := newSkeleton("phase-001.task-002.step-009", "Late step", "added later", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Create(step); err != nil {
		t.Fatal(err)
	}
	got, err := ExcerptsFor(r, []string{"phase-001.task-002.step-009"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "second part text") {
		t.Errorf("ExcerptsFor = %q, want the ancestor's chunk", got)
	}
}

func TestExcerptsForIsBoundedAndRuneSafe(t *testing.T) {
	r := plantree.Open(t.TempDir())
	brief := "# One\n" + strings.Repeat("\U0001D11E", 20000) + "\n"
	if _, err := RunPass1(context.Background(), r, brief, &fake{reply: func(int, string) (string, error) {
		return `{"phases":[{"title":"Alpha","digest":"d","objective":"","tasks":[{"title":"T1","digest":"d","objective":"","steps":[{"title":"S1","digest":"d"}]}]}],"overview":"o"}`, nil
	}}, Options{ProjectName: "demo", ChunkBytes: 4000}); err != nil {
		t.Fatal(err)
	}
	got, err := ExcerptsFor(r, []string{"phase-001"}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 900 {
		t.Errorf("ExcerptsFor returned %d bytes, want at most 900", len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("ExcerptsFor cut a rune in half")
	}
	if big, err := ExcerptsFor(r, []string{"phase-001"}, 10*ExcerptsForCapBytes); err != nil || len(big) > ExcerptsForCapBytes {
		t.Errorf("an oversize budget must be clamped: %d bytes, %v", len(big), err)
	}
}

func TestExcerptsForWithoutABriefOrNodesSaysNothing(t *testing.T) {
	r := newRepo(t) // no pass 1 ran here, so there is no brief and no state
	for _, ids := range [][]string{nil, {"phase-001"}} {
		got, err := ExcerptsFor(r, ids, 0)
		if err != nil || got != "" {
			t.Errorf("ExcerptsFor(%v) = %q, %v; want no excerpt and no error", ids, got, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -run ExcerptsFor -count=1`
Expected: FAIL, `undefined: ExcerptsFor` and `undefined: ExcerptsForCapBytes`.

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/excerptsfor.go`:

```go
package plan

import (
	"errors"
	"os"

	"gophermind/gophermind-lib/plantree"
)

// ExcerptsForCapBytes is the default budget of ExcerptsFor, and the most it
// ever returns however large a budget a caller asks for.
const ExcerptsForCapBytes = 4000

// ExcerptsFor returns the part of the brief that produced the given nodes, as
// one block of at most budget bytes (ExcerptsForCapBytes when budget is zero
// or larger than it). It is how a user interface shows "why was this asked":
// the question carries node ids, and this turns them back into brief text.
//
// A node with no provenance of its own (a step a later merge added under a
// recorded task) falls back to its ancestors, so the answer is the task's
// brief text rather than nothing. When there is no stored brief or no pass-1
// state it returns "" and no error: excerpts are context, and a plan built
// without pass 1 simply has none.
func ExcerptsFor(repo *plantree.Repo, nodeIDs []string, budget int) (string, error) {
	if len(nodeIDs) == 0 {
		return "", nil
	}
	if budget < 1 || budget > ExcerptsForCapBytes {
		budget = ExcerptsForCapBytes
	}
	p, err := loadProvenance(repo)
	if err != nil {
		return "", err
	}
	chunks, err := BriefChunks(repo)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Excerpts(chunks, p.chunksFor(withAncestors(nodeIDs)), budget), nil
}

// withAncestors returns ids followed by every ancestor of each, once, so a
// lookup finds the chunk that produced a node's task or phase when the node
// itself was never recorded. An id that does not parse contributes only
// itself.
func withAncestors(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		for cur := id; cur != ""; {
			if !seen[cur] {
				seen[cur] = true
				out = append(out, cur)
			}
			parent, err := plantree.ParentID(cur)
			if err != nil {
				break
			}
			cur = parent
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -run ExcerptsFor -count=1 -v`
Expected: the four `TestExcerptsFor...` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/excerptsfor.go gophermind-lib/plantree/plan/excerptsfor_test.go
git commit -m "feat(plan): export the brief excerpts behind a set of nodes

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 2: Changing an answer

**Files:**
- Create: `gophermind-lib/plantree/plan/questions_change.go`, `questions_change_test.go`
- Modify: `gophermind-lib/plantree/plan/questions.go`

**Interfaces:**
- Consumes: `lockQuestions`, `loadQuestionFile`, `saveQuestionFile`, `checkAnswer`, `cutBytes`, `oneLine`, `decisionLine`, `stepsUnder`, `onHold` (all existing and package-private), the test helpers `newRepo` (`merge_test.go`), `newSkeleton` (`merge.go`) and `twoOptions` (`questions_test.go`).
- Produces:
  - `type PriorAnswer struct{ OptionIDs []string; Text, AnsweredAt, ReplacedAt string }` and `Question.PriorAnswers []PriorAnswer` (`json:"prior_answers,omitempty"`)
  - `questionsSchema = 2`, `questionsSchemaMin = 1`, `maxAnswerHistory = 5`, `historyTextBytes = 500`
  - `var ErrNotAnswered`
  - `type Reconciled struct{ Flagged, Executed []string }`
  - `func ChangeAnswer(repo *plantree.Repo, id string, a Answer) (Question, Reconciled, error)`
  - `const reconcileNoteBytes = 300`
  - test helpers `answeredRepo(t)` and `draft(t, repo, id)`, reused by Task 3

`ChangeAnswer` returns the reconciliation alongside the question because the caller has to report it: how many steps must be planned again, and which ones were left alone because they are already running.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/questions_change_test.go`:

```go
package plan

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// answeredRepo builds a tree with two steps under the task a question affects
// and one step under a task it does not, answers the question, and drafts
// every step, which is the state an owner is in when they change their mind.
func answeredRepo(t *testing.T) (*plantree.Repo, Question) {
	t.Helper()
	r := newRepo(t)
	for _, id := range []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001",
		"phase-001.task-001.step-002", "phase-001.task-002", "phase-001.task-002.step-001"} {
		n, err := newSkeleton(id, "node "+id, "why "+id, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	nq := twoOptions()
	nq.Affects = []string{"phase-001.task-001"}
	qs, err := AddQuestions(r, []NewQuestion{nq})
	if err != nil {
		t.Fatal(err)
	}
	q, err := AnswerQuestion(r, qs[0].ID, Answer{OptionIDs: []string{"opt-1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002", "phase-001.task-002.step-001"} {
		draft(t, r, id)
	}
	return r, q
}

// draft gives a step a complete specification and the drafted stage.
func draft(t *testing.T, r *plantree.Repo, id string) {
	t.Helper()
	cur, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Update(id, cur.NodeRevision, func(n *plantree.Node) error {
		n.Work = &plantree.Work{Description: "build " + id, AcceptanceCriteria: []string{"it works"}}
		n.Planning.Stage = plantree.StageDrafted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestChangeAnswerFlagsTheDraftedStepsItAffects(t *testing.T) {
	r, q := answeredRepo(t)
	got, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}, Text: "on reflection"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer == nil || len(got.Answer.OptionIDs) != 1 || got.Answer.OptionIDs[0] != "opt-2" || got.Answer.Text != "on reflection" {
		t.Errorf("answer = %+v", got.Answer)
	}
	if len(got.PriorAnswers) != 1 || len(got.PriorAnswers[0].OptionIDs) != 1 || got.PriorAnswers[0].OptionIDs[0] != "opt-1" {
		t.Errorf("prior answers = %+v", got.PriorAnswers)
	}
	if got.Status != QuestionAnswered {
		t.Errorf("status = %q, want answered", got.Status)
	}
	want := []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"}
	if strings.Join(rec.Flagged, ",") != strings.Join(want, ",") {
		t.Errorf("Flagged = %v, want %v", rec.Flagged, want)
	}
	if len(rec.Executed) != 0 {
		t.Errorf("Executed = %v, want none", rec.Executed)
	}
	for _, id := range want {
		n, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if n.Planning.Stage != plantree.StageNeedsReconciliation {
			t.Errorf("%s is %s, want needs_reconciliation", id, n.Planning.Stage)
		}
		if !strings.Contains(n.ResumeNote, q.ID) || !strings.Contains(n.ResumeNote, "Postgres") {
			t.Errorf("%s resume note = %q, want the question and the new choice", id, n.ResumeNote)
		}
		if n.Work == nil {
			t.Errorf("%s lost its specification", id)
		}
	}
	// A step the question does not affect is untouched, and so is everything else.
	if n, _ := r.Get("phase-001.task-002.step-001"); n.Planning.Stage != plantree.StageDrafted || n.ResumeNote != "" {
		t.Errorf("an unaffected step changed: %s %q", n.Planning.Stage, n.ResumeNote)
	}
}

func TestChangeAnswerIsIdempotentForTheSameAnswer(t *testing.T) {
	r, q := answeredRepo(t)
	if _, _, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	// Put the flagged steps back so a second identical change would show up.
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		draft(t, r, id)
	}
	got, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Flagged) != 0 || len(rec.Executed) != 0 {
		t.Errorf("an unchanged answer flagged %+v", rec)
	}
	if len(got.PriorAnswers) != 1 {
		t.Errorf("an unchanged answer grew the history to %d entries", len(got.PriorAnswers))
	}
	if n, _ := r.Get("phase-001.task-001.step-001"); n.Planning.Stage != plantree.StageDrafted {
		t.Errorf("an unchanged answer moved %s to %s", n.ID, n.Planning.Stage)
	}
}

func TestChangeAnswerLeavesExecutedStepsAlone(t *testing.T) {
	r, q := answeredRepo(t)
	cur, _ := r.Get("phase-001.task-001.step-002")
	if _, err := r.Update(cur.ID, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusCompleted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, rec, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rec.Flagged, ",") != "phase-001.task-001.step-001" {
		t.Errorf("Flagged = %v", rec.Flagged)
	}
	if strings.Join(rec.Executed, ",") != "phase-001.task-001.step-002" {
		t.Errorf("Executed = %v, want the completed step reported, not re-planned", rec.Executed)
	}
	if n, _ := r.Get("phase-001.task-001.step-002"); n.Planning.Stage != plantree.StageDrafted {
		t.Errorf("a completed step was re-planned: %s", n.Planning.Stage)
	}
}

func TestChangeAnswerValidatesLikeAnswerQuestion(t *testing.T) {
	r, q := answeredRepo(t)
	for _, bad := range []Answer{{}, {OptionIDs: []string{"opt-9"}}, {OptionIDs: []string{"opt-1", "opt-2"}}} {
		if _, _, err := ChangeAnswer(r, q.ID, bad); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("ChangeAnswer(%+v) = %v, want ErrInvalidAnswer", bad, err)
		}
	}
	if _, _, err := ChangeAnswer(r, "q-404", Answer{OptionIDs: []string{"opt-1"}}); !errors.Is(err, ErrNoSuchQuestion) {
		t.Errorf("unknown id: %v", err)
	}
	open, err := AddQuestions(r, []NewQuestion{{Question: "Still open?", Why: "w", Affects: []string{"phase-001"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ChangeAnswer(r, open[0].ID, Answer{Text: "yes"}); !errors.Is(err, ErrNotAnswered) {
		t.Errorf("open question: %v, want ErrNotAnswered", err)
	}
}

func TestChangeAnswerBoundsTheHistory(t *testing.T) {
	r, q := answeredRepo(t)
	long := strings.Repeat("y", 1500)
	for i := 0; i < maxAnswerHistory+3; i++ {
		if _, _, err := ChangeAnswer(r, q.ID, Answer{Text: long + strings.Repeat("z", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	qs, err := LoadQuestions(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs[0].PriorAnswers) != maxAnswerHistory {
		t.Fatalf("history = %d entries, want %d", len(qs[0].PriorAnswers), maxAnswerHistory)
	}
	for _, p := range qs[0].PriorAnswers {
		if len(p.Text) > historyTextBytes+3 {
			t.Errorf("a stored previous answer is %d bytes, want at most %d", len(p.Text), historyTextBytes)
		}
	}
	// The oldest entries are the ones dropped: the first kept entry is not the
	// original answer any more.
	if len(qs[0].PriorAnswers[0].OptionIDs) != 0 {
		t.Errorf("the oldest entry survived: %+v", qs[0].PriorAnswers[0])
	}
}

func TestQuestionsFileFromSchema1StillLoads(t *testing.T) {
	r := newRepo(t)
	const v1 = `{
  "schema_version": 1,
  "revision": 3,
  "questions": [
    {"id":"q-001","question":"Which database?","why":"the schema depends on it",
     "options":[{"id":"opt-1","label":"SQLite","description":"embedded"}],
     "multi_select":false,"allow_free_text":true,"recommended":null,
     "affects":["phase-001"],"source":"chunk 1 of the brief",
     "status":"answered","answer":{"option_ids":["opt-1"],"text":""},"answered_at":"2026-09-19T00:00:00Z"}
  ]
}`
	if err := os.WriteFile(questionsPath(r), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	qs, err := LoadQuestions(r)
	if err != nil || len(qs) != 1 || qs[0].Answer == nil || len(qs[0].PriorAnswers) != 0 {
		t.Fatalf("LoadQuestions = %+v, %v", qs, err)
	}
	if _, _, err := ChangeAnswer(r, "q-001", Answer{Text: "actually Postgres"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(questionsPath(r))
	if err != nil {
		t.Fatal(err)
	}
	var f questionFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.SchemaVersion != questionsSchema || f.Revision != 4 || len(f.Questions[0].PriorAnswers) != 1 {
		t.Errorf("after a change the file is schema %d revision %d with %d prior answers",
			f.SchemaVersion, f.Revision, len(f.Questions[0].PriorAnswers))
	}
}

func TestQuestionsFileFromAFutureSchemaIsRefused(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(questionsPath(r), []byte(`{"schema_version":99,"revision":1,"questions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuestions(r); err == nil || !strings.Contains(err.Error(), "schema_version 99") {
		t.Errorf("err = %v, want a refusal naming the version", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -run 'ChangeAnswer|QuestionsFile' -count=1`
Expected: FAIL, `undefined: ChangeAnswer`, `undefined: ErrNotAnswered`, `undefined: maxAnswerHistory`, `q.PriorAnswers undefined`.

- [ ] **Step 3: Write the implementation**

Four edits to `plantree/plan/questions.go`.

Edit 1, the constants. Find:

```go
	maxStoredAffects   = 200
	questionsFile      = "questions.json"
	questionsSchema    = 1
	maxAnswerTextRunes = 2000
	QuestionOpen       = "open"
	QuestionAnswered   = "answered"
```

Replace with:

```go
	maxStoredAffects = 200
	questionsFile    = "questions.json"
	// questionsSchema is what this code writes. Schema 2 added the optional
	// prior_answers history; a schema-1 file still loads (it simply has none)
	// and is rewritten as schema 2 by the next save.
	questionsSchema    = 2
	questionsSchemaMin = 1
	maxAnswerTextRunes = 2000
	// maxAnswerHistory caps the previous answers kept on one question and
	// historyTextBytes caps the text of each, so an owner who changes their
	// mind repeatedly cannot grow questions.json without bound.
	maxAnswerHistory = 5
	historyTextBytes = 500
	QuestionOpen     = "open"
	QuestionAnswered = "answered"
```

Edit 2, the new sentinel. Find:

```go
	// ErrInvalidAnswer is returned when an answer does not fit its question.
	ErrInvalidAnswer = errors.New("plan: invalid answer")
)
```

Replace with:

```go
	// ErrInvalidAnswer is returned when an answer does not fit its question.
	ErrInvalidAnswer = errors.New("plan: invalid answer")
	// ErrNotAnswered is returned when changing the answer of a question that
	// has none yet. Answer it with AnswerQuestion first.
	ErrNotAnswered = errors.New("plan: the question is not answered yet")
)
```

Edit 3, the record. Find:

```go
// Question is a decision the plan needs from a person.
```

Replace with:

```go
// PriorAnswer is an answer that was replaced, kept so the record shows what
// the owner decided before they changed their mind. Its text is cut to
// historyTextBytes.
type PriorAnswer struct {
	OptionIDs  []string `json:"option_ids"`
	Text       string   `json:"text"`
	AnsweredAt string   `json:"answered_at"`
	ReplacedAt string   `json:"replaced_at"`
}

// Question is a decision the plan needs from a person.
```

and find:

```go
	Answer        *Answer         `json:"answer"`
	AnsweredAt    string          `json:"answered_at"`
}
```

Replace with:

```go
	Answer        *Answer         `json:"answer"`
	AnsweredAt    string          `json:"answered_at"`
	// PriorAnswers holds the answers this question had before, oldest first,
	// at most maxAnswerHistory of them. Added in schema 2, so it is omitted
	// when empty and a schema-1 file simply has none.
	PriorAnswers []PriorAnswer `json:"prior_answers,omitempty"`
}
```

Edit 4, the version check. Find:

```go
	if f.SchemaVersion != questionsSchema {
		return questionFile{}, fmt.Errorf("plan: %s has unsupported schema_version %d (want %d)", questionsPath(repo), f.SchemaVersion, questionsSchema)
	}
```

Replace with:

```go
	if f.SchemaVersion < questionsSchemaMin || f.SchemaVersion > questionsSchema {
		return questionFile{}, fmt.Errorf("plan: %s has unsupported schema_version %d (want %d to %d)", questionsPath(repo), f.SchemaVersion, questionsSchemaMin, questionsSchema)
	}
```

Create `plantree/plan/questions_change.go`:

```go
package plan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gophermind/gophermind-lib/plantree"
)

// reconcileNoteBytes bounds the resume note a changed answer leaves on a step.
const reconcileNoteBytes = 300

// Reconciled reports what a changed answer did to the tree.
type Reconciled struct {
	// Flagged lists, in tree order, the steps moved to
	// needs_reconciliation: they were specified from the old answer and must
	// be specified again (RunPass2 with Options2.Reconcile).
	Flagged []string
	// Executed lists steps the answer affects that are already in progress or
	// completed. They are left exactly as they are, because re-planning
	// finished work is not this function's decision to make; a caller should
	// show them to the owner.
	Executed []string
}

// ChangeAnswer replaces the answer of an already answered question. The new
// answer is validated exactly like a first answer, the old one is kept in the
// question's bounded history, and every step that was specified from the old
// answer is moved to stage needs_reconciliation with a resume note naming the
// question, so the next reconciling pass 2 re-specifies it and nothing else.
//
// Changing an answer to what it already says does nothing at all: no history
// entry, no flagged step, no write. A question that is still open is refused
// with ErrNotAnswered; answer it with AnswerQuestion instead.
func ChangeAnswer(repo *plantree.Repo, id string, a Answer) (Question, Reconciled, error) {
	unlock, err := lockQuestions(repo)
	if err != nil {
		return Question{}, Reconciled{}, err
	}
	defer unlock()
	f, err := loadQuestionFile(repo)
	if err != nil {
		return Question{}, Reconciled{}, err
	}
	for i := range f.Questions {
		q := &f.Questions[i]
		if q.ID != id {
			continue
		}
		if q.Status != QuestionAnswered || q.Answer == nil {
			return Question{}, Reconciled{}, fmt.Errorf("%w: %s", ErrNotAnswered, id)
		}
		if err := checkAnswer(*q, a); err != nil {
			return Question{}, Reconciled{}, err
		}
		next := Answer{OptionIDs: append([]string{}, a.OptionIDs...), Text: strings.TrimSpace(a.Text)}
		if sameAnswer(*q.Answer, next) {
			return *q, Reconciled{}, nil
		}
		now := time.Now().UTC().Format(time.RFC3339)
		q.PriorAnswers = appendHistory(q.PriorAnswers, *q.Answer, q.AnsweredAt, now)
		q.Answer = &next
		q.AnsweredAt = now
		if err := saveQuestionFile(repo, f); err != nil {
			return Question{}, Reconciled{}, err
		}
		rec, err := flagForReconciliation(repo, *q)
		return *q, rec, err
	}
	return Question{}, Reconciled{}, fmt.Errorf("%w: %s", ErrNoSuchQuestion, id)
}

// sameAnswer reports whether two answers say the same thing. Option order is
// not part of an answer, so a re-ordered selection is the same answer.
func sameAnswer(a, b Answer) bool {
	if strings.TrimSpace(a.Text) != strings.TrimSpace(b.Text) || len(a.OptionIDs) != len(b.OptionIDs) {
		return false
	}
	x := append([]string{}, a.OptionIDs...)
	y := append([]string{}, b.OptionIDs...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// appendHistory adds the replaced answer to hist, keeping the newest
// maxAnswerHistory entries and cutting each stored text to historyTextBytes.
func appendHistory(hist []PriorAnswer, old Answer, answeredAt, replacedAt string) []PriorAnswer {
	hist = append(hist, PriorAnswer{
		OptionIDs:  append([]string{}, old.OptionIDs...),
		Text:       cutBytes(old.Text, historyTextBytes),
		AnsweredAt: answeredAt,
		ReplacedAt: replacedAt,
	})
	if len(hist) > maxAnswerHistory {
		hist = append([]PriorAnswer{}, hist[len(hist)-maxAnswerHistory:]...)
	}
	return hist
}

// flagForReconciliation moves every step the question affects that already
// carries a specification (drafted or approved, and not on hold) to stage
// needs_reconciliation, with a resume note saying which decision changed. A
// step that still waits for its first specification is left where it is: the
// new answer reaches it through the prompt anyway.
func flagForReconciliation(repo *plantree.Repo, q Question) (Reconciled, error) {
	steps, err := stepsUnder(repo, q.Affects)
	if err != nil {
		return Reconciled{}, err
	}
	note := cutBytes("re-plan: the answer to "+q.ID+" changed: "+oneLine(decisionLine(q)), reconcileNoteBytes)
	var rec Reconciled
	for _, s := range steps {
		if onHold(s) {
			continue
		}
		if s.Planning.Stage != plantree.StageDrafted && s.Planning.Stage != plantree.StageApproved {
			continue
		}
		if s.Status == plantree.StatusInProgress || s.Status == plantree.StatusCompleted {
			rec.Executed = append(rec.Executed, s.ID)
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageNeedsReconciliation
			n.ResumeNote = note
			return nil
		}); err != nil {
			return rec, fmt.Errorf("flagging %s for re-planning: %w", s.ID, err)
		}
		rec.Flagged = append(rec.Flagged, s.ID)
	}
	return rec, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/... -count=1 && go vet ./plantree/...`
Expected: `ok` for both packages, vet clean. Every M4 test still passes: the schema bump is backward compatible and nothing else changed.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/questions.go gophermind-lib/plantree/plan/questions_change.go gophermind-lib/plantree/plan/questions_change_test.go
git commit -m "feat(plan): change an answer and flag the steps it invalidated

questions.json goes to schema 2 with a bounded prior_answers history; a
schema-1 file still loads. A changed answer moves every step already
specified from it to needs_reconciliation and reports the ones already
running, which it leaves alone.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 3: Pass 2 re-plans what a changed answer invalidated

**Files:**
- Create: `gophermind-lib/plantree/plan/reconcile_test.go`
- Modify: `gophermind-lib/plantree/plan/prompt2.go`, `runner2.go`, `runner2_test.go`

**Interfaces:**
- Consumes: `ChangeAnswer`, `answeredRepo`, `draft` (Task 2), `Pass2Prompt`, `askJSON`, `setOf`, `cutBytes`, `oneLine`, the test helpers `node`, `worstDecisions` (`prompt2_test.go`), `specFake`, `stepsToSpecify`, `actionKinds` (`runner2_test.go`), `stageOf` (`questions_apply_test.go`).
- Produces:
  - `Options2.Reconcile bool` (default false) and `Result2.Reconciled int`
  - `const reconcileStepsPerPass = 3`, `priorWorkBytes = 500`, `reconcileNoteShownBytes = 200`
  - `func needsRespec(s plantree.Node) bool`, `func batchesOf(steps []plantree.Node, size int) [][]plantree.Node`, `func respecNote(old *plantree.Work) string`
  - `pendingWork(repo, reconcile bool)` and `applySpecs(repo, out, redo map[string]bool)` (both package-private, signatures changed)
  - `stepTag` gains the `specified, being re-planned` tag

A re-planning batch is smaller than an ordinary one and never mixed with fresh steps, because each of its steps also carries its previous specification and the reason it is being redone. That is the whole reason the worst case still fits: a batch of three such steps is smaller than a batch of six fresh ones.

- [ ] **Step 1: Write the failing tests**

One edit to `plantree/plan/runner2_test.go`, because the prompt now repeats a step's id in the indented lines under it and the fixture scraped ids from the whole region. Find:

```go
// stepsToSpecify returns the step ids listed under "Steps to specify now".
func stepsToSpecify(prompt string) []string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	if start < 0 || end < start {
		return nil
	}
	return stepIDRE.FindAllString(prompt[start:end], -1)
}
```

Replace with:

```go
// stepsToSpecify returns the step ids listed under "Steps to specify now".
// Only the "- <id>: ..." lines count: a re-planned step is followed by
// indented lines quoting its previous specification, which mention its id
// again without asking for it.
func stepsToSpecify(prompt string) []string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	if start < 0 || end < start {
		return nil
	}
	// An end-to-end test passes the JSON request body, where the prompt's
	// newlines are escaped, so both forms are split here.
	region := strings.ReplaceAll(prompt[start:end], `\n`, "\n")
	var out []string
	for _, line := range strings.Split(region, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if id := stepIDRE.FindString(line); id != "" {
			out = append(out, id)
		}
	}
	return out
}
```

Create `plantree/plan/reconcile_test.go` (the last test in it belongs to Task 4; it is written now because it shares these fixtures):

```go
package plan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

// replanned is a step in the state a changed answer leaves it in: specified,
// flagged for re-planning, with a note saying why.
func replanned(t *testing.T, id string) plantree.Node {
	t.Helper()
	n := node(t, id, "Create module", "needed to compile", "")
	n.Planning.Stage = plantree.StageNeedsReconciliation
	n.Work = &plantree.Work{Description: "the old description", AcceptanceCriteria: []string{"it works"}}
	n.ResumeNote = "re-plan: the answer to q-001 changed: \"Which database?\" -> Postgres"
	return n
}

func TestPass2PromptShowsWhatARePlannedStepIsReplacing(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	old := replanned(t, s1)
	fresh := node(t, s2, "Add CI", "catch breakage", "")

	p := Pass2Prompt(Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{old, fresh}, Batch: []plantree.Node{old}})
	for _, want := range []string{
		"previous specification: the old description",
		"being re-planned because: re-plan: the answer to q-001 changed",
		"is being re-planned because the owner changed a decision",
		s1 + ": Create module [specified, being re-planned]",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}

	// An ordinary batch says none of it, so the instruction cannot be read as
	// applying to a step that has never been specified.
	q := Pass2Prompt(Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{old, fresh}, Batch: []plantree.Node{fresh}})
	for _, unwanted := range []string{"previous specification:", "being re-planned because:", "changed a decision"} {
		if strings.Contains(q, unwanted) {
			t.Errorf("an ordinary batch carries %q", unwanted)
		}
	}
}

// TestPass2PromptWorstCaseSizeWhenRePlanning pins the largest prompt a
// reconciling pass can produce. It must stay inside the same budget as an
// ordinary pass, which is why such a batch holds fewer steps.
func TestPass2PromptWorstCaseSizeWhenRePlanning(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit string
	}{{"ascii", "s"}, {"multibyte", "\U0001D11E"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := func(n int) string { return strings.Repeat(tc.unit, n) }
			phase := node(t, "phase-001", r(200), r(500), r(1000))
			task := node(t, "phase-001.task-001", r(200), r(500), r(1000))
			var steps []plantree.Node
			for i := 1; i <= 100; i++ {
				n := node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), r(200), r(500), "")
				n.Planning.Stage = plantree.StageNeedsReconciliation
				n.Work = &plantree.Work{Description: r(5000), AcceptanceCriteria: []string{"c"}}
				n.ResumeNote = r(5000)
				steps = append(steps, n)
			}
			p := Pass2Prompt(Pass2Input{
				Project: r(100), Overview: strings.Repeat("o", 20000), Facts: r(20000),
				Decisions: worstDecisions(tc.unit) + strings.Repeat("d", 20000), Excerpts: strings.Repeat("e", 20000),
				Phase: phase, Task: task, Siblings: steps, Batch: steps[:reconcileStepsPerPass],
			})
			t.Logf("worst-case re-planning pass-2 prompt (%s): %d bytes", tc.name, len(p))
			if len(p) > 27000 {
				t.Errorf("worst-case re-planning prompt is %d bytes, want at most 27000", len(p))
			}
			if !utf8.ValidString(p) {
				t.Error("the prompt is not valid UTF-8")
			}
		})
	}
}

// reconcileRepo is a two-step task, both steps specified, whose answer the
// owner then changes, so both steps are flagged for re-planning.
func reconcileRepo(t *testing.T) (*plantree.Repo, Question) {
	t.Helper()
	r, q := answeredRepo(t)
	if _, _, err := ChangeAnswer(r, q.ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	return r, q
}

func TestRunPass2LeavesRePlanningAloneUnlessAsked(t *testing.T) {
	r, _ := reconcileRepo(t)
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps != 0 || res.Reconciled != 0 || res.Passes != 0 || len(f.prompts) != 0 {
		t.Errorf("an ordinary resume redid work: %+v after %d prompts", res, len(f.prompts))
	}
	if got := stageOf(t, r, "phase-001.task-001.step-001"); got != plantree.StageNeedsReconciliation {
		t.Errorf("step-001 is %s, want needs_reconciliation", got)
	}
}

func TestRunPass2RePlansOnlyTheFlaggedSteps(t *testing.T) {
	r, q := reconcileRepo(t)
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{Reconcile: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reconciled != 2 || res.Steps != 2 || res.Passes != 1 || res.Unspecified != 0 {
		t.Errorf("Result2 = %+v, want 2 reconciled in one pass", res)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		n, err := r.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s is %s, want drafted again", id, n.Planning.Stage)
		}
		if n.Work == nil || n.Work.Description != "implement "+id {
			t.Errorf("%s work = %+v, want the new specification", id, n.Work)
		}
		if !strings.Contains(n.ResumeNote, "the previous specification was: build "+id) {
			t.Errorf("%s resume note = %q, want the replaced specification", id, n.ResumeNote)
		}
	}
	// The task the question never named was not touched by any of this.
	if n, _ := r.Get("phase-001.task-002.step-001"); n.Work.Description != "build phase-001.task-002.step-001" {
		t.Errorf("an unaffected step was re-specified: %+v", n.Work)
	}
	if len(f.prompts) != 1 || !strings.Contains(f.prompts[0], "previous specification: build phase-001.task-001.step-001") {
		t.Errorf("the pass did not see what it was replacing: %d prompts", len(f.prompts))
	}
	if !strings.Contains(f.prompts[0], q.Question) {
		t.Errorf("the pass did not see the decision that changed")
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval once everything is re-planned", got)
	}
}

func TestRunPass2ReconcileBatchesStayInsideTheirSize(t *testing.T) {
	r, _ := answeredRepo(t)
	for i := 3; i <= 2+reconcileStepsPerPass; i++ {
		n, err := newSkeleton(fmt.Sprintf("phase-001.task-001.step-%03d", i), "extra", "extra", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(n); err != nil {
			t.Fatal(err)
		}
		draft(t, r, n.ID)
	}
	qs, err := LoadQuestions(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, rec, err := ChangeAnswer(r, qs[0].ID, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	} else if len(rec.Flagged) != 2+reconcileStepsPerPass {
		t.Fatalf("flagged %d steps", len(rec.Flagged))
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{Reconcile: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passes != 2 || res.Reconciled != 2+reconcileStepsPerPass {
		t.Errorf("Result2 = %+v, want %d steps over two passes", res, 2+reconcileStepsPerPass)
	}
	for _, p := range f.prompts {
		if got := len(stepsToSpecify(p)); got > reconcileStepsPerPass {
			t.Errorf("a re-planning pass asked for %d steps, cap %d", got, reconcileStepsPerPass)
		}
	}
}

func TestNextActionsWhileStepsWaitToBeRePlanned(t *testing.T) {
	r, q := reconcileRepo(t)
	a, err := NextActions(r)
	if err != nil {
		t.Fatal(err)
	}
	var reconcile int
	for _, x := range a.Runnable {
		if x.Kind == plantree.ActionApprove {
			t.Error("approve is offered while a step waits to be re-planned")
		}
		if x.Kind != plantree.ActionReconcile {
			continue
		}
		reconcile++
		if !strings.Contains(x.Reason, q.ID) {
			t.Errorf("reconcile reason = %q, want the question whose answer changed", x.Reason)
		}
	}
	if reconcile != 2 {
		t.Errorf("%d reconcile actions, want 2", reconcile)
	}

	// An open question keeps approval away as well, even after re-planning.
	if _, err := RunPass2(context.Background(), r, specFake(), Options2{Reconcile: true}); err != nil {
		t.Fatal(err)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Fatalf("NextActions = %s, want only approval", got)
	}
	if _, err := AddQuestions(r, []NewQuestion{{Question: "One more thing?", Why: "w", Affects: []string{"phase-001.task-002"}}}); err != nil {
		t.Fatal(err)
	}
	a, err = NextActions(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range a.Runnable {
		if x.Kind == plantree.ActionApprove {
			t.Error("approve is offered while a question is open")
		}
	}
	if len(a.Blocked) != 1 || a.Blocked[0].Kind != plantree.ActionAnswer {
		t.Errorf("Blocked = %+v, want one answer action", a.Blocked)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -run 'RePlan|Reconcil' -count=1`
Expected: FAIL, `undefined: reconcileStepsPerPass`, `unknown field Reconcile in struct literal of type Options2`, `res.Reconciled undefined`.

- [ ] **Step 3: Write the implementation**

Four edits to `plantree/plan/prompt2.go`.

Edit 1, the defaults. Find:

```go
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 4000
)
```

Replace with:

```go
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 4000
	// reconcileStepsPerPass is the batch size when steps are being re-planned.
	// It is smaller than defaultStepsPerPass because each such step also
	// carries its previous specification and the reason it is being redone,
	// which keeps the worst-case prompt no larger than an ordinary one.
	reconcileStepsPerPass = 3
	// priorWorkBytes bounds the previous description shown for a re-planned
	// step, and reconcileNoteShownBytes the reason shown with it.
	priorWorkBytes          = 500
	reconcileNoteShownBytes = 200
)
```

Edit 2, the sibling tag. Find:

```go
	case s.Planning.Stage == plantree.StageAwaitingAnswers:
		return "waiting for an answer"
	}
```

Replace with:

```go
	case s.Planning.Stage == plantree.StageAwaitingAnswers:
		return "waiting for an answer"
	case s.Planning.Stage == plantree.StageNeedsReconciliation:
		return "specified, being re-planned"
	}
```

Edit 3, the batch listing. Find:

```go
	b.WriteString("\nSteps to specify now:\n")
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, fit(s.Title, 200), fit(s.ContextDigest, 500))
	}
```

Replace with:

```go
	b.WriteString("\nSteps to specify now:\n")
	replanning := false
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, fit(s.Title, 200), fit(s.ContextDigest, 500))
		if s.Planning.Stage != plantree.StageNeedsReconciliation {
			continue
		}
		replanning = true
		if s.Work != nil {
			if prev := fit(s.Work.Description, priorWorkBytes); prev != "" {
				fmt.Fprintf(&b, "  previous specification: %s\n", prev)
			}
		}
		if why := fit(s.ResumeNote, reconcileNoteShownBytes); why != "" {
			fmt.Fprintf(&b, "  being re-planned because: %s\n", why)
		}
	}
```

Edit 4, the rule that goes with it. It is written only when the batch has such a step, so an ordinary prompt is byte for byte what it was. Find:

```go
	b.WriteString("- Do not depend on a step marked on hold or waiting for an answer.\n")
```

Replace with:

```go
	b.WriteString("- Do not depend on a step marked on hold or waiting for an answer.\n")
	if replanning {
		b.WriteString("- A step shown with a previous specification is being re-planned because the owner changed a decision. Write it afresh so it follows the decisions above; keep only what still holds, and do not repeat the old specification out of habit.\n")
	}
```

Replace the whole of `plantree/plan/runner2.go`:

```go
package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// Options2 tunes RunPass2. Zero values pick the defaults.
type Options2 struct {
	ProjectName string // default: the root node's title
	// StepsPerPass bounds how many steps one model call specifies (default 6),
	// so the reply stays small however large a task is.
	StepsPerPass int
	// BriefBytes bounds the brief excerpts shown with a task (default 4000).
	// With the defaults one pass is at most 27,000 bytes (a test pins it):
	// BriefBytes + OverviewCapBytes + FactsCapBytes + 3000 (step list) + about
	// 2,000 (decisions) + about 8,000 (phase, task and the steps being
	// specified) + about 3,500 (instructions). Pass2Prompt cuts each of those
	// inputs itself; a BriefBytes above the default raises the total by the
	// difference. At 3 to 4 bytes per token that must leave room for the reply
	// inside the model's window.
	BriefBytes int
	// Facts is text describing the repository (language, how to build and test,
	// layout) shown to every pass, cut to FactsCapBytes. If empty, RunPass2
	// reads the facts stored with WriteFacts, if any.
	Facts string
	// Reconcile also re-specifies the steps a changed answer flagged (stage
	// needs_reconciliation), showing each its previous specification and the
	// decision that changed. It is off by default so an ordinary resume never
	// silently redoes work that has already been paid for.
	Reconcile bool
}

// Result2 summarizes one RunPass2 call.
type Result2 struct {
	Tasks  int // tasks this call ran passes for, whether or not every step got a specification
	Steps  int // steps specified by this call
	Passes int // model passes made by this call
	// Released counts steps that were waiting for an answer and were released
	// because their questions are now answered. They become eligible for
	// specification in this call, which may still ask about them again.
	Released int
	// Unspecified counts steps this call put in a pass batch that ended the
	// pass neither specified nor waiting for an answer: the model re-asked an
	// already answered question (a duplicate that holds nothing) or left a step
	// out. Such a step is selected again by the next RunPass2, so a caller
	// that sees Unspecified > 0 with no new Steps or Questions is making no
	// progress and should stop rather than loop.
	Unspecified int
	// Questions counts new questions asked by this call. The steps they name
	// wait for an answer and are not specified.
	Questions int
	// EmptyTasks counts tasks that have no steps at all; pass 2 cannot specify
	// them, and NextActions keeps offering decompose for them.
	EmptyTasks int
	// Reconciled counts steps that were waiting to be re-planned (stage
	// needs_reconciliation) and were specified again by this call. It is
	// always zero unless Options2.Reconcile is set. Such a step is counted in
	// Steps as well.
	Reconciled int
}

// taskWork is one task with the steps still waiting for a specification.
type taskWork struct {
	phase   plantree.Node
	task    plantree.Node
	steps   []plantree.Node // every step of the task
	pending []plantree.Node // steps that need a first specification
	redo    []plantree.Node // steps to specify again (needs_reconciliation)
}

// onHold reports whether a step is parked in a status that stops work on it.
func onHold(s plantree.Node) bool {
	switch s.Status {
	case plantree.StatusBlocked, plantree.StatusDelayed, plantree.StatusEscalated,
		plantree.StatusFailed, plantree.StatusNeedsRevision, plantree.StatusSkipped:
		return true
	}
	return false
}

// markSpecified records in the in-memory snapshot that the steps in out were
// written, so a later batch of the same task sees them as specified.
func markSpecified(steps []plantree.Node, out Pass2Output) {
	done := map[string]bool{}
	for _, s := range out.Steps {
		done[s.ID] = true
	}
	for i := range steps {
		if done[steps[i].ID] {
			steps[i].Planning.Stage = plantree.StageDrafted
		}
	}
}

// needsSpec reports whether a step is waiting for its specification: still a
// skeleton or only inspected, and not on hold.
func needsSpec(s plantree.Node) bool {
	if onHold(s) {
		return false
	}
	return s.Planning.Stage == plantree.StageSkeleton || s.Planning.Stage == plantree.StageInspected
}

// needsRespec reports whether a step carries a specification that a changed
// answer invalidated, so a reconciling pass must write it again. Holding and
// releasing ignore such a step: it already has a specification, so it is not
// waiting for one, and it blocks approval on its own (NextActions offers
// reconcile for it).
func needsRespec(s plantree.Node) bool {
	return !onHold(s) && s.Planning.Stage == plantree.StageNeedsReconciliation
}

// pendingWork lists, in tree order, every task that has a step needing a
// specification. It is derived from the tree alone, so a resumed run finds
// exactly what is left. With reconcile set it also collects the steps a
// changed answer flagged for re-planning.
func pendingWork(repo *plantree.Repo, reconcile bool) ([]taskWork, error) {
	var out []taskWork
	var phase plantree.Node
	err := repo.Walk(func(n plantree.Node) error {
		switch n.Kind() {
		case plantree.KindPhase:
			phase = n
		case plantree.KindTask:
			steps, err := repo.Children(n.ID)
			if err != nil {
				return err
			}
			var pending, redo []plantree.Node
			for _, s := range steps {
				switch {
				case needsSpec(s):
					pending = append(pending, s)
				case reconcile && needsRespec(s):
					redo = append(redo, s)
				}
			}
			if len(pending)+len(redo) > 0 {
				out = append(out, taskWork{phase: phase, task: n, steps: steps, pending: pending, redo: redo})
			}
		}
		return nil
	})
	return out, err
}

func idsOf(nodes []plantree.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// RunPass2 writes the work specification of every step that lacks one: one
// fresh-context pass per batch of steps of one task, in tree order. It keeps no
// cursor: what is left is derived from the tree, so calling it again after any
// error continues with the steps still waiting. It does not rewrite
// overview.md; only pass 1 does.
func RunPass2(ctx context.Context, repo *plantree.Repo, c Completer, opt Options2) (Result2, error) {
	if opt.StepsPerPass < 1 {
		opt.StepsPerPass = defaultStepsPerPass
	}
	if opt.BriefBytes < 1 {
		opt.BriefBytes = defaultBriefBytes
	}
	root, err := repo.Get(plantree.RootID)
	if err != nil {
		return Result2{}, err
	}
	if opt.ProjectName == "" {
		opt.ProjectName = root.Title
	}
	released, err := ReleaseAnswered(repo)
	if err != nil {
		return Result2{Released: released}, err
	}
	if _, err := HoldOpen(repo); err != nil {
		return Result2{Released: released}, err
	}
	facts := opt.Facts
	if strings.TrimSpace(facts) == "" {
		if facts, err = ReadFacts(repo); err != nil {
			return Result2{Released: released}, err
		}
	}
	overview, err := ReadOverview(repo.Dir())
	if err != nil {
		return Result2{}, err
	}
	overview = FitOverview(overview, OverviewCapBytes)
	prov, err := loadProvenance(repo)
	if err != nil {
		return Result2{}, err
	}
	chunks, err := BriefChunks(repo)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result2{}, err
	}
	work, err := pendingWork(repo, opt.Reconcile)
	if err != nil {
		return Result2{}, err
	}
	allQuestions, err := LoadQuestions(repo)
	if err != nil {
		return Result2{}, err
	}

	empty, err := EmptyTasks(repo)
	if err != nil {
		return Result2{}, err
	}
	res := Result2{EmptyTasks: len(empty), Released: released}
	for _, w := range work {
		ids := append([]string{w.task.ID}, idsOf(w.steps)...)
		excerpts := Excerpts(chunks, prov.chunksFor(ids), opt.BriefBytes)
		decisions := decisionsFor(allQuestions, append([]string{w.phase.ID}, ids...))
		// Batches are never mixed: a batch of steps being re-planned is
		// smaller and carries each step's previous specification, so keeping
		// the two apart is what keeps the prompt inside its budget.
		redo := setOf(idsOf(w.redo))
		batches := append(batchesOf(w.pending, opt.StepsPerPass), batchesOf(w.redo, reconcileStepsPerPass)...)
		for _, batch := range batches {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			batchIDs := idsOf(batch)
			var live []plantree.Node
			for _, s := range w.steps {
				if !onHold(s) && s.Planning.Stage != plantree.StageAwaitingAnswers {
					live = append(live, s)
				}
			}
			siblingIDs := idsOf(live)
			prompt := Pass2Prompt(Pass2Input{
				Project: opt.ProjectName, Overview: overview, Facts: facts, Decisions: decisions, Excerpts: excerpts, ExcerptsCap: opt.BriefBytes,
				Phase: w.phase, Task: w.task, Siblings: w.steps, Batch: batch,
			})
			out, err := askJSON(ctx, c, prompt, func(reply string) (Pass2Output, error) {
				return ParsePass2(reply, batchIDs, siblingIDs)
			})
			res.Passes++
			if err != nil {
				return res, taskError(w.task.ID, err)
			}
			asked, err := recordPass2Questions(repo, w.task.ID, out)
			res.Questions += asked
			if err != nil {
				return res, taskError(w.task.ID, err)
			}
			if err := applySpecs(repo, out, redo); err != nil {
				return res, taskError(w.task.ID, err)
			}
			markSpecified(w.steps, out)
			markAsked(w.steps, out)
			res.Steps += len(out.Steps)
			for _, s := range out.Steps {
				if redo[s.ID] {
					res.Reconciled++
				}
			}
			for _, s := range batch {
				cur, err := repo.Get(s.ID)
				if err != nil {
					return res, taskError(w.task.ID, err)
				}
				if needsSpec(cur) || (opt.Reconcile && needsRespec(cur)) {
					res.Unspecified++
				}
			}
		}
		res.Tasks++
	}
	return res, nil
}

// batchesOf cuts steps into consecutive batches of at most size, in order.
func batchesOf(steps []plantree.Node, size int) [][]plantree.Node {
	if size < 1 {
		size = 1
	}
	var out [][]plantree.Node
	for start := 0; start < len(steps); start += size {
		end := start + size
		if end > len(steps) {
			end = len(steps)
		}
		out = append(out, steps[start:end])
	}
	return out
}

func taskError(taskID string, err error) error {
	hint := ""
	if _, ok := llm.ContextLimitFromError(err); ok {
		hint = " (the server's context window is smaller than one pass needs: lower Options2.BriefBytes or StepsPerPass)"
	}
	return fmt.Errorf("plan: task %s: %w%s", taskID, err, hint)
}

// applySpecs writes each step's specification and moves it to the drafted
// stage. Update rejects a step whose specification is incomplete, so nothing
// half-specified reaches the tree. A step in redo is being specified again
// after a decision changed: its resume note is replaced with a summary of the
// specification this one overwrites, so what it used to say is not simply
// lost.
func applySpecs(repo *plantree.Repo, out Pass2Output, redo map[string]bool) error {
	for _, s := range out.Steps {
		cur, err := repo.Get(s.ID)
		if err != nil {
			return err
		}
		spec := s
		note := ""
		if redo[s.ID] {
			note = respecNote(cur.Work)
		}
		if _, err := repo.Update(s.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Work = &plantree.Work{
				Description:        spec.Description,
				TargetPaths:        spec.TargetPaths,
				AcceptanceCriteria: spec.AcceptanceCriteria,
				TestCommand:        spec.TestCommand,
			}
			n.DependsOn = spec.DependsOn
			n.Planning.Stage = plantree.StageDrafted
			if note != "" {
				n.ResumeNote = note
			}
			return nil
		}); err != nil {
			return fmt.Errorf("writing the specification of %s: %w", s.ID, err)
		}
	}
	return nil
}

// respecNote summarizes the specification a re-planned step is losing. It
// replaces the note the changed answer left, which the prompt for this pass
// has already shown the model, and only the most recent replacement is kept,
// so the note cannot grow.
func respecNote(old *plantree.Work) string {
	if old == nil || strings.TrimSpace(old.Description) == "" {
		return "re-planned after the owner changed a decision"
	}
	return cutBytes("re-planned; the previous specification was: "+oneLine(old.Description), reconcileNoteBytes)
}

// EmptyTasks returns, in tree order, the ids of every task that has no steps.
func EmptyTasks(repo *plantree.Repo) ([]string, error) {
	var out []string
	err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindTask {
			return nil
		}
		steps, err := repo.Children(n.ID)
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			out = append(out, n.ID)
		}
		return nil
	})
	return out, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/... -count=1 -v -run 'RePlan|Reconcil|WorstCase'`
Expected: every one PASS, with these logged sizes:

```
    prompt2_test.go:98: worst-case pass-2 prompt: 26481 bytes
    prompt2_test.go:116: multibyte worst-case pass-2 prompt: 26572 bytes
    reconcile_test.go:77: worst-case re-planning pass-2 prompt (ascii): 26728 bytes
    reconcile_test.go:77: worst-case re-planning pass-2 prompt (multibyte): 26800 bytes
```

Then run the whole package: `go test ./plantree/... -count=1`. Expected: `ok` for both.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/prompt2.go gophermind-lib/plantree/plan/runner2.go gophermind-lib/plantree/plan/runner2_test.go gophermind-lib/plantree/plan/reconcile_test.go
git commit -m "feat(plan): re-plan the steps a changed answer flagged

RunPass2 with Options2.Reconcile selects needs_reconciliation steps in
batches of three, shows each its previous specification and the decision
that changed, and returns it to drafted with a note saying what it
replaced. Result2.Reconciled counts them.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 4: What is left to do says why

**Files:**
- Modify: `gophermind-lib/plantree/plan/next.go`

**Interfaces:**
- Consumes: `plantree.Repo.NextActions`, `OpenQuestions`, `oneLine`, `cutBytes`, `reconcileNoteBytes` (Task 2).
- Produces: `func explainReconcile(repo *plantree.Repo, actions []plantree.Action) error`, and a `plan.NextActions` whose reconcile actions carry the step's resume note.

`TestNextActionsWhileStepsWaitToBeRePlanned` was written in Task 3's test file, because it needs the same fixtures. This task makes it pass.

- [ ] **Step 1: Run the failing test**

Run: `go test ./plantree/plan/... -run NextActionsWhileSteps -count=1`
Expected: FAIL, `reconcile reason = "a requirement changed", want the question whose answer changed`.

- [ ] **Step 2: Write the implementation**

Two edits to `plantree/plan/next.go`.

Edit 1. Find:

```go
// instead of repo.NextActions.
func NextActions(repo *plantree.Repo) (plantree.Actions, error) {
	a, err := repo.NextActions()
	if err != nil {
		return plantree.Actions{}, err
	}
	open, err := OpenQuestions(repo)
```

Replace with:

```go
// instead of repo.NextActions.
//
// Each reconcile action also says which decision changed, taken from the
// step's resume note, so the owner reads why the step must be planned again
// rather than only that it must. A reconcile action is runnable and keeps
// approval away until RunPass2 with Options2.Reconcile has redone the step.
func NextActions(repo *plantree.Repo) (plantree.Actions, error) {
	a, err := repo.NextActions()
	if err != nil {
		return plantree.Actions{}, err
	}
	if err := explainReconcile(repo, a.Runnable); err != nil {
		return plantree.Actions{}, err
	}
	open, err := OpenQuestions(repo)
```

Edit 2, at the end of the file. Find:

```go
	a.Blocked = append(a.Blocked, plantree.Action{
		Kind:   plantree.ActionAnswer,
		NodeID: plantree.RootID,
		Reason: fmt.Sprintf("%d open question(s) wait for an answer, first %s", len(open), open[0].ID),
	})
	return a, nil
}
```

Replace with:

```go
	a.Blocked = append(a.Blocked, plantree.Action{
		Kind:   plantree.ActionAnswer,
		NodeID: plantree.RootID,
		Reason: fmt.Sprintf("%d open question(s) wait for an answer, first %s", len(open), open[0].ID),
	})
	return a, nil
}

// explainReconcile replaces each reconcile action's generic reason with the
// step's resume note, which names the question whose answer changed. A step
// with no note keeps the generic reason.
func explainReconcile(repo *plantree.Repo, actions []plantree.Action) error {
	for i, x := range actions {
		if x.Kind != plantree.ActionReconcile {
			continue
		}
		n, err := repo.Get(x.NodeID)
		if err != nil {
			return err
		}
		if note := oneLine(n.ResumeNote); note != "" {
			actions[i].Reason = cutBytes(note, reconcileNoteBytes)
		}
	}
	return nil
}
```

- [ ] **Step 3: Run every check for Part A**

Run: `gofmt -w plantree && go test ./plantree/... -count=1 && go test -race ./plantree/... -short -count=1 && gofmt -l plantree && go vet ./plantree/... && go build ./...`
Expected: all `ok`, no gofmt output, vet clean, the whole module builds.

- [ ] **Step 4: Commit**

```bash
git add gophermind-lib/plantree/plan/next.go
git commit -m "feat(plan): next actions explain why a step must be re-planned

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 5: The question round component

**Files:**
- Create: `gophermind-lib/tui/question_round.go`, `question_round_test.go`
- Modify: `gophermind-lib/tui/input.go`

**Interfaces:**
- Consumes: `plan.Question`, `plan.Option`, `plan.Recommendation`, `plan.Answer`, `plan.QuestionAnswered`, `textarea`, `oneLine` (`update.go`), `wrappedRows` (extracted here).
- Produces:
  - `type roundMode int` with `roundAnswer`, `roundChange`
  - `type roundItem`, `roundAnswerOut{ID string; Answer plan.Answer; Change bool}`, `roundResult{Submitted, Cancelled bool}`
  - `type questionRound` with `newQuestionRound(mode, qs []plan.Question, why []string, width int)`, `Update(tea.KeyMsg) (questionRound, roundResult)`, `View() string`, `count()`, `canSubmit()`, `answers() []roundAnswerOut`
  - `const roundListRows = 7`, `roundNoteRows = 5`, `roundNoteLimit = 2000`, `roundWhyBytes = 400`
  - the round's styles, including `roundDialogStyle` used by `view.go` in Task 6
  - `func wrappedRows(value string, textWidth int) int` in `input.go`

The component is a value: `Update` takes a key and returns a new round, so a test drives it with no program, no channel and no agent. It owns no repository: the excerpts are handed to it, the answers are read out of it. Keys: up and down (or j and k) move between questions, left and right move the option cursor, space or enter chooses, `e` opens the note, `s` skips, Ctrl-S submits, Esc leaves the note and then cancels.

- [ ] **Step 1: Write the failing tests**

Create `tui/question_round_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/plantree/plan"
)

// roundQuestion builds a question with two options and a recommendation.
func roundQuestion(id, text string, multi bool) plan.Question {
	return plan.Question{
		ID: id, Question: text, Why: "it changes the schema",
		Options: []plan.Option{
			{ID: "opt-1", Label: "SQLite", Description: "embedded"},
			{ID: "opt-2", Label: "Postgres", Description: "server"},
		},
		MultiSelect:   multi,
		AllowFreeText: true,
		Recommended:   &plan.Recommendation{OptionIDs: []string{"opt-1"}, Rationale: "simplest to run"},
		Affects:       []string{"phase-001.task-001"},
		Status:        plan.QuestionOpen,
	}
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// pressRound sends each key in order and returns the round and the last result.
func pressRound(r questionRound, keys ...tea.KeyMsg) (questionRound, roundResult) {
	var res roundResult
	for _, k := range keys {
		r, res = r.Update(k)
	}
	return r, res
}

func TestQuestionRoundChoosesOneOptionUnlessMultiSelect(t *testing.T) {
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Which database?", false)}, nil, 80)
	if r.answeredCount() != 0 {
		t.Fatal("a fresh round starts with nothing chosen")
	}
	r, _ = pressRound(r, key(tea.KeySpace))
	if got := r.answers(); len(got) != 1 || len(got[0].Answer.OptionIDs) != 1 || got[0].Answer.OptionIDs[0] != "opt-1" {
		t.Fatalf("answers = %+v, want the option under the cursor", got)
	}
	// A single-answer question replaces rather than accumulates.
	r, _ = pressRound(r, key(tea.KeyRight), key(tea.KeySpace))
	got := r.answers()
	if len(got) != 1 || len(got[0].Answer.OptionIDs) != 1 || got[0].Answer.OptionIDs[0] != "opt-2" {
		t.Fatalf("answers = %+v, want only the second option", got)
	}
	// Choosing the same option again clears it.
	r, _ = pressRound(r, key(tea.KeySpace))
	if len(r.answers()) != 0 || r.answeredCount() != 0 {
		t.Errorf("a second press did not clear the choice: %+v", r.answers())
	}
}

func TestQuestionRoundMultiSelectAccumulatesInOptionOrder(t *testing.T) {
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Which caches?", true)}, nil, 80)
	r, _ = pressRound(r, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyLeft), key(tea.KeySpace))
	got := r.answers()
	if len(got) != 1 || strings.Join(got[0].Answer.OptionIDs, ",") != "opt-1,opt-2" {
		t.Errorf("answers = %+v, want both options in option order", got)
	}
}

func TestQuestionRoundNeverChoosesTheRecommendationForYou(t *testing.T) {
	q := roundQuestion("q-001", "Which database?", false)
	r := newQuestionRound(roundAnswer, []plan.Question{q}, nil, 80)
	if len(r.answers()) != 0 {
		t.Error("the recommendation was taken as an answer")
	}
	v := r.View()
	if !strings.Contains(v, "SQLite - embedded (recommended)") {
		t.Errorf("the recommendation is not marked:\n%s", v)
	}
	if !strings.Contains(v, "recommended because: simplest to run") {
		t.Errorf("the rationale is missing:\n%s", v)
	}
	if strings.Contains(v, "[x]") {
		t.Errorf("something is pre-selected:\n%s", v)
	}
}

func TestQuestionRoundFreeTextIsAlwaysAvailableAndGrows(t *testing.T) {
	q := roundQuestion("q-001", "Which database?", false)
	q.Options = nil // a question may have no options at all
	r := newQuestionRound(roundAnswer, []plan.Question{q}, nil, 80)
	if !strings.Contains(r.View(), "answer in your own words") {
		t.Error("a question with no options must say the answer is free text")
	}
	r, _ = pressRound(r, runes("e"))
	if !r.editing {
		t.Fatal("e did not open the note")
	}
	before := r.note.Height()
	r, _ = pressRound(r, runes("neither: use the existing store"))
	if !strings.Contains(r.View(), "neither: use the existing store") {
		t.Error("the note is not shown while it is being typed")
	}
	// Enough text to wrap several times must grow the box, up to its cap.
	r, _ = pressRound(r, runes(strings.Repeat("more words ", 60)))
	if r.note.Height() <= before || r.note.Height() > roundNoteRows {
		t.Errorf("note height = %d, want it grown to at most %d", r.note.Height(), roundNoteRows)
	}
	r, _ = pressRound(r, key(tea.KeyEsc))
	if r.editing {
		t.Fatal("esc did not leave the note")
	}
	got := r.answers()
	if len(got) != 1 || !strings.HasPrefix(got[0].Answer.Text, "neither: use the existing store") {
		t.Errorf("answers = %+v, want the typed note", got)
	}
	if got[0].Change {
		t.Error("an open question is answered, not changed")
	}
}

func TestQuestionRoundSkipLeavesAQuestionOpen(t *testing.T) {
	qs := []plan.Question{roundQuestion("q-001", "Which database?", false), roundQuestion("q-002", "Which framework?", false)}
	r := newQuestionRound(roundAnswer, qs, nil, 80)
	if r.canSubmit() {
		t.Fatal("an unanswered round must not be submittable")
	}
	r, _ = pressRound(r, key(tea.KeySpace), key(tea.KeyDown), runes("s"))
	if !r.canSubmit() {
		t.Fatal("answered plus skipped must be submittable")
	}
	got := r.answers()
	if len(got) != 1 || got[0].ID != "q-001" {
		t.Errorf("answers = %+v, want only the answered question", got)
	}
	if !strings.Contains(r.View(), "this question stays open") {
		t.Error("the view does not say a skipped question stays open")
	}
}

func TestQuestionRoundSubmitsOnlyWhenItMay(t *testing.T) {
	qs := []plan.Question{roundQuestion("q-001", "Which database?", false), roundQuestion("q-002", "Which framework?", false)}
	r := newQuestionRound(roundAnswer, qs, nil, 80)
	r, res := pressRound(r, key(tea.KeyCtrlS))
	if res.Submitted {
		t.Fatal("submitted with nothing answered")
	}
	r, _ = pressRound(r, key(tea.KeySpace), key(tea.KeyDown), key(tea.KeySpace))
	_, res = pressRound(r, key(tea.KeyCtrlS))
	if !res.Submitted {
		t.Fatal("a complete round did not submit")
	}
}

func TestQuestionRoundEscapeLeavesTheNoteBeforeItCancels(t *testing.T) {
	r := newQuestionRound(roundAnswer, []plan.Question{roundQuestion("q-001", "Which database?", false)}, nil, 80)
	r, _ = pressRound(r, runes("e"))
	r, res := pressRound(r, key(tea.KeyEsc))
	if res.Cancelled || r.editing {
		t.Fatalf("the first esc must only leave the note: cancelled=%v editing=%v", res.Cancelled, r.editing)
	}
	_, res = pressRound(r, key(tea.KeyEsc))
	if !res.Cancelled {
		t.Error("the second esc must cancel the round")
	}
}

func TestQuestionRoundViewShowsProgressTheCursorAndTheBriefExcerpt(t *testing.T) {
	qs := []plan.Question{
		roundQuestion("q-001", "Which database?", false),
		roundQuestion("q-002", "Which framework?", false),
		roundQuestion("q-003", "Which cache?", false),
	}
	r := newQuestionRound(roundAnswer, qs, []string{"[part 1 of 3]\nthe brief said storage matters"}, 80)
	v := r.View()
	if !strings.Contains(v, "Questions: 0 of 3 answered") {
		t.Errorf("no progress line:\n%s", v)
	}
	if !strings.Contains(v, "the brief said storage matters") {
		t.Errorf("the brief excerpt behind the question is missing:\n%s", v)
	}
	if !strings.Contains(v, "why: it changes the schema") {
		t.Errorf("the reason the question was asked is missing:\n%s", v)
	}
	r, _ = pressRound(r, key(tea.KeySpace), key(tea.KeyDown))
	v = r.View()
	if !strings.Contains(v, "Questions: 1 of 3 answered") || !strings.Contains(v, "Q2: Which framework?") {
		t.Errorf("progress or cursor did not move:\n%s", v)
	}
	// The second question has no excerpt, so nothing is shown for it.
	if strings.Contains(v, "the brief said storage matters") {
		t.Errorf("another question's excerpt leaked:\n%s", v)
	}
}

func TestQuestionRoundListWindowFollowsTheCursor(t *testing.T) {
	var qs []plan.Question
	for i := 0; i < roundListRows+5; i++ {
		qs = append(qs, roundQuestion("q-00"+string(rune('1'+i)), "Question "+string(rune('a'+i)), false))
	}
	r := newQuestionRound(roundAnswer, qs, nil, 80)
	if !strings.Contains(r.View(), "below)") {
		t.Error("a long round must say how many questions are below the window")
	}
	for i := 0; i < len(qs)-1; i++ {
		r, _ = pressRound(r, key(tea.KeyDown))
	}
	v := r.View()
	if !strings.Contains(v, "above)") || !strings.Contains(v, "Question "+string(rune('a'+len(qs)-1))) {
		t.Errorf("the window did not follow the cursor:\n%s", v)
	}
}

func TestQuestionRoundChangeModeStartsFromTheStoredAnswer(t *testing.T) {
	q := roundQuestion("q-001", "Which database?", false)
	q.Status = plan.QuestionAnswered
	q.Answer = &plan.Answer{OptionIDs: []string{"opt-1"}, Text: "for now"}
	other := roundQuestion("q-002", "Which framework?", false)
	other.Status = plan.QuestionAnswered
	other.Answer = &plan.Answer{OptionIDs: []string{"opt-2"}}

	r := newQuestionRound(roundChange, []plan.Question{q, other}, nil, 80)
	if !strings.Contains(r.View(), "0 of 2 changed") {
		t.Errorf("a revisiting round counts changes:\n%s", r.View())
	}
	if len(r.answers()) != 0 {
		t.Fatalf("leaving every answer alone must change nothing: %+v", r.answers())
	}
	if !r.canSubmit() {
		t.Fatal("a revisiting round may always be submitted")
	}
	r, _ = pressRound(r, key(tea.KeyRight), key(tea.KeySpace))
	got := r.answers()
	if len(got) != 1 || got[0].ID != "q-001" || !got[0].Change {
		t.Fatalf("answers = %+v, want one change", got)
	}
	if len(got[0].Answer.OptionIDs) != 1 || got[0].Answer.OptionIDs[0] != "opt-2" || got[0].Answer.Text != "for now" {
		t.Errorf("the changed answer = %+v, want the new option and the kept note", got[0].Answer)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./tui/ -run QuestionRound -count=1`
Expected: FAIL, `undefined: newQuestionRound`, `undefined: roundAnswer`, `undefined: roundNoteRows`.

- [ ] **Step 3: Write the implementation**

One edit to `tui/input.go`, extracting the row arithmetic so both boxes grow the same way. Find:

```go
	rows := 0
	for _, line := range strings.Split(value, "\n") {
		w := uniseg.StringWidth(line)
		// bubbles textarea appends a trailing padding row when content exactly fills
		// the last row, so we add 1 to account for it: w/textWidth + 1.
		// For w=0: 0/textWidth + 1 = 1 (empty line = 1 row).
		// For w=textWidth: textWidth/textWidth + 1 = 2 (exact multiple = 1 row + 1 padding).
		// For w=textWidth+k (0<k<textWidth): (textWidth+k)/textWidth + 1 = 1 + 1 = 2.
		lineRows := w/textWidth + 1
		if lineRows < 1 {
			lineRows = 1
		}
		rows += lineRows
	}
	if rows < 1 {
		rows = 1
	}
	if rows > maxInputRows {
		rows = maxInputRows
	}
	return rows
}
```

Replace with:

```go
	rows := wrappedRows(value, textWidth)
	if rows > maxInputRows {
		rows = maxInputRows
	}
	return rows
}

// wrappedRows returns the display rows value occupies in a textarea whose
// stored width is textWidth, counting soft wrapping. The question round uses
// it to grow its own note box (see question_round.go), so both boxes grow by
// the same arithmetic.
func wrappedRows(value string, textWidth int) int {
	if textWidth < 1 {
		textWidth = 1
	}
	rows := 0
	for _, line := range strings.Split(value, "\n") {
		w := uniseg.StringWidth(line)
		// bubbles textarea appends a trailing padding row when content exactly fills
		// the last row, so we add 1 to account for it: w/textWidth + 1.
		// For w=0: 0/textWidth + 1 = 1 (empty line = 1 row).
		// For w=textWidth: textWidth/textWidth + 1 = 2 (exact multiple = 1 row + 1 padding).
		// For w=textWidth+k (0<k<textWidth): (textWidth+k)/textWidth + 1 = 1 + 1 = 2.
		lineRows := w/textWidth + 1
		if lineRows < 1 {
			lineRows = 1
		}
		rows += lineRows
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}
```

Create `tui/question_round.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gophermind/gophermind-lib/plantree/plan"
)

// This file is the question round: the whole set of questions the planning
// passes asked, shown at once as a list with a cursor, so the owner answers
// them in ONE sitting rather than one modal prompt after another. It is a
// self-contained component: it owns no repository, no agent and no goroutine,
// takes key messages and returns itself, so it can be driven directly from a
// test. questions.go hosts it inside the model.

const (
	// roundListRows is the most questions listed around the cursor at once,
	// so a round of forty questions still fits on a screen.
	roundListRows = 7
	// roundNoteRows is the tallest the free-text box grows before it scrolls
	// its own content, and roundNoteLimit the most text it accepts (the store
	// rejects an answer longer than 2,000 characters).
	roundNoteRows  = 5
	roundNoteLimit = 2000
	// roundWhyBytes bounds the brief excerpt shown behind a question.
	roundWhyBytes = 400
)

// roundMode says what the round is for.
type roundMode int

const (
	// roundAnswer collects answers to open questions.
	roundAnswer roundMode = iota
	// roundChange revisits answered questions so the owner can change their
	// mind. Steps already specified from a changed answer are flagged for
	// re-planning (see plan.ChangeAnswer).
	roundChange
)

// roundItem is one question and the answer being composed for it.
type roundItem struct {
	q       plan.Question
	why     string // brief excerpt behind the question, "" when there is none
	chosen  map[string]bool
	note    string
	skipped bool
	opt     int // the option the cursor is on
}

// answered reports whether this item now carries an answer the store accepts:
// at least one option or some text.
func (it roundItem) answered() bool {
	return len(it.chosen) > 0 || strings.TrimSpace(it.note) != ""
}

// answer renders the item as an answer for the store.
func (it roundItem) answer() plan.Answer {
	a := plan.Answer{Text: strings.TrimSpace(it.note)}
	for _, o := range it.q.Options { // option order, not map order, so it is stable
		if it.chosen[o.ID] {
			a.OptionIDs = append(a.OptionIDs, o.ID)
		}
	}
	return a
}

// roundAnswerOut is one answer the round collected.
type roundAnswerOut struct {
	ID     string
	Answer plan.Answer
	// Change is true when this replaces an answer the question already had,
	// so the caller must use plan.ChangeAnswer rather than AnswerQuestion.
	Change bool
}

// roundResult is what one key did to the round.
type roundResult struct {
	Submitted bool
	Cancelled bool
}

// questionRound is the component. Its zero value is not usable; build it with
// newQuestionRound.
type questionRound struct {
	mode    roundMode
	items   []roundItem
	cur     int
	editing bool
	note    textarea.Model
	width   int
}

// newQuestionRound builds a round over qs. why[i] is the brief excerpt behind
// qs[i] (see plan.ExcerptsFor); a shorter or nil slice simply means some
// questions show no excerpt. In roundChange mode each item starts from the
// answer the question already has, so leaving it alone changes nothing.
func newQuestionRound(mode roundMode, qs []plan.Question, why []string, width int) questionRound {
	ta := textarea.New()
	ta.Placeholder = "anything the options do not cover…"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = roundNoteLimit
	ta.SetHeight(1)
	r := questionRound{mode: mode, note: ta, width: width}
	for i, q := range qs {
		it := roundItem{q: q, chosen: map[string]bool{}}
		if i < len(why) {
			it.why = why[i]
		}
		if mode == roundChange && q.Answer != nil {
			for _, id := range q.Answer.OptionIDs {
				it.chosen[id] = true
			}
			it.note = q.Answer.Text
		}
		r.items = append(r.items, it)
	}
	r.setWidth(width)
	r.loadNote()
	return r
}

func (r *questionRound) setWidth(w int) {
	r.width = w
	inner := w - 6
	if inner < 20 {
		inner = 20
	}
	r.note.SetWidth(inner)
}

// loadNote points the text box at the current item's note.
func (r *questionRound) loadNote() {
	if len(r.items) == 0 {
		return
	}
	r.note.SetValue(r.items[r.cur].note)
	r.growNote()
}

// saveNote copies the text box back into the current item.
func (r *questionRound) saveNote() {
	if len(r.items) == 0 {
		return
	}
	r.items[r.cur].note = r.note.Value()
	if strings.TrimSpace(r.items[r.cur].note) != "" {
		r.items[r.cur].skipped = false
	}
}

// growNote resizes the text box to its content, up to roundNoteRows.
func (r *questionRound) growNote() {
	w := r.note.Width()
	if w < 1 {
		w = 1
	}
	rows := 1
	if v := r.note.Value(); v != "" {
		rows = wrappedRows(v, w)
	}
	if rows > roundNoteRows {
		rows = roundNoteRows
	}
	r.note.SetHeight(rows)
}

// count returns how many questions are in the round.
func (r questionRound) count() int { return len(r.items) }

// answeredCount returns how many carry an answer.
func (r questionRound) answeredCount() int {
	n := 0
	for _, it := range r.items {
		if it.answered() {
			n++
		}
	}
	return n
}

// progressCount is what the progress line counts: answers given in a round
// that collects them, changes made in a round that revisits them.
func (r questionRound) progressCount() int {
	if r.mode == roundChange {
		return len(r.answers())
	}
	return r.answeredCount()
}

// canSubmit reports whether the round may be submitted: every question is
// answered or explicitly skipped. Skipping leaves a question open, so an
// owner is never trapped by one they cannot decide. A round that revisits
// answers can always be submitted; leaving everything alone submits nothing.
func (r questionRound) canSubmit() bool {
	if r.mode == roundChange {
		return true
	}
	for _, it := range r.items {
		if !it.answered() && !it.skipped {
			return false
		}
	}
	return true
}

// answers returns what to write to the store: the questions the owner
// answered, skipping the ones they skipped and, in a round that revisits
// answers, the ones they left exactly as they were.
func (r questionRound) answers() []roundAnswerOut {
	var out []roundAnswerOut
	for _, it := range r.items {
		if it.skipped || !it.answered() {
			continue
		}
		a := it.answer()
		change := r.mode == roundChange || it.q.Status == plan.QuestionAnswered
		if change && it.q.Answer != nil && sameRoundAnswer(*it.q.Answer, a) {
			continue
		}
		out = append(out, roundAnswerOut{ID: it.q.ID, Answer: a, Change: change})
	}
	return out
}

// sameRoundAnswer reports whether an answer is the one already stored. Option
// ids are produced in option order on both sides, so a plain comparison is
// enough.
func sameRoundAnswer(stored, now plan.Answer) bool {
	if strings.TrimSpace(stored.Text) != strings.TrimSpace(now.Text) || len(stored.OptionIDs) != len(now.OptionIDs) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range stored.OptionIDs {
		seen[id] = true
	}
	for _, id := range now.OptionIDs {
		if !seen[id] {
			return false
		}
	}
	return true
}

// Update handles one key. Every key belongs to the round while it is showing,
// except the ones the session handles first (Ctrl-C, and Esc, which reaches
// here only to leave the note box). The returned result says whether the
// round is finished.
func (r questionRound) Update(msg tea.KeyMsg) (questionRound, roundResult) {
	if len(r.items) == 0 {
		if msg.Type == tea.KeyEsc {
			return r, roundResult{Cancelled: true}
		}
		return r, roundResult{}
	}
	if r.editing {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyTab:
			r.saveNote()
			r.editing = false
			r.note.Blur()
			return r, roundResult{}
		}
		var cmd tea.Cmd
		r.note, cmd = r.note.Update(msg)
		_ = cmd // the round is driven by the session's own tick, not its own commands
		r.saveNote()
		r.growNote()
		return r, roundResult{}
	}

	switch msg.Type {
	case tea.KeyEsc:
		return r, roundResult{Cancelled: true}
	case tea.KeyUp:
		r.move(-1)
		return r, roundResult{}
	case tea.KeyDown:
		r.move(1)
		return r, roundResult{}
	case tea.KeyLeft:
		r.moveOption(-1)
		return r, roundResult{}
	case tea.KeyRight:
		r.moveOption(1)
		return r, roundResult{}
	case tea.KeyEnter, tea.KeySpace:
		r.toggle()
		return r, roundResult{}
	case tea.KeyCtrlS:
		if r.canSubmit() {
			return r, roundResult{Submitted: true}
		}
		return r, roundResult{}
	}

	switch strings.ToLower(msg.String()) {
	case "e":
		r.editing = true
		r.loadNote()
		r.note.Focus()
		r.note.CursorEnd()
	case "s":
		r.skip()
	case "k":
		r.move(-1)
	case "j":
		r.move(1)
	}
	return r, roundResult{}
}

func (r *questionRound) move(d int) {
	r.cur = (r.cur + d + len(r.items)) % len(r.items)
	r.loadNote()
}

func (r *questionRound) moveOption(d int) {
	it := &r.items[r.cur]
	if len(it.q.Options) == 0 {
		return
	}
	it.opt = (it.opt + d + len(it.q.Options)) % len(it.q.Options)
}

// toggle selects or clears the option under the cursor. A question that takes
// one answer clears the others, which is how a radio group behaves; a
// multi-select question accumulates. A recommendation is only ever a marker,
// so nothing is ever selected until the owner does it here.
func (r *questionRound) toggle() {
	it := &r.items[r.cur]
	if len(it.q.Options) == 0 {
		return
	}
	id := it.q.Options[it.opt].ID
	on := it.chosen[id]
	if !it.q.MultiSelect {
		it.chosen = map[string]bool{}
	}
	if on {
		delete(it.chosen, id)
	} else {
		it.chosen[id] = true
	}
	it.skipped = false
}

// skip leaves the current question unanswered and open. Nothing is written
// for it, so the next round asks it again.
func (r *questionRound) skip() {
	it := &r.items[r.cur]
	it.chosen = map[string]bool{}
	it.note = ""
	it.skipped = true
	r.loadNote()
}

// View renders the round: the progress line, a window of the list around the
// cursor, then the current question in full with its options, the brief
// excerpt behind it and its free-text box.
func (r questionRound) View() string {
	if len(r.items) == 0 {
		return roundTitleStyle.Render("No questions.")
	}
	var b strings.Builder
	verb := "answered"
	if r.mode == roundChange {
		verb = "changed"
	}
	b.WriteString(roundTitleStyle.Render(fmt.Sprintf("Questions: %d of %d %s", r.progressCount(), len(r.items), verb)))
	b.WriteString("\n")

	first, last := listWindow(r.cur, len(r.items), roundListRows)
	if first > 0 {
		b.WriteString(fmt.Sprintf("   (%d above)\n", first))
	}
	for i := first; i < last; i++ {
		it := r.items[i]
		mark := " "
		switch {
		case it.skipped:
			mark = "-"
		case it.answered():
			mark = "x"
		}
		line := fmt.Sprintf("  [%s] %d. %s", mark, i+1, oneLine(it.q.Question))
		if i == r.cur {
			line = roundCursorStyle.Render(">" + line[1:])
		}
		b.WriteString(line + "\n")
	}
	if last < len(r.items) {
		b.WriteString(fmt.Sprintf("   (%d below)\n", len(r.items)-last))
	}

	it := r.items[r.cur]
	b.WriteString("\n" + roundQuestionStyle.Render(fmt.Sprintf("Q%d: %s", r.cur+1, oneLine(it.q.Question))) + "\n")
	if why := oneLine(it.q.Why); why != "" {
		b.WriteString("  why: " + why + "\n")
	}
	if it.why != "" {
		b.WriteString("  from the brief: " + oneLine(cutRoundBytes(it.why, roundWhyBytes)) + "\n")
	}
	for i, o := range it.q.Options {
		box := " "
		if it.chosen[o.ID] {
			box = "x"
		}
		line := fmt.Sprintf("  [%s] %s", box, o.Label)
		if o.Description != "" {
			line += " - " + oneLine(o.Description)
		}
		if recommends(it.q, o.ID) {
			line += " (recommended)"
		}
		if i == it.opt {
			line = roundCursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(it.q.Options) == 0 {
		b.WriteString("  (no options: answer in your own words)\n")
	}
	if it.q.Recommended != nil && strings.TrimSpace(it.q.Recommended.Rationale) != "" {
		b.WriteString("  recommended because: " + oneLine(it.q.Recommended.Rationale) + "\n")
	}
	b.WriteString("  note: ")
	if r.editing {
		b.WriteString("\n" + r.note.View() + "\n")
	} else if n := oneLine(it.note); n != "" {
		b.WriteString(n + "\n")
	} else {
		b.WriteString("(none)\n")
	}
	if it.skipped {
		b.WriteString("  skipped: this question stays open\n")
	}
	b.WriteString(roundKeysStyle.Render(r.keyHelp()))
	return b.String()
}

func (r questionRound) keyHelp() string {
	if r.editing {
		return "typing a note · esc when done"
	}
	s := "up/down question · left/right option · space choose · e note · s skip · esc cancel"
	if r.canSubmit() {
		return s + " · ctrl+s submit"
	}
	return s + " · ctrl+s submit (answer or skip them all first)"
}

// recommends reports whether the question recommends this option. A
// recommendation is shown, never chosen.
func recommends(q plan.Question, optionID string) bool {
	if q.Recommended == nil {
		return false
	}
	for _, id := range q.Recommended.OptionIDs {
		if id == optionID {
			return true
		}
	}
	return false
}

// listWindow returns the half-open range of list rows to show so the cursor
// is inside it.
func listWindow(cur, n, rows int) (int, int) {
	if n <= rows {
		return 0, n
	}
	first := cur - rows/2
	if first < 0 {
		first = 0
	}
	if first+rows > n {
		first = n - rows
	}
	return first, first + rows
}

// cutRoundBytes shortens s to at most max bytes without splitting a rune.
func cutRoundBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

var (
	roundTitleStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"})
	roundQuestionStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#5AA6BC"})
	roundCursorStyle = lipgloss.NewStyle().Bold(true)
	roundKeysStyle   = lipgloss.NewStyle().Faint(true)
	roundDialogStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}).
				Padding(0, 1)
)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w tui && go test ./tui/ -count=1 && go vet ./tui/`
Expected: `ok`, vet clean. Every existing TUI test still passes: `wrappedRows` is an extraction, not a change in behavior.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/tui/question_round.go gophermind-lib/tui/question_round_test.go gophermind-lib/tui/input.go
git commit -m "feat(tui): the question round component

One round, every question listed with a cursor, options single or multi
select, the recommendation marked and never chosen, a growing free-text
note, and skip leaving a question open.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 6: /questions runs the round and the pass that follows

**Files:**
- Create: `gophermind-lib/tui/questions.go`, `questions_test.go`
- Modify: `gophermind-lib/tui/model.go`, `update.go`, `view.go`, `commands_registry.go`

**Interfaces:**
- Consumes: `questionRound` (Task 5), `plan.OpenQuestions`, `plan.LoadQuestions`, `plan.AnswerQuestion`, `plan.ChangeAnswer`, `plan.ExcerptsFor`, `plan.RunPass2`, `plan.NextActions`, `plan.ClientCompleter`, `phaseflow.PlanningDir`, `plantree.Open`, `m.sub`, `errMsg`, `waitFor`, `beginAttention`.
- Produces:
  - `type qPhase int` with `qNone`, `qAsking`, `qRunning`; `const maxRoundQuestions = 20`
  - `questionsProgressMsg`, `questionsDoneMsg{res plan.Result2; actions plantree.Actions}`
  - `model.qphase`, `model.round`, `model.completer plan.Completer`
  - `func (m model) handleQuestionsCommand(text string) (model, tea.Cmd)`, `handleRoundKey`, `submitRound`, `planCompleter`, `endRound`
  - `renderQuestionsResult`, `renderNextActions`, `questionsDialogText`
  - the `/questions` registry entry, so help and completion pick it up

The answers are written on the UI goroutine because writing them is local, quick, and must be finished before the pass reads the store; the pass itself is the slow part and runs like `/project-execute` does, on a goroutine posting to `m.sub`, cancellable with Ctrl-C or Esc.

- [ ] **Step 1: Write the failing tests**

Create `tui/questions_test.go`:

```go
package tui

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

var roundStepID = regexp.MustCompile(`phase-\d{3}\.task-\d{3}\.step-\d{3}`)

// specCompleter answers a pass-2 prompt with a specification for every step
// it asks for, and records what it was asked.
type specCompleter struct {
	mu      sync.Mutex
	prompts []string
}

func (c *specCompleter) Complete(_ context.Context, prompt string) (string, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, prompt)
	c.mu.Unlock()
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	var steps []plan.StepSpecOut
	if start >= 0 && end > start {
		for _, line := range strings.Split(prompt[start:end], "\n") {
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			if id := roundStepID.FindString(line); id != "" {
				steps = append(steps, plan.StepSpecOut{
					ID: id, Description: "build " + id, TargetPaths: []string{"main.go"},
					AcceptanceCriteria: []string{"it works"}, TestCommand: []string{"go", "test", "./..."},
					DependsOn: []string{},
				})
			}
		}
	}
	b, err := json.Marshal(plan.Pass2Output{Steps: steps})
	return string(b), err
}

func (c *specCompleter) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.prompts...)
}

// seedPlan writes a two-step plan into dir/.planning/plan with one open
// question holding both steps, which is the state a pass leaves behind.
func seedPlan(t *testing.T, dir string) *plantree.Repo {
	t.Helper()
	repo := plantree.Open(phaseflow.PlanningDir(dir))
	node := func(id, title string) plantree.Node {
		ref, err := plantree.ParentRef(id)
		if err != nil {
			t.Fatal(err)
		}
		n := plantree.Node{
			SchemaVersion: plantree.SchemaVersion, ID: id, Title: title, NodeRevision: 1,
			ContextDigest: "why " + id, DependsOn: []string{},
			Planning: plantree.Planning{Stage: plantree.StageSkeleton},
		}
		if id != plantree.RootID {
			n.ParentRef = &ref
		}
		if n.Kind() == plantree.KindStep {
			n.Status = plantree.StatusUntouched
		}
		return n
	}
	if err := repo.Init(node(plantree.RootID, "demo")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		if err := repo.Create(node(id, "node "+id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := plan.AddQuestions(repo, []plan.NewQuestion{{
		Question: "Which database?", Why: "the schema depends on it",
		Options:           []plan.NewOption{{Label: "SQLite", Description: "embedded"}, {Label: "Postgres", Description: "server"}},
		RecommendedLabels: []string{"SQLite"}, Rationale: "simplest to run",
		Affects: []string{"phase-001.task-001"}, Source: "chunk 1 of the brief",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.HoldOpen(repo); err != nil { // a real pass holds the steps it asked about
		t.Fatal(err)
	}
	return repo
}

// roundModel is a session whose planning passes run on a fake completer, in a
// temporary working directory holding a seeded plan.
func roundModel(t *testing.T) (model, *plantree.Repo, *specCompleter) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	repo := seedPlan(t, dir)
	m := testModel(t)
	c := &specCompleter{}
	m.completer = c
	return m, repo, c
}

// submit runs one line through handleSubmit.
func submit(t *testing.T, m model, line string) model {
	t.Helper()
	m.input.SetValue(line)
	next, _ := m.handleSubmit()
	return next
}

// keys drives the model's key handler, which is what the round is reached
// through in a real session.
func keys(t *testing.T, m model, msgs ...tea.KeyMsg) model {
	t.Helper()
	for _, k := range msgs {
		next, _ := m.handleKey(k)
		m = next.(model)
	}
	return m
}

// settle delivers whatever the background pass posts until the model is idle
// again, so a test sees the transcript a user would.
func settle(t *testing.T, m model) model {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for m.st != stateIdle {
		select {
		case msg := <-m.sub:
			next, _ := m.Update(msg)
			m = next.(model)
		case <-deadline:
			t.Fatalf("the pass did not finish; transcript:\n%s", m.content)
		}
	}
	return m
}

// waitPass reads messages until the background pass has reported, so nothing
// is still running when a test returns.
func waitPass(t *testing.T, m model) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-m.sub:
			switch msg.(type) {
			case questionsDoneMsg, errMsg:
				return
			}
		case <-deadline:
			t.Fatal("the pass did not report")
		}
	}
}

func TestSlashQuestionsNeedsAPlan(t *testing.T) {
	t.Chdir(t.TempDir())
	m := submit(t, testModel(t), "/questions")
	if m.qphase != qNone || !strings.Contains(m.content, "no plan in") {
		t.Errorf("phase = %v, transcript = %q", m.qphase, m.content)
	}
}

func TestSlashQuestionsOpensTheRoundAndShowsTheBriefBehindIt(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")
	if m.qphase != qAsking || m.round.count() != 1 {
		t.Fatalf("phase = %v with %d questions", m.qphase, m.round.count())
	}
	if !strings.Contains(m.content, "Answering 1 question(s)") {
		t.Errorf("transcript = %q", m.content)
	}
	v := m.View()
	if !strings.Contains(v, "Which database?") || !strings.Contains(v, "(recommended)") {
		t.Errorf("the round is not on screen:\n%s", v)
	}
	// There is no brief here, so the excerpt is simply absent, not an error.
	if strings.Contains(v, "from the brief:") {
		t.Errorf("an excerpt appeared without a brief:\n%s", v)
	}
}

func TestSlashQuestionsChangeNeedsAnAnsweredQuestion(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions change")
	if m.qphase != qNone || !strings.Contains(m.content, "no answered question to change") {
		t.Errorf("phase = %v, transcript = %q", m.qphase, m.content)
	}
}

func TestQuestionRoundEscapeWritesNothing(t *testing.T) {
	m, repo, c := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeyEsc))
	if m.qphase != qNone || !strings.Contains(m.content, "cancelled") {
		t.Errorf("phase = %v, transcript = %q", m.qphase, m.content)
	}
	open, err := plan.OpenQuestions(repo)
	if err != nil || len(open) != 1 {
		t.Errorf("the question was written after a cancel: %+v, %v", open, err)
	}
	if len(c.seen()) != 0 {
		t.Error("a cancelled round ran a pass")
	}
}

func TestQuestionRoundAnswerReleasesTheStepsAndSpecifiesThem(t *testing.T) {
	m, repo, c := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qRunning || m.st != stateWorking {
		t.Fatalf("phase = %v, state = %v, want a pass running", m.qphase, m.st)
	}
	m = settle(t, m)

	if m.qphase != qNone {
		t.Errorf("phase = %v after the pass", m.qphase)
	}
	for _, want := range []string{"questions: 1 written, 0 refused", "2 step(s) specified", "2 released by an answer", "next: approve the plan, every step is specified"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
	qs, err := plan.LoadQuestions(repo)
	if err != nil || len(qs) != 1 || qs[0].Status != plan.QuestionAnswered || qs[0].Answer.OptionIDs[0] != "opt-1" {
		t.Fatalf("stored question = %+v, %v", qs, err)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-001.step-002"} {
		n, err := repo.Get(id)
		if err != nil || n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s = %v, %v", id, n.Planning.Stage, err)
		}
	}
	if seen := c.seen(); len(seen) != 1 || !strings.Contains(seen[0], "Which database?") {
		t.Errorf("the pass did not see the decision: %d prompts", len(seen))
	}
}

func TestQuestionRoundWithoutASessionWritesTheAnswersAnyway(t *testing.T) {
	m, repo, _ := roundModel(t)
	m.completer = nil // no injected completer and no agent: nothing can run
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qNone || m.st != stateIdle {
		t.Fatalf("phase = %v, state = %v", m.qphase, m.st)
	}
	if !strings.Contains(m.content, "no active session") {
		t.Errorf("transcript = %q", m.content)
	}
	qs, _ := plan.LoadQuestions(repo)
	if len(qs) != 1 || qs[0].Status != plan.QuestionAnswered {
		t.Errorf("the answer was not written: %+v", qs)
	}
}

// TestQuestionRoundLeavesTheSessionKeysAlone covers the ordering in
// update.go: Ctrl-C and Esc keep their meaning while the round is showing.
func TestQuestionRoundLeavesTheSessionKeysAlone(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")

	// Ctrl-C while the round is showing quits, exactly as it does when idle.
	_, cmd := m.handleKey(key(tea.KeyCtrlC))
	if cmd == nil {
		t.Fatal("ctrl-c did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl-c produced %T, want tea.QuitMsg", cmd())
	}

	// Typing goes to the round, not to the input line, so nothing is left
	// behind in the prompt when the round ends.
	m = keys(t, m, runes("e"), runes("some note"), key(tea.KeyEsc), key(tea.KeyEsc))
	if m.qphase != qNone {
		t.Fatalf("phase = %v, want the round gone", m.qphase)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("input = %q, want the round to have kept the keys", got)
	}
	// With the round gone, "/exit" works as it always has.
	m.input.SetValue("/exit")
	_, cmd = m.handleKey(key(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("/exit did not quit after the round")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("/exit produced %T, want tea.QuitMsg", cmd())
	}
}

func TestSlashQuestionsIsRegistered(t *testing.T) {
	found := false
	for _, name := range commandNames() {
		if name == "/questions" {
			found = true
		}
	}
	if !found {
		t.Fatal("/questions missing from commandNames()")
	}
}

// TestQuestionRoundCancelledPassEndsThePhase covers the path a Ctrl-C during
// the pass takes: the cancelled turn arrives as an errMsg, and the session
// must come back to an ordinary idle prompt rather than a phase whose keys
// nothing handles.
func TestQuestionRoundCancelledPassEndsThePhase(t *testing.T) {
	m, _, _ := roundModel(t)
	m = submit(t, m, "/questions")
	m = keys(t, m, key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qRunning {
		t.Fatalf("phase = %v", m.qphase)
	}
	// The pass runs on a goroutine. Let it finish before the cancelled
	// message is delivered, so it is not still writing to the temporary
	// directory when the test ends.
	waitPass(t, m)
	next, _ := m.Update(errMsg{err: context.Canceled})
	m = next.(model)
	if m.qphase != qNone || m.st != stateIdle || m.cancel != nil {
		t.Errorf("after a cancelled pass: phase=%v state=%v cancel=%v", m.qphase, m.st, m.cancel != nil)
	}
	if !strings.Contains(m.content, "cancelled") {
		t.Errorf("transcript = %q", m.content)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./tui/ -run 'Questions|QuestionRound' -count=1`
Expected: FAIL, `m.completer undefined`, `undefined: qNone`, `m.qphase undefined`.

- [ ] **Step 3: Write the implementation**

One edit to `tui/commands_registry.go`. Find:

```go
	{Name: "/project-execute", Arg: "", Desc: "run every pending task in the approved plan autonomously"},
```

Replace with:

```go
	{Name: "/project-execute", Arg: "", Desc: "run every pending task in the approved plan autonomously"},
	{Name: "/questions", Arg: "[change]", Desc: "answer the plan's open questions in one round (\"change\" revisits answered ones)"},
```

Two edits to `tui/model.go`. Find:

```go
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/prompthistory"
```

Replace with:

```go
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree/plan"
	"gophermind/gophermind-lib/prompthistory"
```

Find:

```go
	// execOutcomes accumulates /project-execute's finished tasks as they
```

Replace with:

```go
	// Question round state (see questions.go and question_round.go). qphase is
	// qNone unless the round is showing or its pass is running; round holds
	// the answers being composed. completer overrides the planning completer
	// so a test can drive the round with no agent; nil means build one from
	// this session's client.
	qphase    qPhase
	round     questionRound
	completer plan.Completer

	// execOutcomes accumulates /project-execute's finished tasks as they
```

Five edits to `tui/update.go`. Edit 1, the new messages. Find:

```go
	case execDoneMsg:
```

Replace with:

```go
	case questionsProgressMsg:
		m.appendLine(string(msg))
		m.sync()
		return m, waitFor(m.sub)

	case questionsDoneMsg:
		m.appendLine(renderQuestionsResult(msg.res))
		m.appendLine(renderNextActions(msg.actions))
		m.endRound()
		m.st = stateIdle
		m.cancel = nil
		m.sync()
		return m, tea.Batch(m.beginAttention(), waitFor(m.sub))

	case execDoneMsg:
```

Edit 2, a cancelled or failed pass ends the round. Find:

```go
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		m.sync()
		return m, tea.Batch(m.beginAttention(), waitFor(m.sub))
	}
	return m, nil
}
```

Replace with:

```go
		m.stream = ""
		m.st = stateIdle
		m.cancel = nil
		// A cancelled or failed pass ends the round with it, so the session
		// is not left in a phase whose keys nothing handles.
		m.endRound()
		m.sync()
		return m, tea.Batch(m.beginAttention(), waitFor(m.sub))
	}
	return m, nil
}
```

Edit 3, Esc. Find:

```go
	case tea.KeyEsc:
		// An active suggestion (ghost or menu) eats the first Esc — dismiss it
		// instead of the cancel/interrupt behavior below. A second Esc (or Esc
		// with nothing active) falls through to that behavior as before.
```

Replace with:

```go
	case tea.KeyEsc:
		// The question round takes Esc while it is showing: the first one
		// leaves the note box, the second cancels the round. The cancel and
		// interrupt behavior below is unchanged everywhere else, including
		// while the round's pass is running.
		if m.qphase == qAsking {
			return m.handleRoundKey(msg)
		}
		// An active suggestion (ghost or menu) eats the first Esc — dismiss it
		// instead of the cancel/interrupt behavior below. A second Esc (or Esc
		// with nothing active) falls through to that behavior as before.
```

Edit 4, the key routing, which goes after the approval block and before predictive text, so Ctrl-C, Esc and "/exit" all keep running first. Find:

```go
	// Predictive-text gets first refusal on keys while idle (Tab/→ accept a
	// suggestion, ↑/↓ navigate an open menu, ...). Suggestions are never
```

Replace with:

```go
	// While the question round is showing it owns the keyboard. Ctrl-C, Esc
	// and "/exit" are handled above, so none of them is taken over here; the
	// round consumes everything else, which is why "/exit" cannot be typed
	// until Esc has left the round.
	if m.qphase == qAsking {
		return m.handleRoundKey(msg)
	}

	// Predictive-text gets first refusal on keys while idle (Tab/→ accept a
	// suggestion, ↑/↓ navigate an open menu, ...). Suggestions are never
```

Edit 5, the command. Find:

```go
	if strings.Fields(text)[0] == "/project-execute" {
		return m.handleProjectExecuteCommand()
	}
```

Replace with:

```go
	if strings.Fields(text)[0] == "/project-execute" {
		return m.handleProjectExecuteCommand()
	}

	// "/questions" opens the question round over the plan tree in
	// .planning/plan; see questions.go.
	if strings.Fields(text)[0] == "/questions" {
		return m.handleQuestionsCommand(text)
	}
```

Three edits to `tui/view.go`, the first two switching completion off while the round is up, exactly as `m.proj` does. Find:

```go
	if m.st == stateIdle && m.proj == projNone && m.complete.Mode() == bubblecomplete.ModeMenu {
```

Replace with:

```go
	if m.st == stateIdle && m.proj == projNone && m.qphase == qNone && m.complete.Mode() == bubblecomplete.ModeMenu {
```

Find:

```go
	if m.st == stateIdle && m.proj == projNone && m.complete.Mode() == bubblecomplete.ModeGhost {
```

Replace with:

```go
	if m.st == stateIdle && m.proj == projNone && m.qphase == qNone && m.complete.Mode() == bubblecomplete.ModeGhost {
```

Find:

```go
	// During a guided /project flow, show a dialog panel above the input.
```

Replace with:

```go
	// The question round takes the panel above the input while it is showing,
	// and keeps a one-line panel while its pass runs.
	if m.qphase != qNone {
		panel := questionsDialogText(m.qphase)
		if m.qphase == qAsking {
			panel = m.round.View()
		}
		return lipgloss.JoinVertical(
			lipgloss.Left,
			vp.View(),
			roundDialogStyle.Width(width).Render(panel),
			boxStyle.Width(width).Render(inputContent),
			status,
		)
	}

	// During a guided /project flow, show a dialog panel above the input.
```

Create `tui/questions.go`:

```go
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/plantree"
	"gophermind/gophermind-lib/plantree/plan"
)

// This file hosts the question round (question_round.go) inside the session:
// the "/questions" command, the writes to the question store, and the pass-2
// run that follows. M6 makes "/project" enter the same round; until then this
// command is how the round is reached, and it works on the plan tree in
// <cwd>/.planning/plan.

// qPhase is the step of the question round the model is in.
type qPhase int

const (
	qNone    qPhase = iota
	qAsking         // the round is showing and owns the keyboard
	qRunning        // the answers are written and pass 2 is running
)

// maxRoundQuestions bounds one round, so a pass that asked forty questions
// still produces a screen a person can work through. The rest stay open and
// the next "/questions" asks them.
const maxRoundQuestions = 20

// questionsProgressMsg is a line from the pass-2 goroutine.
type questionsProgressMsg string

// questionsDoneMsg carries the finished pass and what the plan needs next.
type questionsDoneMsg struct {
	res     plan.Result2
	actions plantree.Actions
}

// planRepo opens the plan tree of the current working directory.
func planRepo() (*plantree.Repo, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return plantree.Open(phaseflow.PlanningDir(root)), nil
}

// planCompleter returns the completer planning passes run on: the injected
// one if a test supplied it, otherwise this session's client in a fresh
// two-message conversation. It is nil when there is no session, which is what
// makes the round usable in a test with no agent.
func (m model) planCompleter() plan.Completer {
	if m.completer != nil {
		return m.completer
	}
	if m.agent == nil {
		return nil
	}
	return plan.ClientCompleter{Client: m.agent.LLM()}
}

// handleQuestionsCommand dispatches "/questions" and "/questions change".
// With open questions it shows them for answering; with none (or when change
// is asked for) it shows the answered ones so the owner can change their
// mind.
func (m model) handleQuestionsCommand(text string) (model, tea.Cmd) {
	repo, err := planRepo()
	if err != nil {
		return m.questionsError(err.Error())
	}
	if _, err := repo.Get(plantree.RootID); err != nil {
		return m.questionsError("no plan in " + repo.Dir() + " yet")
	}
	wantChange := len(strings.Fields(text)) > 1 && strings.EqualFold(strings.Fields(text)[1], "change")

	open, err := plan.OpenQuestions(repo)
	if err != nil {
		return m.questionsError(err.Error())
	}
	mode, qs := roundAnswer, open
	if wantChange || len(open) == 0 {
		answered, err := answeredQuestions(repo)
		if err != nil {
			return m.questionsError(err.Error())
		}
		if len(answered) == 0 {
			if wantChange {
				return m.questionsError("no answered question to change")
			}
			return m.questionsError("no open questions; nothing to answer")
		}
		mode, qs = roundChange, answered
	}
	if len(qs) > maxRoundQuestions {
		m.appendLine(fmt.Sprintf("questions: showing the first %d of %d; run /questions again for the rest", maxRoundQuestions, len(qs)))
		qs = qs[:maxRoundQuestions]
	}

	why := make([]string, len(qs))
	for i, q := range qs {
		// The excerpt is context, so a failure to read it must not stop the
		// round: the question is still answerable without it.
		if ex, err := plan.ExcerptsFor(repo, q.Affects, roundWhyBytes); err == nil {
			why[i] = ex
		}
	}
	m.round = newQuestionRound(mode, qs, why, m.width-4)
	m.qphase = qAsking
	verb := "Answering"
	if mode == roundChange {
		verb = "Revisiting"
	}
	m.appendLine(roundTitleStyle.Render(fmt.Sprintf("%s %d question(s) from the plan", verb, len(qs))))
	m.sync()
	return m, nil
}

// answeredQuestions returns the answered questions, newest last.
func answeredQuestions(repo *plantree.Repo) ([]plan.Question, error) {
	all, err := plan.LoadQuestions(repo)
	if err != nil {
		return nil, err
	}
	var out []plan.Question
	for _, q := range all {
		if q.Status == plan.QuestionAnswered {
			out = append(out, q)
		}
	}
	return out, nil
}

func (m model) questionsError(detail string) (model, tea.Cmd) {
	m.appendLine("questions: " + detail)
	m.sync()
	return m, nil
}

// handleRoundKey gives one key to the round and acts on what it returns. It
// runs after the session's own keys (Ctrl-C, Esc, "/exit"), so none of those
// is taken over by the round.
func (m model) handleRoundKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r, res := m.round.Update(msg)
	m.round = r
	switch {
	case res.Cancelled:
		m.endRound()
		m.appendLine("questions: cancelled, nothing was written")
		m.sync()
		return m, nil
	case res.Submitted:
		return m.submitRound()
	}
	return m, nil
}

// endRound drops the round and its state.
func (m *model) endRound() {
	m.qphase = qNone
	m.round = questionRound{}
}

// submitRound writes the answers the round collected and starts the pass that
// acts on them. Writing is done here rather than in the goroutine because it
// is local, quick and must be finished before anything reads the store; the
// model pass that follows is the slow part.
func (m model) submitRound() (tea.Model, tea.Cmd) {
	repo, err := planRepo()
	if err != nil {
		m.endRound()
		return m.questionsErrorModel(err.Error())
	}
	answers := m.round.answers()
	m.endRound()
	if len(answers) == 0 {
		m.appendLine("questions: nothing was answered or changed")
		m.sync()
		return m, nil
	}

	written, changed, failed := 0, false, 0
	for _, a := range answers {
		if !a.Change {
			if _, err := plan.AnswerQuestion(repo, a.ID, a.Answer); err != nil {
				m.appendLine("questions: " + a.ID + ": " + err.Error())
				failed++
				continue
			}
			written++
			continue
		}
		q, rec, err := plan.ChangeAnswer(repo, a.ID, a.Answer)
		if err != nil {
			m.appendLine("questions: " + a.ID + ": " + err.Error())
			failed++
			continue
		}
		written++
		changed = true
		m.appendLine(fmt.Sprintf("%s changed: %d step(s) to re-plan", q.ID, len(rec.Flagged)))
		if len(rec.Executed) > 0 {
			m.appendLine("  already running or finished, left alone: " + strings.Join(rec.Executed, ", "))
		}
	}
	m.appendLine(fmt.Sprintf("questions: %d written, %d refused", written, failed))
	if written == 0 {
		m.sync()
		return m, nil
	}

	c := m.planCompleter()
	if c == nil {
		m.appendLine("questions: no active session, so nothing was re-planned; run /questions again with a session")
		m.sync()
		return m, nil
	}
	m.qphase = qRunning
	m.st = stateWorking
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	sub := m.sub
	reconcile := changed
	go func() {
		// RunPass2 reports nothing until it returns (its progress counters are
		// per call, not per step), so this is the one line the run can promise
		// before its result.
		sub <- questionsProgressMsg("specifying the steps the answers released…")
		res, err := plan.RunPass2(ctx, repo, c, plan.Options2{Reconcile: reconcile})
		if err != nil {
			sub <- errMsg{err: err}
			return
		}
		actions, err := plan.NextActions(repo)
		if err != nil {
			sub <- errMsg{err: err}
			return
		}
		sub <- questionsDoneMsg{res: res, actions: actions}
	}()
	m.sync()
	return m, nil
}

// questionsErrorModel is questionsError for the callers that return tea.Model.
func (m model) questionsErrorModel(detail string) (tea.Model, tea.Cmd) {
	nm, cmd := m.questionsError(detail)
	return nm, cmd
}

// renderQuestionsResult is the transcript summary of a finished pass.
func renderQuestionsResult(res plan.Result2) string {
	line := fmt.Sprintf("plan updated: %d step(s) specified in %d pass(es)", res.Steps, res.Passes)
	if res.Reconciled > 0 {
		line += fmt.Sprintf(", %d re-planned", res.Reconciled)
	}
	if res.Released > 0 {
		line += fmt.Sprintf(", %d released by an answer", res.Released)
	}
	if res.Questions > 0 {
		line += fmt.Sprintf(", %d new question(s)", res.Questions)
	}
	if res.Unspecified > 0 {
		line += fmt.Sprintf(", %d still unspecified", res.Unspecified)
	}
	return line
}

// renderNextActions is the transcript view of what the plan needs next, at
// most a few lines however large the plan is.
func renderNextActions(a plantree.Actions) string {
	if len(a.Runnable) == 0 && len(a.Blocked) == 0 {
		return "next: nothing, the plan is complete"
	}
	if len(a.Blocked) == 0 && len(a.Runnable) == 1 && a.Runnable[0].Kind == plantree.ActionApprove {
		return "next: approve the plan, every step is specified"
	}
	const shown = 5
	var lines []string
	add := func(prefix string, actions []plantree.Action) {
		for i, x := range actions {
			if i == shown {
				lines = append(lines, fmt.Sprintf("  (%d more %s)", len(actions)-shown, strings.TrimSpace(prefix)))
				return
			}
			lines = append(lines, fmt.Sprintf("  %s %s %s: %s", prefix, x.Kind, x.NodeID, oneLine(x.Reason)))
		}
	}
	add("next", a.Runnable)
	add("blocked", a.Blocked)
	return strings.Join(lines, "\n")
}

// questionsDialogText is the line shown in the dialog panel while a round or
// its pass is active.
func questionsDialogText(p qPhase) string {
	if p == qRunning {
		return "questions · re-planning the affected steps · esc or ctrl-c to stop"
	}
	return ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w tui && go test ./tui/ -count=1 && go vet ./tui/`
Expected: `ok`, vet clean. Note that `TestSlashHelpListsRegisteredCommands` now also covers `/questions`.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/tui/questions.go gophermind-lib/tui/questions_test.go gophermind-lib/tui/model.go gophermind-lib/tui/update.go gophermind-lib/tui/view.go gophermind-lib/tui/commands_registry.go
git commit -m "feat(tui): /questions runs the round and the pass that follows

The round opens over .planning/plan, writes each answer with
AnswerQuestion or ChangeAnswer, then runs pass 2 on a goroutine with
Reconcile set when an answer changed, and reports the result and what the
plan needs next.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 7: The whole loop in one session

**Files:**
- Modify: `gophermind-lib/tui/questions_test.go` (append one test)

**Interfaces:**
- Consumes: `roundModel`, `submit`, `keys`, `settle`, `specCompleter` (Task 6), `plan.LoadQuestions`, `plan.NextActions`.
- Produces: `TestQuestionRoundEndToEnd`, which answers the open question, lets pass 2 specify the steps, changes that answer, lets the reconciling pass re-specify exactly the steps it invalidated, and ends with nothing left but approval.

- [ ] **Step 1: Write the test**

Append to `tui/questions_test.go`:

```go
// TestQuestionRoundEndToEnd is the whole M5 loop in one session: answer the
// open question, let pass 2 specify the steps, then change that answer, let
// the reconciling pass re-specify exactly the steps it invalidated, and end
// with nothing left but approval.
func TestQuestionRoundEndToEnd(t *testing.T) {
	m, repo, c := roundModel(t)

	m = settle(t, keys(t, submit(t, m, "/questions"), key(tea.KeySpace), key(tea.KeyCtrlS)))
	if !strings.Contains(m.content, "next: approve the plan, every step is specified") {
		t.Fatalf("after answering, the plan is not complete:\n%s", m.content)
	}

	m = submit(t, m, "/questions")
	if m.qphase != qAsking || m.round.mode != roundChange {
		t.Fatalf("with nothing open, /questions must offer the answered ones: phase=%v mode=%v", m.qphase, m.round.mode)
	}
	// Choose the other option and submit: this invalidates both specifications.
	m = keys(t, m, key(tea.KeyRight), key(tea.KeySpace), key(tea.KeyCtrlS))
	if m.qphase != qRunning {
		t.Fatalf("a changed answer did not start a pass: %v, %q", m.qphase, m.content)
	}
	m = settle(t, m)

	for _, want := range []string{"q-001 changed: 2 step(s) to re-plan", "2 re-planned", "next: approve the plan, every step is specified"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript is missing %q:\n%s", want, m.content)
		}
	}
	qs, err := plan.LoadQuestions(repo)
	if err != nil || len(qs) != 1 || len(qs[0].PriorAnswers) != 1 || qs[0].Answer.OptionIDs[0] != "opt-2" {
		t.Fatalf("stored question = %+v, %v", qs, err)
	}
	actions, err := plan.NextActions(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions.Blocked) != 0 || len(actions.Runnable) != 1 || actions.Runnable[0].Kind != plantree.ActionApprove {
		t.Errorf("NextActions = %+v, want only approval", actions)
	}
	// Three passes: one for the first two steps, then the re-planning pass.
	if seen := c.seen(); len(seen) != 2 {
		t.Errorf("%d passes, want 2", len(seen))
	} else if !strings.Contains(seen[1], "previous specification: build phase-001.task-001.step-001") {
		t.Error("the re-planning pass was not shown what it was replacing")
	}
}
```

- [ ] **Step 2: Run it**

Run: `gofmt -w tui && go test ./tui/ -run EndToEnd -count=1 -v`
Expected: PASS. (It adds no production code: it checks that Tasks 1 to 6 work together.)

- [ ] **Step 3: Run every check**

Run: `go test ./plantree/... ./tui/... -count=1 && go test -race ./plantree/... ./tui/... -short -count=1 && gofmt -l plantree tui && go vet ./... && go build ./...`
Expected: all `ok`, no gofmt output, vet clean, the whole module builds.

- [ ] **Step 4: Commit**

```bash
git add gophermind-lib/tui/questions_test.go
git commit -m "test(tui): the question round end to end

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

## Self-review (M5)

- **Design coverage:** the questions the passes collected are asked in one round, with options, multi-select, a recommendation that is never an answer and free text; the brief behind each question is shown; an answer can be changed afterwards; only the steps that answer invalidated are planned again; approval stays away until nothing is open and nothing waits to be re-planned. Approval itself, the export and `/project` entering the round are M6 by design.
- **M4 carry-forward resolved here:** `ExcerptsFor` (provenance readable outside the package), changing and reopening an answer, `StageNeedsReconciliation` no longer a dead end (an explicit re-plan path plus a named set of steps to re-specify, through `Options2.Reconcile`), and `Node.ResumeNote` put to use for both the reason a step must be redone and a summary of what the redo replaced.
- **Placeholders:** none. Every step carries full code, and the code was run green task by task in this order before the plan was generated.
- **Types:** `Question`, `PriorAnswer`, `Reconciled`, `Options2`, `Result2`, `questionRound`, `roundAnswerOut`, `roundResult`, `qPhase` are defined once and used with the same names later.
- **Known limits:**
  - A question asked about a step that is being re-planned holds nothing (`needsSpec` excludes that stage by design, decision 6). The step stays at `needs_reconciliation`, which keeps approval away, and the next reconciling pass sees the new decision, but the step is not parked at `awaiting_answers` like a fresh one would be.
  - A step that is in progress or completed is never flagged. It is reported in `Reconciled.Executed` and the transcript names it; deciding what to do about finished work that a changed decision invalidates is left to the owner.
  - The re-planning prompt shows the previous specification's description only, not its acceptance criteria, target paths or test command.
  - `RunPass2` still reports nothing until it returns, so the round's only progress line is the one posted before the pass starts. Exporting per-step progress is on the M6 list.
  - Only the most recent replacement survives in a step's resume note; the one before it is overwritten. The question's own history keeps five answers.
  - While the round is showing it consumes ordinary keys, so "/exit" cannot be typed until Esc has left the round. Ctrl-C keeps its meaning throughout.
  - One round shows at most `maxRoundQuestions` (20) questions; the rest stay open for the next `/questions`.
  - Two sessions running `/questions` at once can each run a pass over the same tree. The questions store has a cross-process lock; the run lock over both passes is still M6.

## Definition of done (M5)

`go test ./plantree/... ./tui/... -count=1`, `go test -race ./plantree/... ./tui/... -short -count=1`, `gofmt -l plantree tui` (empty), `go vet ./...` and `go build ./...` all clean; seven commits; the worst-case pass-2 prompt pinned under 27,000 bytes for an ordinary pass and for a re-planning pass; and a test that drives a real session model through answering a question, specifying the steps it released, changing that answer, re-planning exactly the steps it invalidated, and ending with `plan.NextActions` offering only approval.

## Deferred to M6

- Approval itself, and the export of the approved tree to the legacy `assignments.json` (task-level rows).
- `/project <name> <brief>` entering this same round instead of `/questions`, and the resume wiring around it.
- One run lock over `RunPass1`, `RunPass2` and the provenance load-modify-write, so two sessions cannot run passes over one tree.
- A home for agent, model and wave (the node schema is closed), and a rule for wave.
- Deriving `ChunkBytes`, `BriefBytes` and `StepsPerPass` from the model's context window instead of the fixed caps this plan measures against.
- Everything else still standing on the M3 and M4 outcome lists: `Summarize` deriving only reviewed or untouched, no node removal (a skipped step blocks approval forever), a task having no description to export, `ExtractJSON` having no production caller, and exporting per-call progress.
