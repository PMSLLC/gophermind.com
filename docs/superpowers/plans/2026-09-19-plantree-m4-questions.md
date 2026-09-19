# plantree M4: questions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the passes ask questions instead of guessing, record the owner's answers, hold the steps a question affects until it is answered, show the decisions already made to pass 2, and give pass 2 a bounded block of project facts so it stops guessing test commands.

**Architecture:** Everything extends the package `gophermind-lib/plantree/plan`. A question store (`questions.json` beside the tree) holds each question with its options, an optional recommendation, the node ids it affects, and, once answered, the answer. Pass 1 and pass 2 replies gain an optional `questions` array, validated strictly. A question's affected steps move to stage `awaiting_answers`, which `NextActions` already reports as a blocked `answer` action and which pass 2 skips. `ReleaseAnswered` moves a step back to `inspected` once no open question affects it; pass 2 then specifies it with the answer shown in its prompt. Project facts live in `facts.md` and appear in every pass-2 prompt.

**Tech Stack:** Go (module `gophermind/gophermind-lib`), standard library, the existing `plantree`, `lockfile` and `llm` packages.

**Spec:** `docs/superpowers/specs/2026-09-19-brief-workflow-design.md` (approved design: questions are collected during the passes and asked in one round, each with options plus a freeform answer), `docs/superpowers/plans/2026-09-19-brief-workflow-roadmap.md` (M3 outcome and the carry-forward decisions this plan implements).

## Global Constraints

- All commands run from `/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com/gophermind-lib`.
- Test command: `go test ./plantree/... -count=1` (about 15 seconds). Fast loop: add `-short`. Race check: `go test -race ./plantree/... -short -count=1`.
- Run `gofmt -w plantree` before every check, then `gofmt -l plantree` must print nothing. `go vet ./plantree/...` and `go build ./...` must be clean.
- Package `plantree/plan` may import `plantree`, `lockfile` and `llm` only. Nothing in `plantree` may import `phaseflow`.
- The code and tests in this plan were written and run green, task by task in this order, before the plan was generated. Copy them exactly. Where a task says "Replace the whole of" a file, the file's complete new content is given: write it exactly, replacing the old file. If a real compile or vet error appears, fix it minimally and disclose the fix in your report. If a test fails, report BLOCKED with specifics instead of editing the test.
- Do NOT run `git switch`, `git checkout`, `git branch`, `git reset` or `git stash`, and do not use `--gw-force`: a git wrapper blocks them. Only `git add`, `git commit`, `git status`, `git diff` and `git log` are needed.
- No em dashes and no emojis in code, comments or commit messages.
- Commit messages end with these two lines:
  `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr`

## Decisions made for M4

1. **Questions live in package `plan`**, not a new package, so they can use the shared helpers (`askJSON`, `parseFirst`, `clip`, `cutBytes`) that are package-private.
2. **A question is a decision, never a default.** `questions.json` (strict, `schema_version` 1) holds id (`q-001`), text, why, options (`opt-1`, `opt-2`, ...), `multi_select`, an optional recommendation with a rationale (a recommendation is never an answer), the node ids it affects, where it was asked, its status, and its answer. Every question accepts free text as well as its options, and a question may have no options at all. A question whose text (ignoring case and spacing) was already asked, open or answered, is not added again, so replaying a pass after a crash asks nothing twice.
3. **Pass 1 questions affect phase or task titles.** They are matched, ignoring case and spacing, against the nodes that chunk created or reused; a title that matches nothing is ignored. Every step under an affected node that still waits for its specification moves to stage `awaiting_answers`.
4. **Pass 2 questions affect step ids from the batch.** A step named in a question must not also be specified: it is left out of `steps`, held at `awaiting_answers`, and counts as covered. At most 5 questions per reply, each with 2 to 8 options (or none).
5. **Answers change state only.** `ReleaseAnswered` moves a step back to `inspected` once no open question affects it, directly or through the task or phase above it. `RunPass2` calls it first, so a step released by an answer is specified by the next run, with the decision (at most 6 lines of 300 bytes) in its prompt. An answer that changes the plan's structure (drop a task, add a phase) is M5, as is reopening or changing an answer.
6. **Project facts.** `facts.md` beside the tree (`WriteFacts`, `ReadFacts`), cut to 1,200 bytes, overridden by `Options2.Facts`. The pass-2 prompt tells the model to take `test_command` from the facts and to use an empty array rather than guess.
7. **`Pass2Prompt` takes a `Pass2Input` struct** (it was seven positional arguments and is about to gain more).
8. **Budget.** Worst-case pass-2 prompt with the defaults (brief excerpts 4,000, step list 3,000, facts 1,200, decisions 6 x 300): 26,250 bytes (ASCII) and 26,337 (4-byte runes), pinned below 27,000 by tests.
9. **Out of scope:** the question-round UI and re-planning (M5), export and wiring into `/project` (M6), a run lock (M6), deriving prompt sizes from the model's window (M6).

---

## File structure

| File | Responsibility |
|---|---|
| `plantree/plan/facts.go` | `ReadFacts`, `WriteFacts`, `FactsCapBytes` |
| `plantree/plan/questions.go` | question types and the store: `LoadQuestions`, `OpenQuestions`, `AddQuestions`, `AnswerQuestion` |
| `plantree/plan/questions_parse.go` | `QuestionOut`, `validateQuestionOuts`, `newQuestion` (shared by both passes) |
| `plantree/plan/questions_apply.go` | `holdSteps`, `applyPass1Questions`, `recordPass2Questions`, `markAsked`, `ReleaseAnswered` |
| `plantree/plan/decisions.go` | `decisionsFor`: answered questions rendered for a prompt |
| `plantree/plan/pass1json.go`, `prompt.go`, `runner.go` (modify) | pass 1 replies and prompt gain questions; `runChunk` records them |
| `plantree/plan/pass2json.go`, `prompt2.go`, `runner2.go` (modify) | pass 2 replies and prompt gain questions, decisions and facts; `RunPass2` records, holds and releases |

---
### Task 1: Project facts and the prompt input struct

**Files:**
- Create: `gophermind-lib/plantree/plan/facts.go`, `facts_test.go`
- Replace: `prompt2.go`, `prompt2_test.go`, `runner2.go`, `runner2_test.go` (same directory)

**Interfaces:**
- Produces:
  - `const FactsCapBytes = 1200`, `func ReadFacts(repo *plantree.Repo) (string, error)`, `func WriteFacts(repo *plantree.Repo, text string) error`
  - `type Pass2Input struct{ Project, Overview, Facts, Excerpts string; Phase, Task plantree.Node; Siblings, Batch []plantree.Node }` and `func Pass2Prompt(in Pass2Input) string` (replaces the seven positional arguments; the prompt gains a "Repository facts" block and tells the model not to guess a test command)
  - `Options2.Facts string` (overrides the stored facts)
  - `defaultBriefBytes` is now 5000

The existing `prompt2_test.go` and `runner2_test.go` are replaced with versions that call the new `Pass2Prompt(Pass2Input{...})` and add the facts tests.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/facts_test.go`:

```go
package plan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFactsRoundTripAndTrim(t *testing.T) {
	r := newRepo(t)
	if got, err := ReadFacts(r); err != nil || got != "" {
		t.Fatalf("no facts: %q, %v", got, err)
	}
	if err := WriteFacts(r, "  Go 1.25\n  test: go test ./...\n\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFacts(r)
	if err != nil || got != "Go 1.25\n  test: go test ./..." {
		t.Errorf("ReadFacts = %q, %v", got, err)
	}
}

func TestReadFactsIsBounded(t *testing.T) {
	r := newRepo(t)
	if err := WriteFacts(r, strings.Repeat("x", 50_000)); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadFacts(r)
	if len(got) > FactsCapBytes+3 || !strings.HasSuffix(got, "...") {
		t.Errorf("len=%d, want at most %d plus the marker", len(got), FactsCapBytes)
	}
	if err := WriteFacts(r, strings.Repeat("\U0001D11E", 5000)); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadFacts(r); !utf8.ValidString(got) || len(got) > FactsCapBytes+3 {
		t.Errorf("multibyte facts: valid=%v len=%d", utf8.ValidString(got), len(got))
	}
}
```

Replace the whole of `plantree/plan/prompt2_test.go`:

```go
package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

func node(t *testing.T, id, title, digest, objective string) plantree.Node {
	t.Helper()
	n, err := newSkeleton(id, title, digest, objective)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPass2PromptCarriesOneTaskAndItsContext(t *testing.T) {
	phase := node(t, "phase-001", "Foundation", "the base of everything", "set up the base")
	task := node(t, "phase-001.task-001", "Repo layout", "where code lives", "")
	st1 := node(t, s1, "Create module", "needed to compile", "")
	st2 := node(t, s2, "Add CI", "catch breakage", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "an overview", Excerpts: "[part 1 of 1]\nthe brief text", Phase: phase, Task: task, Siblings: []plantree.Node{st1, st2}, Batch: []plantree.Node{st2}})
	for _, want := range []string{
		`"demo"`, "an overview", "Phase: Foundation", "the base of everything", "Task: Repo layout", "where code lives",
		"- " + s1 + ": Create module", "Steps to specify now:", "- " + s2 + ": Add CI. Why: catch breakage",
		"<<<BRIEF EXCERPTS", "the brief text", "EARLIER step", `"depends_on"`, "ONE JSON object",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	section := p[strings.Index(p, "Steps to specify now:"):strings.Index(p, "Brief excerpts")]
	if strings.Contains(section, s1) {
		t.Error("the steps to specify now must list only the batch")
	}
}

func TestPass2PromptWithoutExcerptsSaysSo(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}})
	if !strings.Contains(p, "(not available)") || strings.Contains(p, "<<<BRIEF EXCERPTS") {
		t.Errorf("no excerpts: %q", p[strings.Index(p, "Brief excerpts"):])
	}
}

func TestPass2PromptBoundsTheStepList(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("a long step title ", 10), "d", ""))
	}
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: steps, Batch: steps[:1]})
	if !strings.Contains(p, "more steps not shown") {
		t.Error("an oversize step list must say how many steps it left out")
	}
	list := p[strings.Index(p, "in order."):strings.Index(p, "Steps to specify now:")]
	if len(list) > siblingListCapBytes+200 {
		t.Errorf("the step list is %d bytes, cap %d", len(list), siblingListCapBytes)
	}
}

// TestPass2PromptWorstCaseSize pins the largest prompt one pass can produce
// with the default caps, so a change that lets it grow shows up here.
func TestPass2PromptWorstCaseSize(t *testing.T) {
	phase := node(t, "phase-001", strings.Repeat("p", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	task := node(t, "phase-001.task-001", strings.Repeat("t", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("s", 200), strings.Repeat("d", 500), ""))
	}
	chunks := []Chunk{{Index: 0, Text: strings.Repeat("brief text line\n", 2000)}}
	excerpts := Excerpts(chunks, []int{0}, defaultBriefBytes)
	overview := FitOverview(strings.Repeat("o", 20000), OverviewCapBytes)
	p := Pass2Prompt(Pass2Input{Project: strings.Repeat("n", 100), Overview: overview, Facts: strings.Repeat("f", FactsCapBytes), Excerpts: excerpts, Phase: phase, Task: task, Siblings: steps, Batch: steps[:defaultStepsPerPass]})
	t.Logf("worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 26000 {
		t.Errorf("worst-case pass-2 prompt is %d bytes, want at most 26000", len(p))
	}
}

func TestPass2PromptWorstCaseSizeWithMultibyteText(t *testing.T) {
	r := func(n int) string { return strings.Repeat("\U0001D11E", n) }
	phase := node(t, "phase-001", r(200), r(500), r(1000))
	task := node(t, "phase-001.task-001", r(200), r(500), r(1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), r(200), r(500), ""))
	}
	chunks := []Chunk{{Index: 0, Text: strings.Repeat("brief text line\n", 2000)}}
	excerpts := Excerpts(chunks, []int{0}, defaultBriefBytes)
	overview := FitOverview(strings.Repeat("o", 20000), OverviewCapBytes)
	p := Pass2Prompt(Pass2Input{Project: r(100), Overview: overview, Facts: r(FactsCapBytes / 4), Excerpts: excerpts, Phase: phase, Task: task, Siblings: steps, Batch: steps[:defaultStepsPerPass]})
	t.Logf("multibyte worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 26000 {
		t.Errorf("multibyte worst-case pass-2 prompt is %d bytes, want at most 26000", len(p))
	}
	if !utf8.ValidString(p) {
		t.Error("the prompt is not valid UTF-8")
	}
}

func TestPass2PromptTellsTheModelWhatTheParserEnforces(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}})
	for _, want := range []string{"2000 characters", "300 characters", "never put an empty string", "Do not depend on a step marked on hold"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if strings.Contains(p, `[""]`) {
		t.Error("the shape example must not contain an empty string")
	}
	found := false
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, `{"steps"`) {
			found = true
			if !json.Valid([]byte(line)) {
				t.Errorf("the shape line is not valid JSON: %s", line)
			}
		}
	}
	if !found {
		t.Error("no shape line found")
	}
}

func TestPass2PromptTagsEachSiblingByStageAndHold(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	skipped := node(t, "phase-001.task-001.step-001", "One", "d", "")
	skipped.Status = plantree.StatusSkipped
	drafted := node(t, "phase-001.task-001.step-002", "Two", "d", "")
	drafted.Planning.Stage = plantree.StageDrafted
	skel := node(t, "phase-001.task-001.step-003", "Three", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{skipped, drafted, skel}, Batch: []plantree.Node{skel}})
	for _, want := range []string{
		"step-001: One [on hold: skipped]", "step-002: Two [specified]", "step-003: Three [to specify]",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestPass2PromptShowsFactsOrSaysNone(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	base := Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}}

	without := Pass2Prompt(base)
	if !strings.Contains(without, "Repository facts") || !strings.Contains(without, "(not provided)") {
		t.Error("with no facts the prompt must say so")
	}
	if !strings.Contains(without, "rather than guessing") {
		t.Error("the prompt must tell the model not to guess a test command")
	}

	base.Facts = "Go 1.25. Test: go test ./..."
	with := Pass2Prompt(base)
	if !strings.Contains(with, "Go 1.25. Test: go test ./...") || strings.Contains(with, "(not provided)") {
		t.Error("the facts must appear and replace the placeholder")
	}

	base.Facts = strings.Repeat("f", 10*FactsCapBytes)
	if got := Pass2Prompt(base); len(got) > len(with)+FactsCapBytes+200 {
		t.Errorf("oversize facts must be cut to FactsCapBytes, prompt grew to %d", len(got))
	}
}
```

Replace the whole of `plantree/plan/runner2_test.go`:

```go
package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

var stepIDRE = regexp.MustCompile(`phase-\d{3}\.task-\d{3}\.step-\d{3}`)

// stepsToSpecify returns the step ids listed under "Steps to specify now".
func stepsToSpecify(prompt string) []string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	if start < 0 || end < start {
		return nil
	}
	return stepIDRE.FindAllString(prompt[start:end], -1)
}

// pass2Reply answers a pass-2 prompt with a valid specification for every step
// it asks for. Step 2 of a task depends on step 1.
func pass2Reply(prompt string) string {
	var steps []StepSpecOut
	for _, id := range stepsToSpecify(prompt) {
		s := StepSpecOut{
			ID:                 id,
			Description:        "implement " + id,
			TargetPaths:        []string{"pkg/" + id[len(id)-8:] + ".go"},
			AcceptanceCriteria: []string{"it works"},
			TestCommand:        []string{"go", "test", "./..."},
			DependsOn:          []string{},
		}
		if strings.HasSuffix(id, "step-002") {
			s.DependsOn = []string{strings.TrimSuffix(id, "step-002") + "step-001"}
		}
		steps = append(steps, s)
	}
	b, _ := json.Marshal(Pass2Output{Steps: steps})
	return string(b)
}

func specFake() *fake {
	return &fake{reply: func(_ int, p string) (string, error) { return pass2Reply(p), nil }}
}

// planned runs pass 1 over threePartBrief and returns the repo and the planning
// directory it lives in, so a test can reopen it as a new process would.
func planned(t *testing.T) (*plantree.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func actionKinds(t *testing.T, r *plantree.Repo) string {
	t.Helper()
	a, err := r.NextActions()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range append(a.Runnable, a.Blocked...) {
		out = append(out, string(x.Kind)+":"+x.NodeID)
	}
	return fmt.Sprint(out)
}

func TestRunPass2SpecifiesEveryStepAndLeavesOnlyApproval(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result2{Tasks: 1, Steps: 2, Passes: 1}) {
		t.Errorf("Result2 = %+v", res)
	}
	a, _ := r.Get(s1)
	b, _ := r.Get(s2)
	if a.Planning.Stage != plantree.StageDrafted || a.Status != plantree.StatusUntouched || a.Work == nil ||
		a.Work.Description != "implement "+s1 || len(a.Work.TestCommand) != 3 || a.Work.TargetPaths[0] != "pkg/step-001.go" {
		t.Errorf("step 1 = %+v work=%+v", a, a.Work)
	}
	if len(b.DependsOn) != 1 || b.DependsOn[0] != s1 {
		t.Errorf("step 2 depends_on = %v", b.DependsOn)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if !strings.Contains(f.prompts[0], `"demo"`) {
		t.Error("the project name defaults to the root title")
	}
}

func TestRunPass2BatchesStepsOfOneTask(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passes != 2 || res.Steps != 2 || res.Tasks != 1 || len(f.prompts) != 2 {
		t.Errorf("Result2 = %+v, %d calls", res, len(f.prompts))
	}
	if got := stepsToSpecify(f.prompts[1]); len(got) != 1 || got[0] != s2 {
		t.Errorf("the second batch must be only step 2, got %v", got)
	}
}

func TestRunPass2ResumesFromTheTreeAfterAFailure(t *testing.T) {
	r, dir := planned(t)
	broken := &fake{reply: func(_ int, p string) (string, error) {
		if strings.Contains(p, "Task: T2") {
			return "", errors.New("model unavailable")
		}
		return pass2Reply(p), nil
	}}
	res, err := RunPass2(context.Background(), r, broken, Options2{})
	if err == nil || !strings.Contains(err.Error(), "task phase-001.task-002") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want it to name the task and the cause", err)
	}
	if res.Tasks != 1 || res.Steps != 1 {
		t.Errorf("Result2 = %+v", res)
	}
	first, _ := r.Get("phase-001.task-001.step-001")
	second, _ := r.Get("phase-001.task-002.step-001")
	if first.Planning.Stage != plantree.StageDrafted || second.Planning.Stage != plantree.StageSkeleton {
		t.Errorf("stages after failure: %s and %s", first.Planning.Stage, second.Planning.Stage)
	}

	good := specFake() // a new process, a new client
	res, err = RunPass2(context.Background(), plantree.Open(dir), good, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if len(good.prompts) != 2 || res.Tasks != 2 || res.Steps != 2 {
		t.Errorf("resume made %d calls, Result2 = %+v; want 2 calls for the 2 tasks left", len(good.prompts), res)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s", got)
	}
}

func TestRunPass2ShowsEachTaskOnlyTheBriefThatProducedIt(t *testing.T) {
	r, _ := planned(t)
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 3 {
		t.Fatalf("%d calls, want 3", len(f.prompts))
	}
	// Task order: phase-001.task-001 (chunk 1), phase-001.task-002 (chunk 2), phase-002.task-001 (chunk 3).
	want := []struct{ has, not string }{
		{"first part text", "third part text"},
		{"second part text", "third part text"},
		{"third part text", "first part text"},
	}
	for i, w := range want {
		if !strings.Contains(f.prompts[i], w.has) || strings.Contains(f.prompts[i], w.not) {
			t.Errorf("prompt %d must contain %q and not %q", i, w.has, w.not)
		}
	}
	if !strings.Contains(f.prompts[0], "[part 1 of 3]") {
		t.Error("excerpts are labeled with their place in the brief")
	}
}

func TestRunPass2WithoutAStoredBriefStillWorks(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "(not available)") {
		t.Error("a plan with no stored brief must say the excerpts are not available")
	}
}

func TestRunPass2RetriesAMalformedReplyOnce(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := &fake{reply: func(n int, p string) (string, error) {
		if n == 0 {
			return "Sorry, here is prose only.", nil
		}
		return pass2Reply(p), nil
	}}
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil || len(f.prompts) != 2 || res.Steps != 2 {
		t.Fatalf("err=%v calls=%d res=%+v", err, len(f.prompts), res)
	}
	if !strings.Contains(f.prompts[1], "Sorry, here is prose only.") {
		t.Error("the retry must quote the rejected reply")
	}
}

func TestRunPass2RejectedTwiceLeavesTheStepsAlone(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	onlyOne := func(_ int, p string) (string, error) {
		ids := stepsToSpecify(p)
		s := StepSpecOut{ID: ids[0], Description: "d", AcceptanceCriteria: []string{"c"}, DependsOn: []string{}}
		b, _ := json.Marshal(Pass2Output{Steps: []StepSpecOut{s}}) // the second step is missing
		return string(b), nil
	}
	res, err := RunPass2(context.Background(), r, &fake{reply: onlyOne}, Options2{})
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "missing from the reply") {
		t.Fatalf("err = %v", err)
	}
	if res.Steps != 0 {
		t.Errorf("Result2 = %+v", res)
	}
	a, _ := r.Get(s1)
	if a.Planning.Stage != plantree.StageSkeleton || a.Work != nil {
		t.Errorf("a rejected reply must write nothing: %+v", a)
	}
}

func TestRunPass2SkipsStepsThatAreDraftedOrOnHold(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s2)
	if _, err := r.Update(s2, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if got := stepsToSpecify(f.prompts[0]); len(got) != 1 || got[0] != s1 {
		t.Errorf("only step 1 should be asked for, got %v", got)
	}
	skipped, _ := r.Get(s2)
	if skipped.Work != nil {
		t.Error("a skipped step must not be specified")
	}

	again := specFake()
	res, err := RunPass2(context.Background(), r, again, Options2{})
	if err != nil || len(again.prompts) != 0 || res != (Result2{}) {
		t.Errorf("nothing left to specify: err=%v calls=%d res=%+v", err, len(again.prompts), res)
	}
}

func TestRunPass2AddsAHintForAContextWindowError(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("server said no")
	f := &fake{reply: func(int, string) (string, error) {
		return "", fmt.Errorf("%w: status 400: {\"error\":{\"type\":\"exceed_context_size_error\",\"n_ctx\":4096}}", sentinel)
	}}
	_, err := RunPass2(context.Background(), r, f, Options2{})
	if err == nil || !strings.Contains(err.Error(), "lower Options2.BriefBytes") || !errors.Is(err, sentinel) {
		t.Errorf("err = %v", err)
	}
}

func TestRunPass2NeedsAPlanAndHonorsCancellation(t *testing.T) {
	empty := plantree.Open(t.TempDir())
	if _, err := RunPass2(context.Background(), empty, specFake(), Options2{}); !errors.Is(err, plantree.ErrNotFound) {
		t.Errorf("no plan: err = %v, want ErrNotFound", err)
	}
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := specFake()
	if _, err := RunPass2(ctx, r, f, Options2{}); !errors.Is(err, context.Canceled) || len(f.prompts) != 0 {
		t.Errorf("cancelled: err=%v calls=%d", err, len(f.prompts))
	}
}

func TestRunPass2RefusesADependencyOnAHeldStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s1)
	if _, err := r.Update(s1, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dependsOnHeld := func(_ int, p string) (string, error) {
		s := StepSpecOut{ID: stepsToSpecify(p)[0], Description: "d", AcceptanceCriteria: []string{"c"}, DependsOn: []string{s1}}
		b, _ := json.Marshal(Pass2Output{Steps: []StepSpecOut{s}})
		return string(b), nil
	}
	_, err := RunPass2(context.Background(), r, &fake{reply: dependsOnHeld}, Options2{})
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "not a step of this task") {
		t.Fatalf("err = %v", err)
	}
	b, _ := r.Get(s2)
	if b.Planning.Stage != plantree.StageSkeleton || b.Work != nil {
		t.Errorf("step 2 must stay a skeleton: %+v", b)
	}
}

func TestRunPass2SecondBatchSeesTheFirstBatchAsSpecified(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 2 {
		t.Fatalf("%d prompts", len(f.prompts))
	}
	second := f.prompts[1]
	if !regexp.MustCompile(`step-001: .* \[specified\]`).MatchString(second) || !regexp.MustCompile(`step-002: .* \[to specify\]`).MatchString(second) {
		t.Errorf("second prompt step list:\n%s", second)
	}
	if strings.Contains(f.prompts[0], "[specified]") {
		t.Error("the first prompt must not show any step as specified")
	}
}

func TestRunPass2ReportsTasksWithoutSteps(t *testing.T) {
	r := newRepo(t)
	out := sampleOut()
	out.Phases[0].Tasks = append(out.Phases[0].Tasks, TaskOut{Title: "Docs", Digest: "explain it"})
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), r, specFake(), Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EmptyTasks != 1 {
		t.Errorf("EmptyTasks = %d, want 1", res.EmptyTasks)
	}
	got, err := EmptyTasks(r)
	if err != nil || len(got) != 1 || got[0] != "phase-001.task-002" {
		t.Errorf("EmptyTasks(repo) = %v, %v", got, err)
	}
	kinds := actionKinds(t, r)
	if !strings.Contains(kinds, "decompose:phase-001.task-002") || strings.Contains(kinds, "approve") {
		t.Errorf("NextActions = %s", kinds)
	}
}

func TestRunPass2ShowsTheStoredFactsAndAnOptionOverridesThem(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if err := WriteFacts(r, "STORED FACTS: go test ./..."); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "STORED FACTS: go test ./...") {
		t.Error("RunPass2 must show the stored facts")
	}

	r2 := newRepo(t)
	if _, err := Merge(r2, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if err := WriteFacts(r2, "STORED FACTS"); err != nil {
		t.Fatal(err)
	}
	f2 := specFake()
	if _, err := RunPass2(context.Background(), r2, f2, Options2{Facts: "OVERRIDE FACTS"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f2.prompts[0], "OVERRIDE FACTS") || strings.Contains(f2.prompts[0], "STORED FACTS") {
		t.Error("Options2.Facts must override the stored facts")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: ReadFacts`, `unknown field Facts` and `too many arguments in call to Pass2Prompt`.

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/facts.go`:

```go
package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// FactsCapBytes bounds the project facts shown in a pass-2 prompt.
const FactsCapBytes = 1200

const factsFile = "facts.md"

// ReadFacts returns the project facts stored beside the tree (the language,
// how to build and test, where things live), or "" if there are none. The text
// is cut to FactsCapBytes at a rune boundary, so a large file cannot grow a
// prompt.
func ReadFacts(repo *plantree.Repo) (string, error) {
	b, err := os.ReadFile(filepath.Join(repo.Dir(), factsFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cutBytes(strings.TrimSpace(string(b)), FactsCapBytes), nil
}

// WriteFacts atomically replaces the project facts stored beside the tree.
func WriteFacts(repo *plantree.Repo, text string) error {
	if err := os.MkdirAll(repo.Dir(), 0o755); err != nil {
		return err
	}
	body := strings.TrimSpace(text) + "\n"
	return lockfile.WriteAtomic(filepath.Join(repo.Dir(), factsFile), []byte(body), 0o644)
}
```

Replace the whole of `plantree/plan/prompt2.go`:

```go
package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// Pass-2 defaults, sized so the worst-case prompt stays near 26,000 bytes.
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 5000
)

// siblingListCapBytes bounds the list of every step of the task in a prompt.
const siblingListCapBytes = 4000

// fit makes a node field safe for a prompt: one line, at most n bytes.
func fit(s string, n int) string { return cutBytes(oneLine(s), n) }

// stepTag tells the model whether a sibling can be depended on.
func stepTag(s plantree.Node) string {
	switch {
	case onHold(s):
		return "on hold: " + string(s.Status)
	case s.Planning.Stage == plantree.StageDrafted || s.Planning.Stage == plantree.StageApproved:
		return "specified"
	}
	return "to specify"
}

func stepList(steps []plantree.Node) string {
	var b strings.Builder
	omitted := 0
	for _, s := range steps {
		line := "- " + s.ID + ": " + fit(s.Title, 200) + " [" + stepTag(s) + "]\n"
		if b.Len()+len(line) > siblingListCapBytes {
			omitted++
			continue
		}
		b.WriteString(line)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  (%d more steps not shown)\n", omitted)
	}
	return b.String()
}

// Pass2Input is everything one specification pass shows the model. Facts and
// Excerpts may be empty. Nothing about any other task belongs here.
type Pass2Input struct {
	Project  string
	Overview string
	Facts    string // project facts: language, build and test commands, layout
	Excerpts string // brief excerpts that produced the task
	Phase    plantree.Node
	Task     plantree.Node
	Siblings []plantree.Node // every step of the task
	Batch    []plantree.Node // the steps to specify now
}

// Pass2Prompt builds the prompt for one specification pass. It carries the
// overview, the project facts, the phase and task the steps belong to, the
// list of the task's steps, the steps to specify now, and optionally excerpts
// of the brief. It carries nothing about any other task.
func Pass2Prompt(in Pass2Input) string {
	phase, task, siblings, batch := in.Phase, in.Task, in.Siblings, in.Batch
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing the work specification for some steps of ONE task in a project plan for %q. You see only this task.\n\n", fit(in.Project, 100))
	b.WriteString("Running overview of the whole project:\n")
	b.WriteString(orNone(in.Overview))
	b.WriteString("\n\nRepository facts (language, build and test commands, layout):\n")
	if strings.TrimSpace(in.Facts) == "" {
		b.WriteString("(not provided)\n")
	} else {
		b.WriteString(cutBytes(strings.TrimSpace(in.Facts), FactsCapBytes) + "\n")
	}
	fmt.Fprintf(&b, "\nPhase: %s\nWhy: %s\nObjective: %s\n", fit(phase.Title, 200), fit(phase.ContextDigest, 500), orNone(fit(phase.Objective, 1000)))
	fmt.Fprintf(&b, "\nTask: %s\nWhy: %s\nObjective: %s\n", fit(task.Title, 200), fit(task.ContextDigest, 500), orNone(fit(task.Objective, 1000)))
	b.WriteString("\nAll steps of this task, in order. A step may depend only on an EARLIER step in this list:\n")
	b.WriteString(stepList(siblings))
	b.WriteString("\nSteps to specify now:\n")
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, fit(s.Title, 200), fit(s.ContextDigest, 500))
	}
	b.WriteString("\nBrief excerpts that produced this task (context only, may be partial):\n")
	if strings.TrimSpace(in.Excerpts) == "" {
		b.WriteString("(not available)\n")
	} else {
		fmt.Fprintf(&b, "<<<BRIEF EXCERPTS\n%s\nBRIEF EXCERPTS>>>\n", in.Excerpts)
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Return exactly the steps listed under \"Steps to specify now\", each once, using its id.\n")
	b.WriteString("- description: what to build or change, concrete enough that an agent can start without asking.\n")
	b.WriteString("- target_paths: repository-relative files or directories the step touches. Never absolute, never containing \"..\".\n")
	b.WriteString("- acceptance_criteria: 1 to 10 checks a reviewer can verify.\n")
	b.WriteString("- test_command: the command as an array of arguments that verifies the step, taken from the repository facts; if the facts do not say, use an empty array rather than guessing.\n")
	b.WriteString("- depends_on: ids of EARLIER steps of this task that must be done first, or an empty array.\n")
	b.WriteString("- description: at most 2000 characters. Each acceptance criterion: at most 300 characters, and at most 10 criteria.\n")
	b.WriteString("- target_paths: at most 20 paths of at most 300 characters each. test_command: at most 20 arguments of at most 200 characters each.\n")
	b.WriteString("- In every array, never put an empty string.\n")
	b.WriteString("- Do not depend on a step marked on hold.\n")
	b.WriteString("- Do not invent scope the task does not need. Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"steps":[{"id":"<step id>","description":"<what to build>","target_paths":["<path/to/file>"],"acceptance_criteria":["<a check a reviewer can verify>"],"test_command":["<command>","<arg>"],"depends_on":[]}]}`)
	b.WriteString("\n")
	return b.String()
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
	// BriefBytes bounds the brief excerpts shown with a task (default 5000).
	// With the defaults one pass is at most about 26,000 bytes: BriefBytes +
	// OverviewCapBytes + FactsCapBytes + 4000 (step list) + about 8,000 (phase,
	// task and the steps being specified) + 3,000 (instructions). At 3 to 4
	// bytes per token that must leave room for the reply inside the model's
	// window.
	BriefBytes int
	// Facts is text describing the repository (language, how to build and test,
	// layout) shown to every pass, cut to FactsCapBytes. If empty, RunPass2
	// reads the facts stored with WriteFacts, if any.
	Facts string
}

// Result2 summarizes one RunPass2 call.
type Result2 struct {
	Tasks  int // tasks whose pending steps were all specified by this call
	Steps  int // steps specified by this call
	Passes int // model passes made by this call
	// EmptyTasks counts tasks that have no steps at all; pass 2 cannot specify
	// them, and NextActions keeps offering decompose for them.
	EmptyTasks int
}

// taskWork is one task with the steps still waiting for a specification.
type taskWork struct {
	phase   plantree.Node
	task    plantree.Node
	steps   []plantree.Node // every step of the task
	pending []plantree.Node // steps that need a specification
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

// pendingWork lists, in tree order, every task that has a step needing a
// specification. It is derived from the tree alone, so a resumed run finds
// exactly what is left.
func pendingWork(repo *plantree.Repo) ([]taskWork, error) {
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
			var pending []plantree.Node
			for _, s := range steps {
				if needsSpec(s) {
					pending = append(pending, s)
				}
			}
			if len(pending) > 0 {
				out = append(out, taskWork{phase: phase, task: n, steps: steps, pending: pending})
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
	facts := opt.Facts
	if strings.TrimSpace(facts) == "" {
		if facts, err = ReadFacts(repo); err != nil {
			return Result2{}, err
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
	work, err := pendingWork(repo)
	if err != nil {
		return Result2{}, err
	}

	empty, err := EmptyTasks(repo)
	if err != nil {
		return Result2{}, err
	}
	res := Result2{EmptyTasks: len(empty)}
	for _, w := range work {
		ids := append([]string{w.task.ID}, idsOf(w.steps)...)
		excerpts := Excerpts(chunks, prov.chunksFor(ids), opt.BriefBytes)
		for start := 0; start < len(w.pending); start += opt.StepsPerPass {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			end := start + opt.StepsPerPass
			if end > len(w.pending) {
				end = len(w.pending)
			}
			batch := w.pending[start:end]
			batchIDs := idsOf(batch)
			var live []plantree.Node
			for _, s := range w.steps {
				if !onHold(s) {
					live = append(live, s)
				}
			}
			siblingIDs := idsOf(live)
			prompt := Pass2Prompt(Pass2Input{
				Project: opt.ProjectName, Overview: overview, Facts: facts, Excerpts: excerpts,
				Phase: w.phase, Task: w.task, Siblings: w.steps, Batch: batch,
			})
			out, err := askJSON(ctx, c, prompt, func(reply string) (Pass2Output, error) {
				return ParsePass2(reply, batchIDs, siblingIDs)
			})
			res.Passes++
			if err != nil {
				return res, taskError(w.task.ID, err)
			}
			if err := applySpecs(repo, out); err != nil {
				return res, taskError(w.task.ID, err)
			}
			markSpecified(w.steps, out)
			res.Steps += len(batch)
		}
		res.Tasks++
	}
	return res, nil
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
// half-specified reaches the tree.
func applySpecs(repo *plantree.Repo, out Pass2Output) error {
	for _, s := range out.Steps {
		cur, err := repo.Get(s.ID)
		if err != nil {
			return err
		}
		spec := s
		if _, err := repo.Update(s.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Work = &plantree.Work{
				Description:        spec.Description,
				TargetPaths:        spec.TargetPaths,
				AcceptanceCriteria: spec.AcceptanceCriteria,
				TestCommand:        spec.TestCommand,
			}
			n.DependsOn = spec.DependsOn
			n.Planning.Stage = plantree.StageDrafted
			return nil
		}); err != nil {
			return fmt.Errorf("writing the specification of %s: %w", s.ID, err)
		}
	}
	return nil
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

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/facts.go gophermind-lib/plantree/plan/facts_test.go gophermind-lib/plantree/plan/prompt2.go gophermind-lib/plantree/plan/prompt2_test.go gophermind-lib/plantree/plan/runner2.go gophermind-lib/plantree/plan/runner2_test.go
git commit -m "feat(plan): project facts in the specification prompt, and a Pass2Input struct

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 2: The question store

**Files:**
- Create: `gophermind-lib/plantree/plan/questions.go`, `questions_test.go`

**Interfaces:**
- Consumes: `NormalizeTitle`, `cutBytes` (existing), `lockfile.Acquire`, `lockfile.WriteAtomic`, the test helper `newRepo` (`merge_test.go`).
- Produces:
  - `type Option`, `Recommendation`, `Answer`, `Question`, `NewOption`, `NewQuestion` (with `Source`)
  - `const QuestionOpen`, `QuestionAnswered`; `var ErrNoSuchQuestion`, `ErrAlreadyAnswered`, `ErrInvalidAnswer`
  - `func LoadQuestions(repo) ([]Question, error)`, `func OpenQuestions(repo) ([]Question, error)`
  - `func AddQuestions(repo, in []NewQuestion) ([]Question, error)` (idempotent by question text; assigns `q-NNN` and `opt-N` ids)
  - `func AnswerQuestion(repo, id string, a Answer) (Question, error)` (rules: at least one option or some text; only offered options, each once; one option unless multi-select; text at most 2,000 characters; only an open question)
  - `test helper twoOptions()` in `questions_test.go`, reused by later tests

The store keeps one strict JSON file with a cross-process lock, so concurrent adds never lose a question or reuse an id.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/questions_test.go`:

```go
package plan

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func twoOptions() NewQuestion {
	return NewQuestion{
		Question: "Which database?",
		Why:      "the schema depends on it",
		Options: []NewOption{
			{Label: "SQLite", Description: "embedded"},
			{Label: "Postgres", Description: "server"},
		},
		RecommendedLabels: []string{"sqlite"},
		Rationale:         "simplest to run",
		Affects:           []string{"phase-001.task-001"},
		Source:            "chunk 2 of the brief",
	}
}

func TestAddQuestionsAssignsIdsAndKeepsRecommendationsApart(t *testing.T) {
	r := newRepo(t)
	got, err := AddQuestions(r, []NewQuestion{twoOptions()})
	if err != nil || len(got) != 1 {
		t.Fatalf("AddQuestions = %+v, %v", got, err)
	}
	q := got[0]
	if q.ID != "q-001" || q.Status != QuestionOpen || !q.AllowFreeText || q.Answer != nil ||
		len(q.Options) != 2 || q.Options[0].ID != "opt-1" || q.Options[1].ID != "opt-2" {
		t.Errorf("question = %+v", q)
	}
	if q.Recommended == nil || len(q.Recommended.OptionIDs) != 1 || q.Recommended.OptionIDs[0] != "opt-1" || q.Recommended.Rationale != "simplest to run" {
		t.Errorf("recommended = %+v", q.Recommended)
	}
	if q.Answer != nil {
		t.Error("a recommendation is never an answer")
	}
	if q.Source != "chunk 2 of the brief" || len(q.Affects) != 1 || q.Affects[0] != "phase-001.task-001" {
		t.Errorf("source=%q affects=%v", q.Source, q.Affects)
	}
	open, _ := OpenQuestions(r)
	if len(open) != 1 {
		t.Errorf("OpenQuestions = %d", len(open))
	}
}

func TestAddQuestionsIsIdempotentByQuestionText(t *testing.T) {
	r := newRepo(t)
	first, _ := AddQuestions(r, []NewQuestion{twoOptions()})
	dup := twoOptions()
	dup.Question = "  which DATABASE?  "
	second, err := AddQuestions(r, []NewQuestion{dup})
	if err != nil || len(second) != 1 || second[0].ID != first[0].ID {
		t.Fatalf("a repeated question must return the existing record: %+v, %v", second, err)
	}
	all, _ := LoadQuestions(r)
	if len(all) != 1 {
		t.Errorf("%d questions stored, want 1", len(all))
	}
	// Even after it is answered, asking it again does not reopen it.
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	again, _ := AddQuestions(r, []NewQuestion{twoOptions()})
	if again[0].Status != QuestionAnswered {
		t.Error("an answered question must stay answered")
	}
}

func TestAddQuestionsRejectsAnUnknownRecommendation(t *testing.T) {
	r := newRepo(t)
	bad := twoOptions()
	bad.RecommendedLabels = []string{"Oracle"}
	if _, err := AddQuestions(r, []NewQuestion{bad}); err == nil || !strings.Contains(err.Error(), "not one of its options") {
		t.Errorf("err = %v", err)
	}
	if all, _ := LoadQuestions(r); len(all) != 0 {
		t.Error("a rejected question must not be stored")
	}
}

func TestAnswerQuestionRules(t *testing.T) {
	r := newRepo(t)
	multi := twoOptions()
	multi.Question = "Which platforms?"
	multi.MultiSelect = true
	multi.RecommendedLabels = nil
	if _, err := AddQuestions(r, []NewQuestion{twoOptions(), multi}); err != nil {
		t.Fatal(err)
	}

	bad := map[string]struct {
		id string
		a  Answer
	}{
		"nothing":              {"q-001", Answer{}},
		"blank text":           {"q-001", Answer{Text: "  "}},
		"unknown option":       {"q-001", Answer{OptionIDs: []string{"opt-9"}}},
		"two on single select": {"q-001", Answer{OptionIDs: []string{"opt-1", "opt-2"}}},
		"duplicate on multi":   {"q-002", Answer{OptionIDs: []string{"opt-1", "opt-1"}}},
		"text too long":        {"q-001", Answer{Text: strings.Repeat("x", maxAnswerTextRunes+1)}},
	}
	for name, c := range bad {
		if _, err := AnswerQuestion(r, c.id, c.a); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%s: err = %v, want ErrInvalidAnswer", name, err)
		}
	}
	if open, _ := OpenQuestions(r); len(open) != 2 {
		t.Error("a rejected answer must leave the question open")
	}

	q, err := AnswerQuestion(r, "q-002", Answer{OptionIDs: []string{"opt-1", "opt-2"}, Text: " both, plus a note "})
	if err != nil || q.Status != QuestionAnswered || q.Answer.Text != "both, plus a note" || q.AnsweredAt == "" || len(q.Answer.OptionIDs) != 2 {
		t.Errorf("multi-select answer = %+v, %v", q, err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{Text: "whatever fits"}); err != nil {
		t.Errorf("free text alone must be accepted: %v", err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{Text: "again"}); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("second answer: err = %v, want ErrAlreadyAnswered", err)
	}
	if _, err := AnswerQuestion(r, "q-099", Answer{Text: "x"}); !errors.Is(err, ErrNoSuchQuestion) {
		t.Errorf("unknown id: err = %v, want ErrNoSuchQuestion", err)
	}
}

func TestAnswerSurvivesReopeningTheStore(t *testing.T) {
	r := newRepo(t)
	if _, err := AddQuestions(r, []NewQuestion{twoOptions()}); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	all, err := LoadQuestions(r)
	if err != nil || len(all) != 1 || all[0].Answer == nil || all[0].Answer.OptionIDs[0] != "opt-2" {
		t.Errorf("reloaded = %+v, %v", all, err)
	}
	open, _ := OpenQuestions(r)
	if len(open) != 0 {
		t.Error("an answered question is not open")
	}
}

func TestQuestionFileIsStrict(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(questionsPath(r), []byte(`{"schema_version":1,"revision":1,"questions":[],"extra":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuestions(r); err == nil {
		t.Error("an unknown field must be rejected")
	}
	if err := os.WriteFile(questionsPath(r), []byte(`{"schema_version":7,"revision":1,"questions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQuestions(r); err == nil || !strings.Contains(err.Error(), "schema_version 7") {
		t.Errorf("an unsupported version must be reported: %v", err)
	}
}

func TestConcurrentAddsDoNotLoseQuestions(t *testing.T) {
	r := newRepo(t)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := twoOptions()
			q.Question = strings.Repeat("q", i+1) + "?"
			if _, err := AddQuestions(r, []NewQuestion{q}); err != nil {
				t.Errorf("add %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	all, _ := LoadQuestions(r)
	ids := map[string]bool{}
	for _, q := range all {
		ids[q.ID] = true
	}
	if len(all) != 6 || len(ids) != 6 {
		t.Errorf("%d questions and %d distinct ids, want 6 and 6", len(all), len(ids))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: NewQuestion` (and the other names).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/questions.go`:

```go
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

const (
	questionsFile      = "questions.json"
	questionsSchema    = 1
	maxAnswerTextRunes = 2000
	QuestionOpen       = "open"
	QuestionAnswered   = "answered"
)

var (
	// ErrNoSuchQuestion is returned when a question id does not exist.
	ErrNoSuchQuestion = errors.New("plan: no such question")
	// ErrAlreadyAnswered is returned when answering a question that is answered.
	ErrAlreadyAnswered = errors.New("plan: the question is already answered")
	// ErrInvalidAnswer is returned when an answer does not fit its question.
	ErrInvalidAnswer = errors.New("plan: invalid answer")
)

// Option is one choice offered for a question.
type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Recommendation is the option or options a pass recommends, with its reason.
// A recommendation is never an answer.
type Recommendation struct {
	OptionIDs []string `json:"option_ids"`
	Rationale string   `json:"rationale"`
}

// Answer is a person's answer: the options they chose and/or free text. A
// question always accepts free text.
type Answer struct {
	OptionIDs []string `json:"option_ids"`
	Text      string   `json:"text"`
}

// Question is a decision the plan needs from a person.
type Question struct {
	ID            string          `json:"id"`
	Question      string          `json:"question"`
	Why           string          `json:"why"`
	Options       []Option        `json:"options"`
	MultiSelect   bool            `json:"multi_select"`
	AllowFreeText bool            `json:"allow_free_text"`
	Recommended   *Recommendation `json:"recommended"`
	Affects       []string        `json:"affects"` // node ids the answer changes
	Source        string          `json:"source"`  // where it was asked, for a person to read
	Status        string          `json:"status"`
	Answer        *Answer         `json:"answer"`
	AnsweredAt    string          `json:"answered_at"`
}

// NewOption and NewQuestion describe a question before it has an id.
type NewOption struct {
	Label       string
	Description string
}

// NewQuestion is a question to add. Option ids are assigned by position
// (opt-1, opt-2, ...); RecommendedLabels must name options by label.
type NewQuestion struct {
	Question          string
	Why               string
	Options           []NewOption
	MultiSelect       bool
	RecommendedLabels []string
	Rationale         string
	Affects           []string
	Source            string // where it was asked, for a person to read
}

type questionFile struct {
	SchemaVersion int        `json:"schema_version"`
	Revision      int        `json:"revision"`
	Questions     []Question `json:"questions"`
}

func questionsPath(repo *plantree.Repo) string { return filepath.Join(repo.Dir(), questionsFile) }

func lockQuestions(repo *plantree.Repo) (func(), error) {
	dir := filepath.Join(repo.Dir(), "_state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return lockfile.Acquire(filepath.Join(dir, "questions.lock"))
}

func loadQuestionFile(repo *plantree.Repo) (questionFile, error) {
	b, err := os.ReadFile(questionsPath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return questionFile{SchemaVersion: questionsSchema}, nil
	}
	if err != nil {
		return questionFile{}, err
	}
	var f questionFile
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return questionFile{}, fmt.Errorf("plan: reading %s: %w", questionsPath(repo), err)
	}
	if f.SchemaVersion != questionsSchema {
		return questionFile{}, fmt.Errorf("plan: %s has unsupported schema_version %d (want %d)", questionsPath(repo), f.SchemaVersion, questionsSchema)
	}
	return f, nil
}

func saveQuestionFile(repo *plantree.Repo, f questionFile) error {
	f.SchemaVersion = questionsSchema
	f.Revision++
	if f.Questions == nil {
		f.Questions = []Question{}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return lockfile.WriteAtomic(questionsPath(repo), append(b, '\n'), 0o644)
}

// LoadQuestions returns every question, open and answered, in the order asked.
func LoadQuestions(repo *plantree.Repo) ([]Question, error) {
	f, err := loadQuestionFile(repo)
	return f.Questions, err
}

// OpenQuestions returns the questions still waiting for an answer.
func OpenQuestions(repo *plantree.Repo) ([]Question, error) {
	all, err := LoadQuestions(repo)
	var open []Question
	for _, q := range all {
		if q.Status == QuestionOpen {
			open = append(open, q)
		}
	}
	return open, err
}

// AddQuestions adds new questions and returns the resulting Question for each,
// in order. A question whose text (ignoring case and spacing) matches one
// already asked, open or answered, is not added again: its existing record is
// returned, so replaying a pass after a crash asks nothing twice.
func AddQuestions(repo *plantree.Repo, in []NewQuestion) ([]Question, error) {
	if len(in) == 0 {
		return nil, nil
	}
	unlock, err := lockQuestions(repo)
	if err != nil {
		return nil, err
	}
	defer unlock()
	f, err := loadQuestionFile(repo)
	if err != nil {
		return nil, err
	}
	out := make([]Question, 0, len(in))
	changed := false
	for _, nq := range in {
		key := NormalizeTitle(nq.Question)
		found := -1
		for i, q := range f.Questions {
			if NormalizeTitle(q.Question) == key {
				found = i
				break
			}
		}
		if found >= 0 {
			out = append(out, f.Questions[found])
			continue
		}
		q, err := buildQuestion(nq, nextQuestionID(f.Questions))
		if err != nil {
			return nil, err
		}
		f.Questions = append(f.Questions, q)
		out = append(out, q)
		changed = true
	}
	if changed {
		if err := saveQuestionFile(repo, f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func nextQuestionID(qs []Question) string {
	highest := 0
	for _, q := range qs {
		if len(q.ID) > 2 {
			n := 0
			fmt.Sscanf(q.ID[2:], "%d", &n)
			if n > highest {
				highest = n
			}
		}
	}
	return fmt.Sprintf("q-%03d", highest+1)
}

func buildQuestion(nq NewQuestion, id string) (Question, error) {
	q := Question{
		ID:            id,
		Question:      strings.TrimSpace(nq.Question),
		Why:           strings.TrimSpace(nq.Why),
		MultiSelect:   nq.MultiSelect,
		AllowFreeText: true,
		Affects:       append([]string{}, nq.Affects...),
		Source:        strings.TrimSpace(nq.Source),
		Status:        QuestionOpen,
		Options:       make([]Option, len(nq.Options)),
	}
	byLabel := map[string]string{}
	for i, o := range nq.Options {
		oid := fmt.Sprintf("opt-%d", i+1)
		q.Options[i] = Option{ID: oid, Label: strings.TrimSpace(o.Label), Description: strings.TrimSpace(o.Description)}
		byLabel[NormalizeTitle(o.Label)] = oid
	}
	if len(nq.RecommendedLabels) > 0 {
		rec := &Recommendation{Rationale: strings.TrimSpace(nq.Rationale), OptionIDs: []string{}}
		for _, l := range nq.RecommendedLabels {
			oid, ok := byLabel[NormalizeTitle(l)]
			if !ok {
				return Question{}, fmt.Errorf("plan: question %q recommends %q, which is not one of its options", cutBytes(nq.Question, 80), cutBytes(l, 80))
			}
			rec.OptionIDs = append(rec.OptionIDs, oid)
		}
		q.Recommended = rec
	}
	return q, nil
}

// AnswerQuestion records a person's answer to an open question. The answer
// must select at least one option or give text, select only options the
// question offers (each once), and select at most one option unless the
// question is multi-select. Free text is always allowed.
func AnswerQuestion(repo *plantree.Repo, id string, a Answer) (Question, error) {
	unlock, err := lockQuestions(repo)
	if err != nil {
		return Question{}, err
	}
	defer unlock()
	f, err := loadQuestionFile(repo)
	if err != nil {
		return Question{}, err
	}
	for i := range f.Questions {
		q := &f.Questions[i]
		if q.ID != id {
			continue
		}
		if q.Status != QuestionOpen {
			return Question{}, fmt.Errorf("%w: %s", ErrAlreadyAnswered, id)
		}
		if err := checkAnswer(*q, a); err != nil {
			return Question{}, err
		}
		q.Status = QuestionAnswered
		q.Answer = &Answer{OptionIDs: append([]string{}, a.OptionIDs...), Text: strings.TrimSpace(a.Text)}
		q.AnsweredAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveQuestionFile(repo, f); err != nil {
			return Question{}, err
		}
		return *q, nil
	}
	return Question{}, fmt.Errorf("%w: %s", ErrNoSuchQuestion, id)
}

func checkAnswer(q Question, a Answer) error {
	text := strings.TrimSpace(a.Text)
	if len(a.OptionIDs) == 0 && text == "" {
		return fmt.Errorf("%w: choose an option or write an answer", ErrInvalidAnswer)
	}
	if utf8.RuneCountInString(text) > maxAnswerTextRunes {
		return fmt.Errorf("%w: the text is longer than %d characters", ErrInvalidAnswer, maxAnswerTextRunes)
	}
	if len(a.OptionIDs) > 1 && !q.MultiSelect {
		return fmt.Errorf("%w: this question accepts one option", ErrInvalidAnswer)
	}
	valid := map[string]bool{}
	for _, o := range q.Options {
		valid[o.ID] = true
	}
	seen := map[string]bool{}
	for _, oid := range a.OptionIDs {
		if !valid[oid] {
			return fmt.Errorf("%w: %q is not an option of %s", ErrInvalidAnswer, cutBytes(oid, 40), q.ID)
		}
		if seen[oid] {
			return fmt.Errorf("%w: option %s chosen twice", ErrInvalidAnswer, oid)
		}
		seen[oid] = true
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/questions.go gophermind-lib/plantree/plan/questions_test.go
git commit -m "feat(plan): a store for questions, options, recommendations and answers

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 3: Validating the questions a model proposes

**Files:**
- Create: `gophermind-lib/plantree/plan/questions_parse.go`, `questions_parse_test.go`

**Interfaces:**
- Consumes: `NormalizeTitle`, `clip` (existing), `NewQuestion`, `NewOption` (Task 2).
- Produces:
  - `type OptionOut struct{ Label, Description string }`, `type QuestionOut struct{ Question, Why string; Options []OptionOut; MultiSelect bool; Recommended []string; Rationale string; Affects []string }` (JSON: `question`, `why`, `options`, `multi_select`, `recommended`, `rationale`, `affects`)
  - `func validateQuestionOuts(qs []QuestionOut, checkAffect func(where, affect string) error) error` (at most 5 questions; 2 to 8 distinct options or none; recommended labels must be options, one unless multi-select; bounded fields; every error bounded)
  - `func newQuestion(o QuestionOut, affects []string) NewQuestion`
  - `test helper goodQ()` in `questions_parse_test.go`, reused by later tests

`affects` means phase or task titles in pass 1 and step ids in pass 2, so each pass supplies its own check.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/questions_parse_test.go`:

```go
package plan

import (
	"errors"
	"strings"
	"testing"
)

func goodQ() QuestionOut {
	return QuestionOut{
		Question:    "Which database?",
		Why:         "it shapes the schema",
		Options:     []OptionOut{{Label: "SQLite", Description: "embedded"}, {Label: "Postgres"}},
		Recommended: []string{"sqlite"},
		Rationale:   "simplest",
		Affects:     []string{"Repo layout"},
	}
}

func TestValidateQuestionOutsAccepts(t *testing.T) {
	free := QuestionOut{Question: "What is the deadline?"}
	multi := goodQ()
	multi.Question = "Which platforms?"
	multi.MultiSelect = true
	multi.Recommended = []string{"SQLite", "Postgres"}
	if err := validateQuestionOuts([]QuestionOut{goodQ(), free, multi}, nil); err != nil {
		t.Errorf("valid questions rejected: %v", err)
	}
	if err := validateQuestionOuts(nil, nil); err != nil {
		t.Errorf("no questions is valid: %v", err)
	}
}

func TestValidateQuestionOutsRejects(t *testing.T) {
	with := func(f func(*QuestionOut)) []QuestionOut {
		q := goodQ()
		f(&q)
		return []QuestionOut{q}
	}
	many := make([]QuestionOut, maxQuestionsPerPass+1)
	for i := range many {
		many[i] = QuestionOut{Question: strings.Repeat("q", i+1)}
	}
	cases := map[string]struct {
		qs   []QuestionOut
		want string
	}{
		"too many questions": {many, "too many questions"},
		"empty question":     {with(func(q *QuestionOut) { q.Question = " " }), "a question is empty"},
		"long question":      {with(func(q *QuestionOut) { q.Question = strings.Repeat("q", maxQuestionRunes+1) }), "longer than"},
		"long why":           {with(func(q *QuestionOut) { q.Why = strings.Repeat("w", maxWhyRunes+1) }), "why is longer"},
		"duplicate question": {[]QuestionOut{goodQ(), goodQ()}, "asked twice"},
		"one option":         {with(func(q *QuestionOut) { q.Options = q.Options[:1]; q.Recommended = nil }), "at least two options"},
		"too many options": {with(func(q *QuestionOut) {
			q.Options = nil
			for i := 0; i <= maxOptions; i++ {
				q.Options = append(q.Options, OptionOut{Label: strings.Repeat("o", i+1)})
			}
			q.Recommended = nil
		}), "too many options"},
		"empty label":         {with(func(q *QuestionOut) { q.Options[1].Label = "" }), "label is empty"},
		"long label":          {with(func(q *QuestionOut) { q.Options[1].Label = strings.Repeat("l", maxLabelRunes+1) }), "label is longer"},
		"duplicate label":     {with(func(q *QuestionOut) { q.Options[1].Label = " sqlite " }), "appears twice"},
		"long description":    {with(func(q *QuestionOut) { q.Options[0].Description = strings.Repeat("d", maxOptionDescRunes+1) }), "description is longer"},
		"unknown recommend":   {with(func(q *QuestionOut) { q.Recommended = []string{"Oracle"} }), "not one of its options"},
		"two on single":       {with(func(q *QuestionOut) { q.Recommended = []string{"SQLite", "Postgres"} }), "not multi_select"},
		"duplicate recommend": {with(func(q *QuestionOut) { q.MultiSelect = true; q.Recommended = []string{"SQLite", "sqlite"} }), "twice"},
		"long rationale":      {with(func(q *QuestionOut) { q.Rationale = strings.Repeat("r", maxRationaleRunes+1) }), "rationale is longer"},
		"empty affects":       {with(func(q *QuestionOut) { q.Affects = []string{""} }), "affects entry is empty"},
		"long affects":        {with(func(q *QuestionOut) { q.Affects = []string{strings.Repeat("a", maxAffectRunes+1)} }), "affects entry is longer"},
		"too many affects": {with(func(q *QuestionOut) {
			q.Affects = make([]string, maxAffects+1)
			for i := range q.Affects {
				q.Affects[i] = "x"
			}
		}), "too many affects"},
	}
	for name, c := range cases {
		err := validateQuestionOuts(c.qs, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, c.want)
		}
	}
}

func TestValidateQuestionOutsCallsTheAffectsCheckAndKeepsItsError(t *testing.T) {
	boom := errors.New("not a step of this batch")
	var seen []string
	err := validateQuestionOuts([]QuestionOut{goodQ()}, func(where, affect string) error {
		seen = append(seen, affect)
		return boom
	})
	if !errors.Is(err, boom) || len(seen) != 1 || seen[0] != "Repo layout" {
		t.Errorf("err=%v seen=%v", err, seen)
	}
}

func TestValidateQuestionOutsErrorsStayBounded(t *testing.T) {
	huge := strings.Repeat("x", 3_000_000)
	q := goodQ()
	q.Question = huge
	err := validateQuestionOuts([]QuestionOut{q}, nil)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("error length = %d", len(err.Error()))
	}
	q = goodQ()
	q.Recommended = []string{huge}
	err = validateQuestionOuts([]QuestionOut{q}, nil)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("recommended: error length = %d", len(err.Error()))
	}
}

func TestNewQuestionMapsOptionsAndKeepsResolvedAffects(t *testing.T) {
	nq := newQuestion(goodQ(), []string{"phase-001.task-001"})
	if nq.Question != "Which database?" || len(nq.Options) != 2 || nq.Options[0].Label != "SQLite" ||
		nq.RecommendedLabels[0] != "sqlite" || nq.Affects[0] != "phase-001.task-001" {
		t.Errorf("NewQuestion = %+v", nq)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: QuestionOut` (and `validateQuestionOuts`).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/questions_parse.go`:

```go
package plan

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on the questions one pass may ask.
const (
	maxQuestionsPerPass = 5
	maxQuestionRunes    = 500
	maxWhyRunes         = 500
	maxOptions          = 8
	maxLabelRunes       = 100
	maxOptionDescRunes  = 300
	maxRationaleRunes   = 300
	maxAffects          = 10
	maxAffectRunes      = 200
)

// OptionOut is a choice a pass offers for a question.
type OptionOut struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionOut is a question a pass asks. Recommended lists option labels.
// Affects means phase or task titles in pass 1 and step ids in pass 2.
type QuestionOut struct {
	Question    string      `json:"question"`
	Why         string      `json:"why"`
	Options     []OptionOut `json:"options"`
	MultiSelect bool        `json:"multi_select"`
	Recommended []string    `json:"recommended"`
	Rationale   string      `json:"rationale"`
	Affects     []string    `json:"affects"`
}

// validateQuestionOuts checks the questions a pass returned. checkAffect, if
// not nil, is called for every affects entry so each pass can hold its own
// meaning of it.
func validateQuestionOuts(qs []QuestionOut, checkAffect func(where, affect string) error) error {
	if len(qs) > maxQuestionsPerPass {
		return fmt.Errorf("too many questions (%d, at most %d)", len(qs), maxQuestionsPerPass)
	}
	seen := map[string]bool{}
	for _, q := range qs {
		where := fmt.Sprintf("question %q", clip(q.Question))
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("a question is empty")
		}
		if utf8.RuneCountInString(q.Question) > maxQuestionRunes {
			return fmt.Errorf("%s: longer than %d characters", where, maxQuestionRunes)
		}
		if utf8.RuneCountInString(q.Why) > maxWhyRunes {
			return fmt.Errorf("%s: why is longer than %d characters", where, maxWhyRunes)
		}
		key := NormalizeTitle(q.Question)
		if seen[key] {
			return fmt.Errorf("%s: asked twice", where)
		}
		seen[key] = true
		labels, err := checkOptions(where, q)
		if err != nil {
			return err
		}
		if err := checkRecommended(where, q, labels); err != nil {
			return err
		}
		if len(q.Affects) > maxAffects {
			return fmt.Errorf("%s: too many affects (%d, at most %d)", where, len(q.Affects), maxAffects)
		}
		for _, a := range q.Affects {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("%s: an affects entry is empty", where)
			}
			if utf8.RuneCountInString(a) > maxAffectRunes {
				return fmt.Errorf("%s: an affects entry is longer than %d characters", where, maxAffectRunes)
			}
			if checkAffect != nil {
				if err := checkAffect(where, a); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkOptions requires no options (a free-text question) or 2 to 8 distinct
// ones, and returns the set of normalized labels.
func checkOptions(where string, q QuestionOut) (map[string]bool, error) {
	labels := map[string]bool{}
	if len(q.Options) == 1 {
		return nil, fmt.Errorf("%s: give at least two options, or none for a free-text question", where)
	}
	if len(q.Options) > maxOptions {
		return nil, fmt.Errorf("%s: too many options (%d, at most %d)", where, len(q.Options), maxOptions)
	}
	for _, o := range q.Options {
		if strings.TrimSpace(o.Label) == "" {
			return nil, fmt.Errorf("%s: an option label is empty", where)
		}
		if utf8.RuneCountInString(o.Label) > maxLabelRunes {
			return nil, fmt.Errorf("%s: an option label is longer than %d characters", where, maxLabelRunes)
		}
		if utf8.RuneCountInString(o.Description) > maxOptionDescRunes {
			return nil, fmt.Errorf("%s: an option description is longer than %d characters", where, maxOptionDescRunes)
		}
		key := NormalizeTitle(o.Label)
		if labels[key] {
			return nil, fmt.Errorf("%s: option %q appears twice", where, clip(o.Label))
		}
		labels[key] = true
	}
	return labels, nil
}

func checkRecommended(where string, q QuestionOut, labels map[string]bool) error {
	if utf8.RuneCountInString(q.Rationale) > maxRationaleRunes {
		return fmt.Errorf("%s: rationale is longer than %d characters", where, maxRationaleRunes)
	}
	if len(q.Recommended) > 1 && !q.MultiSelect {
		return fmt.Errorf("%s: recommends %d options but is not multi_select", where, len(q.Recommended))
	}
	seen := map[string]bool{}
	for _, l := range q.Recommended {
		key := NormalizeTitle(l)
		if !labels[key] {
			return fmt.Errorf("%s: recommends %q, which is not one of its options", where, clip(l))
		}
		if seen[key] {
			return fmt.Errorf("%s: recommends %q twice", where, clip(l))
		}
		seen[key] = true
	}
	return nil
}

// newQuestion converts a validated QuestionOut, with its affects already
// resolved to node ids, into a question to add.
func newQuestion(o QuestionOut, affects []string) NewQuestion {
	nq := NewQuestion{
		Question:          o.Question,
		Why:               o.Why,
		MultiSelect:       o.MultiSelect,
		RecommendedLabels: o.Recommended,
		Rationale:         o.Rationale,
		Affects:           affects,
	}
	for _, opt := range o.Options {
		nq.Options = append(nq.Options, NewOption{Label: opt.Label, Description: opt.Description})
	}
	return nq
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/questions_parse.go gophermind-lib/plantree/plan/questions_parse_test.go
git commit -m "feat(plan): strict validation of the questions a pass proposes

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 4: Pass 1 asks questions and holds the steps they affect

**Files:**
- Create: `gophermind-lib/plantree/plan/questions_apply.go`, `questions_apply_test.go`
- Replace: `pass1json.go`, `pass1json_test.go`, `prompt.go`, `overview_test.go` (holds the prompt tests), `runner.go` (same directory)

**Interfaces:**
- Consumes: `validateQuestionOuts`, `newQuestion` (Task 3), `AddQuestions`, `LoadQuestions` (Task 2), `needsSpec`, `mergeTracked`, `recordProvenance` (existing).
- Produces:
  - `Pass1Output.Questions []QuestionOut` (optional; `ParsePass1` validates it)
  - the pass-1 prompt rules and JSON shape teach questions
  - `func stepsUnder(repo, ids []string) ([]plantree.Node, error)`, `func holdSteps(repo, ids []string) (int, error)` (moves waiting steps under `ids` to `awaiting_answers`; idempotent)
  - `func applyPass1Questions(repo, chunk Chunk, out Pass1Output, touched []string) (int, error)` (resolves affected titles among the chunk's nodes, ignoring unmatched ones; replay-safe; an answered question holds nothing)
  - `Result.Questions int`; `runChunk` returns a `chunkResult{created, questions}` and records questions after provenance and before the overview
  - test helpers `byChunkQ`, `reply2Q`, `stageOf` in `questions_apply_test.go`, reused by later tests

The four existing files are replaced with versions that add the questions behavior; everything else in them is unchanged.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/questions_apply_test.go`:

```go
package plan

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

const questionJSON = `"questions":[{"question":"Which framework?","why":"it changes T2","options":[{"label":"A","description":"first"},{"label":"B","description":"second"}],"multi_select":false,"recommended":["A"],"rationale":"common","affects":["T2"]}],`

// reply2Q is reply2 with a question that affects the task titled T2.
var reply2Q = strings.Replace(reply2, `"overview":"overview 2"`, questionJSON+`"overview":"overview 2"`, 1)

// byChunkQ answers like byChunk but asks a question in chunk 2.
func byChunkQ(n int, prompt string) (string, error) {
	if strings.Contains(prompt, "second part text") {
		return reply2Q, nil
	}
	return byChunk(n, prompt)
}

func stageOf(t *testing.T, r *plantree.Repo, id string) plantree.Stage {
	t.Helper()
	n, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return n.Planning.Stage
}

func TestRunPass1AsksAQuestionAndHoldsTheAffectedSteps(t *testing.T) {
	r := plantree.Open(t.TempDir())
	res, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Questions != 1 {
		t.Errorf("Result.Questions = %d, want 1", res.Questions)
	}
	qs, err := LoadQuestions(r)
	if err != nil || len(qs) != 1 {
		t.Fatalf("questions = %+v, %v", qs, err)
	}
	q := qs[0]
	if q.Status != QuestionOpen || q.Source != "chunk 2 of the brief" || len(q.Affects) != 1 || q.Affects[0] != "phase-001.task-002" ||
		q.Recommended == nil || q.Recommended.OptionIDs[0] != "opt-1" {
		t.Errorf("question = %+v", q)
	}
	if got := stageOf(t, r, "phase-001.task-002.step-001"); got != plantree.StageAwaitingAnswers {
		t.Errorf("the affected step is %s, want awaiting_answers", got)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-002.task-001.step-001"} {
		if got := stageOf(t, r, id); got != plantree.StageSkeleton {
			t.Errorf("%s = %s, want skeleton (the question does not affect it)", id, got)
		}
	}
	a, _ := r.NextActions()
	var answers int
	for _, x := range a.Blocked {
		if x.Kind == plantree.ActionAnswer && x.NodeID == "phase-001.task-002.step-001" {
			answers++
		}
	}
	if answers != 1 || len(a.Runnable) != 2 {
		t.Errorf("Blocked=%v Runnable=%v; want one answer action for the held step and two draft actions", a.Blocked, a.Runnable)
	}
}

func TestReplayingAPassAsksNothingTwice(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts); err != nil {
		t.Fatal(err)
	}
	out, err := ParsePass1(reply2Q)
	if err != nil {
		t.Fatal(err)
	}
	_, touched, err := mergeTracked(r, out)
	if err != nil {
		t.Fatal(err)
	}
	added, err := applyPass1Questions(r, Chunk{Index: 1}, out, touched)
	if err != nil || added != 0 {
		t.Fatalf("replay added %d questions, err=%v; want 0", added, err)
	}
	if qs, _ := LoadQuestions(r); len(qs) != 1 {
		t.Errorf("%d questions after a replay, want 1", len(qs))
	}
}

func TestAnAnsweredQuestionHoldsNothingOnReplay(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts); err != nil {
		t.Fatal(err)
	}
	// Put the held step back as a skeleton, as ReleaseAnswered will, then answer.
	step, _ := r.Get("phase-001.task-002.step-001")
	if _, err := r.Update(step.ID, step.NodeRevision, func(n *plantree.Node) error {
		n.Planning.Stage = plantree.StageSkeleton
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}}); err != nil {
		t.Fatal(err)
	}
	out, _ := ParsePass1(reply2Q)
	_, touched, _ := mergeTracked(r, out)
	if _, err := applyPass1Questions(r, Chunk{Index: 1}, out, touched); err != nil {
		t.Fatal(err)
	}
	if got := stageOf(t, r, step.ID); got != plantree.StageSkeleton {
		t.Errorf("a replay of an answered question re-held the step: %s", got)
	}
}

func TestQuestionTitlesThatMatchNothingAreIgnored(t *testing.T) {
	r := plantree.Open(t.TempDir())
	odd := strings.Replace(reply2Q, `"affects":["T2"]`, `"affects":["No such task"]`, 1)
	f := &fake{reply: func(n int, p string) (string, error) {
		if strings.Contains(p, "second part text") {
			return odd, nil
		}
		return byChunk(n, p)
	}}
	if _, err := RunPass1(context.Background(), r, threePartBrief, f, opts); err != nil {
		t.Fatal(err)
	}
	qs, _ := LoadQuestions(r)
	if len(qs) != 1 || len(qs[0].Affects) != 0 {
		t.Errorf("questions = %+v; want one question with no resolved affects", qs)
	}
	if got := stageOf(t, r, "phase-001.task-002.step-001"); got != plantree.StageSkeleton {
		t.Errorf("a question that affects nothing must hold nothing, got %s", got)
	}
}

func TestHoldStepsCoversTheWholeSubtreeAndSkipsHeldOrSpecifiedSteps(t *testing.T) {
	r := newRepo(t)
	out := sampleOut()
	out.Phases[0].Tasks[0].Steps = append(out.Phases[0].Tasks[0].Steps, StepOut{Title: "Add lint", Digest: "style"})
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	// step-002 is skipped, step-003 already drafted.
	second, _ := r.Get(s2)
	if _, err := r.Update(s2, second.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	third, _ := r.Get(s3)
	if _, err := r.Update(s3, third.NodeRevision, func(n *plantree.Node) error {
		n.Work = draftedWorkFor()
		n.Planning.Stage = plantree.StageDrafted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	held, err := holdSteps(r, []string{"phase-001"})
	if err != nil || held != 1 {
		t.Fatalf("held = %d, err = %v; want only step 1", held, err)
	}
	if stageOf(t, r, s1) != plantree.StageAwaitingAnswers || stageOf(t, r, s3) != plantree.StageDrafted {
		t.Error("only the waiting step may be held")
	}
	if held, _ := holdSteps(r, []string{"phase-001"}); held != 0 {
		t.Error("holding again must change nothing")
	}
	if held, _ := holdSteps(r, nil); held != 0 {
		t.Error("no ids holds nothing")
	}
}

func draftedWorkFor() *plantree.Work {
	return &plantree.Work{Description: "d", AcceptanceCriteria: []string{"c"}}
}
```

Replace the whole of `plantree/plan/pass1json_test.go`:

```go
package plan

import (
	"errors"
	"strings"
	"testing"
)

const goodReply = `Here is the plan.
` + "```json" + `
{"phases":[{"title":"Foundation","digest":"Everything else rests on this.","objective":"Set up the base.",
 "tasks":[{"title":"Repo layout","digest":"Where code lives.","objective":"",
  "steps":[{"title":"Create the module","digest":"Needed to compile anything."}]}]}],
 "overview":"A small project."}
` + "```" + `
Hope that helps.`

func TestExtractJSON(t *testing.T) {
	got, err := ExtractJSON(`noise {"a":"}{","b":{"c":1}} trailing {"x":2}`)
	if err != nil || got != `{"a":"}{","b":{"c":1}}` {
		t.Errorf("ExtractJSON = %q, %v", got, err)
	}
	got, err = ExtractJSON(`{"quote":"say \"hi\" {"}`)
	if err != nil || got != `{"quote":"say \"hi\" {"}` {
		t.Errorf("escaped quote: %q, %v", got, err)
	}
	for _, bad := range []string{"no json here", `{"open":1`, ""} {
		if _, err := ExtractJSON(bad); err == nil {
			t.Errorf("ExtractJSON(%q) should fail", bad)
		}
	}
}

func TestParsePass1Accepts(t *testing.T) {
	out, err := ParsePass1(goodReply)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Phases) != 1 || out.Phases[0].Tasks[0].Steps[0].Title != "Create the module" || out.Overview != "A small project." {
		t.Errorf("parsed %+v", out)
	}
	if _, err := ParsePass1(`{"phases":[],"overview":"nothing new in this chunk"}`); err != nil {
		t.Errorf("a chunk that adds nothing is valid: %v", err)
	}
}

func TestParsePass1Rejects(t *testing.T) {
	long := strings.Repeat("x", 501)
	cases := map[string]string{
		"no overview":         `{"phases":[]}`,
		"blank overview":      `{"phases":[],"overview":"  "}`,
		"unknown field":       `{"phases":[],"overview":"o","extra":1}`,
		"not json":            `no braces at all`,
		"empty phase title":   `{"phases":[{"title":"","digest":"d","objective":"","tasks":[]}],"overview":"o"}`,
		"empty digest":        `{"phases":[{"title":"P","digest":"","objective":"","tasks":[]}],"overview":"o"}`,
		"long digest":         `{"phases":[{"title":"P","digest":"` + long + `","objective":"","tasks":[]}],"overview":"o"}`,
		"duplicate phases":    `{"phases":[{"title":"P","digest":"d","objective":"","tasks":[]},{"title":" p ","digest":"d","objective":"","tasks":[]}],"overview":"o"}`,
		"duplicate steps":     `{"phases":[{"title":"P","digest":"d","objective":"","tasks":[{"title":"T","digest":"d","objective":"","steps":[{"title":"S","digest":"d"},{"title":"s","digest":"d"}]}]}],"overview":"o"}`,
		"step missing digest": `{"phases":[{"title":"P","digest":"d","objective":"","tasks":[{"title":"T","digest":"d","objective":"","steps":[{"title":"S","digest":""}]}]}],"overview":"o"}`,
	}
	for name, reply := range cases {
		if _, err := ParsePass1(reply); err == nil {
			t.Errorf("%s: ParsePass1 accepted an invalid reply", name)
		}
	}
	var many strings.Builder
	many.WriteString(`{"phases":[`)
	for i := 0; i < maxPhasesPerPass+1; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"title":"P` + strings.Repeat("x", i) + `","digest":"d","objective":"","tasks":[]}`)
	}
	many.WriteString(`],"overview":"o"}`)
	if _, err := ParsePass1(many.String()); err == nil || !strings.Contains(err.Error(), "too many phases") {
		t.Errorf("too many phases: %v", err)
	}
}

func TestParsePass1ErrorsNameTheProblem(t *testing.T) {
	_, err := ParsePass1(`{"phases":[{"title":"Setup","digest":"","objective":"","tasks":[]}],"overview":"o"}`)
	if err == nil || !strings.Contains(err.Error(), `phase "Setup"`) || !strings.Contains(err.Error(), "digest") {
		t.Errorf("error should name the phase and the field: %v", err)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if NormalizeTitle("  Set   UP\tRepo ") != "set up repo" {
		t.Error("NormalizeTitle did not fold case and whitespace")
	}
}

func TestParsePass1ErrorIsBoundedByAHugeTitle(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	_, err := ParsePass1(`{"phases":[{"title":"` + huge + `","digest":"","objective":"","tasks":[]}],"overview":"o"}`)
	if err == nil || len(err.Error()) > 500 || !strings.Contains(err.Error(), "title is longer") {
		n := 0
		if err != nil {
			n = len(err.Error())
		}
		t.Errorf("want a short error naming the title field, got %d bytes: %v", n, err)
	}
}

func TestExtractJSONFindsTheRealObject(t *testing.T) {
	cases := map[string]string{
		`I used {curly} braces. {"a":1}`:           `{"a":1}`,
		`note { unbalanced. {"a":1}`:               `{"a":1}`,
		`{bad} then {"ok":true}`:                   `{"ok":true}`,
		`{bad}`:                                    `{bad}`,
		`{"a":"x\\"}`:                              `{"a":"x\\"}`,
		`{"phases":[{"title":"P"}], "overview": }`: `{"phases":[{"title":"P"}], "overview": }`,
	}
	for in, want := range cases {
		got, err := ExtractJSON(in)
		if err != nil || got != want {
			t.Errorf("ExtractJSON(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := ParsePass1(`{bad}`)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Errorf("ParsePass1({bad}) = %v, want a decode error", err)
	}
	if _, err := ExtractJSON(`only { prose`); err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Errorf("unclosed: %v", err)
	}
}

func TestCandidates(t *testing.T) {
	list, seen := candidates(`a {"x":{"y":1}} b {bad} {open`)
	if !seen || len(list) != 2 || list[0] != `{"x":{"y":1}}` || list[1] != `{bad}` {
		t.Errorf("candidates = %q, seen=%v", list, seen)
	}
	if list, seen := candidates("no braces at all"); seen || len(list) != 0 {
		t.Errorf("no braces: %q, %v", list, seen)
	}
}

func TestParsePass1SkipsAnEarlierObjectThatIsNotThePlan(t *testing.T) {
	out, err := ParsePass1("I checked {} and {\"note\":1} first.\n" + goodReply)
	if err != nil || len(out.Phases) != 1 || out.Overview != "A small project." {
		t.Fatalf("ParsePass1 = %+v, %v", out, err)
	}
}

func TestParsePass1ReportsTheFirstCandidatesError(t *testing.T) {
	_, err := ParsePass1(`{"phases":[],"extra":1} {"overview":""}`)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Errorf("err = %v, want the first candidate's schema error", err)
	}
}

func TestParsePass1BoundsTheSchemaError(t *testing.T) {
	reply := `{"phases":[],"overview":"o","` + strings.Repeat("k", 200000) + `":1}`
	_, err := ParsePass1(reply)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) >= 700 {
		t.Errorf("error is %d bytes", len(err.Error()))
	}
}

func TestParsePass1ReportsTheLongestCandidate(t *testing.T) {
	_, err := ParsePass1("{} " + `{"phases":[{"title":"","digest":"d","objective":"","tasks":[]}],"overview":"o"}`)
	if err == nil || !strings.Contains(err.Error(), "title is empty") || strings.Contains(err.Error(), "overview") {
		t.Errorf("err = %v", err)
	}
	_, err = ParsePass1(`{"phases":[],"extra":1} {"overview":""}`)
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Errorf("first, longer candidate should win: %v", err)
	}
}

func TestParseFirst(t *testing.T) {
	dec := func(raw string) (int, error) {
		if raw == "{}" {
			return 0, errors.New("bad " + raw)
		}
		return len(raw), nil
	}
	if _, err := parseFirst("no braces", dec); err == nil || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("no braces: %v", err)
	}
	if _, err := parseFirst("{", dec); err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Errorf("lone brace: %v", err)
	}
	v, err := parseFirst("{} {}", dec)
	if err == nil || v != 0 {
		t.Errorf("all fail: %d, %v", v, err)
	}
	v, err = parseFirst("{} {abc} {defg}", dec)
	if err != nil || v != 5 {
		t.Errorf("first success: %d, %v", v, err)
	}
}

func TestParsePass1AcceptsQuestionsAndRejectsBadOnes(t *testing.T) {
	ok := `{"phases":[],"overview":"o","questions":[{"question":"Which database?","why":"schema","options":[{"label":"SQLite","description":""},{"label":"Postgres","description":""}],"multi_select":false,"recommended":["SQLite"],"rationale":"simple","affects":["Repo layout"]}]}`
	out, err := ParsePass1(ok)
	if err != nil || len(out.Questions) != 1 || out.Questions[0].Options[1].Label != "Postgres" {
		t.Fatalf("ParsePass1 = %+v, %v", out, err)
	}
	if _, err := ParsePass1(`{"phases":[],"overview":"o"}`); err != nil {
		t.Errorf("questions are optional: %v", err)
	}
	bad := `{"phases":[],"overview":"o","questions":[{"question":"Q?","options":[{"label":"only one","description":""}],"multi_select":false,"recommended":[],"rationale":"","affects":[],"why":""}]}`
	if _, err := ParsePass1(bad); err == nil || !strings.Contains(err.Error(), "at least two options") {
		t.Errorf("a one-option question must be rejected: %v", err)
	}
}
```

Replace the whole of `plantree/plan/overview_test.go`:

```go
package plan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOverviewRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadOverview(dir); err != nil || got != "" {
		t.Fatalf("missing overview = %q, %v", got, err)
	}
	if err := WriteOverview(dir, "first\n\n"); err != nil {
		t.Fatal(err)
	}
	if err := WriteOverview(dir, "second version"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadOverview(dir); got != "second version\n" {
		t.Errorf("ReadOverview = %q", got)
	}
}

func TestFitOverview(t *testing.T) {
	short := "fits fine"
	if FitOverview(short, 100) != short {
		t.Error("text within the cap must be unchanged")
	}
	long := strings.Repeat("A paragraph of overview text.\n\n", 40)
	got := FitOverview(long, 200)
	if len(got) > 200 || !strings.HasSuffix(got, truncMarker) {
		t.Errorf("len=%d suffix ok=%v", len(got), strings.HasSuffix(got, truncMarker))
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, truncMarker)) {
		t.Error("the kept text must be a prefix of the original")
	}
	multibyte := strings.Repeat("é", 300)
	if got := FitOverview(multibyte, 101); !utf8.ValidString(got) || len(got) > 101 {
		t.Errorf("multibyte cut: valid=%v len=%d", utf8.ValidString(got), len(got))
	}
	if got := FitOverview(strings.Repeat("x", 50), 5); len(got) == 0 {
		t.Error("a tiny cap must still return something")
	}
}

func TestOutlineListsPhasesAndTasksOnly(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	got, err := Outline(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != "- Phase: Foundation\n  - Task: Repo layout\n" {
		t.Errorf("Outline = %q", got)
	}
}

func TestOutlineIsBoundedAndSaysWhatItOmitted(t *testing.T) {
	r := newRepo(t)
	var phases []PhaseOut
	for i := 0; i < 50; i++ {
		phases = append(phases, PhaseOut{Title: strings.Repeat("phase title ", 8) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Digest: "d"})
	}
	if _, err := Merge(r, Pass1Output{Overview: "o", Phases: phases}); err != nil {
		t.Fatal(err)
	}
	got, _ := Outline(r)
	if len(got) > outlineCapBytes+80 || !strings.Contains(got, "more phases and tasks not shown") {
		t.Errorf("outline len=%d, tail=%q", len(got), got[max(0, len(got)-120):])
	}
}

func TestPass1PromptCarriesOneChunkAndNoOthers(t *testing.T) {
	c := Chunk{Index: 1, Title: "Auth", Text: "The service must support login.\n"}
	p := Pass1Prompt("demo", "an overview", "- Phase: Foundation\n", c, 3)
	for _, want := range []string{`"demo"`, "part 2 of 3", "an overview", "- Phase: Foundation", "The service must support login.", "section: Auth", "ONE JSON object"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	empty := Pass1Prompt("demo", "", "", Chunk{Index: 0, Text: "x"}, 1)
	if strings.Count(empty, "(none yet)") != 2 {
		t.Errorf("empty overview and outline should read (none yet)")
	}
}

func TestRetryAndCompressPrompts(t *testing.T) {
	if p := RetryPrompt("ORIGINAL", "I think the plan is X", "digest is empty"); !strings.Contains(p, "ORIGINAL") || !strings.Contains(p, "digest is empty") || !strings.Contains(p, "I think the plan is X") {
		t.Errorf("RetryPrompt = %q", p)
	}
	if p := RetryPrompt("ORIGINAL", strings.Repeat("r", 100000), strings.Repeat("p", 5000)); len(p)-len("ORIGINAL") >= 2500 {
		t.Errorf("a 100,000 byte reply added %d bytes to the prompt", len(p)-len("ORIGINAL"))
	}
	if p := CompressPrompt("long overview", 500); !strings.Contains(p, "Compress") || !strings.Contains(p, "500") || !strings.Contains(p, "long overview") {
		t.Errorf("CompressPrompt = %q", p)
	}
}

func TestPass1PromptTellsTheModelEveryTaskNeedsAStep(t *testing.T) {
	p := Pass1Prompt("demo", "", "", Chunk{Index: 0, Text: "x"}, 1)
	if !strings.Contains(p, "at least one step") {
		t.Error("the pass-1 prompt must say every task needs at least one step")
	}
}

func TestPass1PromptTellsTheModelHowToAskQuestions(t *testing.T) {
	p := Pass1Prompt("demo", "", "", Chunk{Index: 0, Text: "x"}, 1)
	for _, want := range []string{"At most 5 questions", "genuinely ambiguous", "Never ask what the brief already answers", `"questions":[`, `"affects":[]`} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: applyPass1Questions` (and `holdSteps`, `Questions`).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/questions_apply.go`:

```go
package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// stepsUnder returns every step that is one of ids or lies below one of them.
func stepsUnder(repo *plantree.Repo, ids []string) ([]plantree.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []plantree.Node
	err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		for _, id := range ids {
			if n.ID == id || strings.HasPrefix(n.ID, id+".") {
				out = append(out, n)
				return nil
			}
		}
		return nil
	})
	return out, err
}

// holdSteps moves every step under ids that is still waiting for its
// specification (a skeleton or inspected step, not on hold) to the
// awaiting-answers stage, so pass 2 leaves it alone until its questions are
// answered. It returns how many steps it moved. Calling it again changes
// nothing.
func holdSteps(repo *plantree.Repo, ids []string) (int, error) {
	steps, err := stepsUnder(repo, ids)
	if err != nil {
		return 0, err
	}
	held := 0
	for _, s := range steps {
		if !needsSpec(s) {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageAwaitingAnswers
			return nil
		}); err != nil {
			return held, fmt.Errorf("holding %s for an answer: %w", s.ID, err)
		}
		held++
	}
	return held, nil
}

// applyPass1Questions records the questions a skeleton pass asked and holds
// the steps they affect. A question's affects are phase or task titles; they
// are matched, ignoring case and spacing, against the nodes this chunk created
// or reused, and a title that matches none is ignored. It returns how many
// questions were new. Replaying the same pass adds nothing and holds nothing
// new, and a question that is already answered holds nothing.
func applyPass1Questions(repo *plantree.Repo, chunk Chunk, out Pass1Output, touched []string) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	byTitle := map[string][]string{}
	for _, id := range touched {
		n, err := repo.Get(id)
		if err != nil {
			return 0, err
		}
		key := NormalizeTitle(n.Title)
		byTitle[key] = append(byTitle[key], id)
	}
	source := fmt.Sprintf("chunk %d of the brief", chunk.Index+1)
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		var ids []string
		seen := map[string]bool{}
		for _, title := range q.Affects {
			for _, id := range byTitle[NormalizeTitle(title)] {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		nq := newQuestion(q, ids)
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}
```

Replace the whole of `plantree/plan/pass1json.go`:

```go
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on what one skeleton pass may return. They keep a runaway reply from
// flooding the tree and keep every value small enough to sit in a prompt.
const (
	maxPhasesPerPass = 50
	maxTasksPerPhase = 100
	maxStepsPerTask  = 100
	maxTitleRunes    = 200
	maxDigestRunes   = 500
	maxObjective     = 1000
)

// Pass1Output is what one skeleton pass returns for one chunk of the brief.
type Pass1Output struct {
	Phases    []PhaseOut    `json:"phases"`
	Overview  string        `json:"overview"`
	Questions []QuestionOut `json:"questions"` // optional; affects are phase or task titles
}

// PhaseOut is a phase proposed by a pass.
type PhaseOut struct {
	Title     string    `json:"title"`
	Digest    string    `json:"digest"`
	Objective string    `json:"objective"`
	Tasks     []TaskOut `json:"tasks"`
}

// TaskOut is a task proposed by a pass.
type TaskOut struct {
	Title     string    `json:"title"`
	Digest    string    `json:"digest"`
	Objective string    `json:"objective"`
	Steps     []StepOut `json:"steps"`
}

// StepOut is a step proposed by a pass.
type StepOut struct {
	Title  string `json:"title"`
	Digest string `json:"digest"`
}

// ExtractJSON returns the first top-level JSON object in reply that parses,
// ignoring prose and code fences around it. Braces inside strings do not count.
// A balanced object that is not valid JSON is skipped in favor of a later valid
// one, but is returned if no valid one exists so the caller can report why it
// failed to decode. Kept for callers and its own tests; ParsePass1 and
// ParsePass2 use parseFirst.
func ExtractJSON(reply string) (string, error) {
	invalid := ""
	seen := false
	from := 0
	for {
		i := strings.IndexByte(reply[from:], '{')
		if i < 0 {
			break
		}
		start := from + i
		seen = true
		end, ok := balancedEnd(reply, start)
		if !ok {
			from = start + 1
			continue
		}
		cand := reply[start : end+1]
		if json.Valid([]byte(cand)) {
			return cand, nil
		}
		if invalid == "" {
			invalid = cand
		}
		from = end + 1
	}
	if invalid != "" {
		return invalid, nil
	}
	if !seen {
		return "", errors.New("the reply contains no JSON object")
	}
	return "", errors.New("the JSON object in the reply is not closed")
}

// balancedEnd returns the index of the brace that closes the one at start.
func balancedEnd(s string, start int) (int, bool) {
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// candidates lists every balanced top-level object in reply, in order. Objects
// nested inside a balanced one are not listed. seen reports whether any "{" was
// found at all.
func candidates(reply string) (list []string, seen bool) {
	from := 0
	for {
		i := strings.IndexByte(reply[from:], '{')
		if i < 0 {
			return list, seen
		}
		start := from + i
		seen = true
		end, ok := balancedEnd(reply, start)
		if !ok {
			from = start + 1
			continue
		}
		list = append(list, reply[start:end+1])
		from = end + 1
	}
}

// parseFirst tries decode on each candidate object in reply and returns the
// first that succeeds, so prose or a stray "{}" before the real object does not
// hide it. If none succeeds it returns the error of the longest candidate (ties
// go to the earliest), which is the one the model most likely meant.
func parseFirst[T any](reply string, decode func(raw string) (T, error)) (T, error) {
	var zero T
	list, seen := candidates(reply)
	var bestErr error
	bestLen := -1
	for _, raw := range list {
		v, err := decode(raw)
		if err == nil {
			return v, nil
		}
		if len(raw) > bestLen {
			bestLen, bestErr = len(raw), err
		}
	}
	if bestErr != nil {
		return zero, bestErr
	}
	if !seen {
		return zero, errors.New("the reply contains no JSON object")
	}
	return zero, errors.New("the JSON object in the reply is not closed")
}

// ParsePass1 finds, strictly decodes and validates a skeleton pass reply. Its
// errors are specific enough to send back to the model as a correction.
func ParsePass1(reply string) (Pass1Output, error) {
	return parseFirst(reply, decodePass1)
}

func decodePass1(raw string) (Pass1Output, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	var out Pass1Output
	if err := dec.Decode(&out); err != nil {
		return Pass1Output{}, fmt.Errorf("the JSON does not match the schema: %s", cutBytes(err.Error(), 400))
	}
	if err := validatePass1(out); err != nil {
		return Pass1Output{}, err
	}
	return out, nil
}

// NormalizeTitle is the key used to match a proposed node to an existing one.
func NormalizeTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// oneLine collapses all whitespace, including newlines, to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// maxClipRunes bounds a title quoted in an error message.
const maxClipRunes = 80

// clip makes a model-supplied title safe to quote in an error that goes back
// into a prompt: one line, at most maxClipRunes runes.
func clip(s string) string {
	s = oneLine(s)
	if utf8.RuneCountInString(s) <= maxClipRunes {
		return s
	}
	n := 0
	for i := range s {
		if n == maxClipRunes {
			return s[:i] + "..."
		}
		n++
	}
	return s
}

func validatePass1(o Pass1Output) error {
	if strings.TrimSpace(o.Overview) == "" {
		return errors.New(`"overview" must be a non-empty string`)
	}
	if err := validateQuestionOuts(o.Questions, nil); err != nil {
		return err
	}
	if len(o.Phases) > maxPhasesPerPass {
		return fmt.Errorf("too many phases (%d, at most %d)", len(o.Phases), maxPhasesPerPass)
	}
	seenPhase := map[string]bool{}
	for _, p := range o.Phases {
		where := fmt.Sprintf("phase %q", clip(p.Title))
		if err := checkNode(where, p.Title, p.Digest, p.Objective, seenPhase); err != nil {
			return err
		}
		if len(p.Tasks) > maxTasksPerPhase {
			return fmt.Errorf("%s has too many tasks (%d, at most %d)", where, len(p.Tasks), maxTasksPerPhase)
		}
		seenTask := map[string]bool{}
		for _, t := range p.Tasks {
			twhere := fmt.Sprintf("task %q in %s", clip(t.Title), where)
			if err := checkNode(twhere, t.Title, t.Digest, t.Objective, seenTask); err != nil {
				return err
			}
			if len(t.Steps) > maxStepsPerTask {
				return fmt.Errorf("%s has too many steps (%d, at most %d)", twhere, len(t.Steps), maxStepsPerTask)
			}
			seenStep := map[string]bool{}
			for _, s := range t.Steps {
				swhere := fmt.Sprintf("step %q in %s", clip(s.Title), twhere)
				if err := checkNode(swhere, s.Title, s.Digest, "", seenStep); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkNode validates one proposed node and records its title among siblings.
func checkNode(where, title, digest, objective string, siblings map[string]bool) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("%s: title is empty", where)
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		return fmt.Errorf("%s: title is longer than %d characters", where, maxTitleRunes)
	}
	if strings.TrimSpace(digest) == "" {
		return fmt.Errorf("%s: digest is empty (one or two sentences on why it exists)", where)
	}
	if utf8.RuneCountInString(digest) > maxDigestRunes {
		return fmt.Errorf("%s: digest is longer than %d characters", where, maxDigestRunes)
	}
	if utf8.RuneCountInString(objective) > maxObjective {
		return fmt.Errorf("%s: objective is longer than %d characters", where, maxObjective)
	}
	key := NormalizeTitle(title)
	if siblings[key] {
		return fmt.Errorf("%s: duplicate sibling title", where)
	}
	siblings[key] = true
	return nil
}
```

Replace the whole of `plantree/plan/prompt.go`:

```go
package plan

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

// outlineCapBytes bounds the list of existing phases and tasks in a prompt.
const outlineCapBytes = 4000

// Outline lists the phases and tasks already in the tree, titles only, so a
// pass can attach to them instead of repeating them. It stops at
// outlineCapBytes and says how many entries it left out.
func Outline(repo *plantree.Repo) (string, error) {
	var b strings.Builder
	omitted := 0
	err := repo.Walk(func(n plantree.Node) error {
		var line string
		switch n.Kind() {
		case plantree.KindPhase:
			line = "- Phase: " + oneLine(n.Title) + "\n"
		case plantree.KindTask:
			line = "  - Task: " + oneLine(n.Title) + "\n"
		default:
			return nil
		}
		if b.Len()+len(line) > outlineCapBytes {
			omitted++
			return nil
		}
		b.WriteString(line)
		return nil
	})
	if err != nil {
		return "", err
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  (%d more phases and tasks not shown)\n", omitted)
	}
	return b.String(), nil
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none yet)"
	}
	return strings.TrimRight(s, "\n")
}

// Pass1Prompt builds the prompt for one skeleton pass. It carries the running
// overview, the outline of what already exists, and exactly one chunk of the
// brief, and nothing from any earlier conversation.
func Pass1Prompt(project, overview, outline string, c Chunk, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are breaking a project brief into a plan for %q. You see ONE part of the brief (part %d of %d), not all of it.\n\n", project, c.Index+1, total)
	b.WriteString("Running overview of the whole brief so far:\n")
	b.WriteString(orNone(overview))
	b.WriteString("\n\nPhases and tasks already in the plan. To add to one, reuse its title exactly. Never repeat an existing item:\n")
	b.WriteString(orNone(outline))
	title := ""
	if strings.TrimSpace(c.Title) != "" {
		title = fmt.Sprintf(" (section: %s)", oneLine(c.Title))
	}
	fmt.Fprintf(&b, "\n\nThis part of the brief%s:\n<<<BRIEF PART\n%s\nBRIEF PART>>>\n\n", title, strings.TrimRight(c.Text, "\n"))
	b.WriteString("Rules:\n")
	b.WriteString("- A phase groups related work. A task is a unit of work one agent can own. A step is the smallest independently verifiable piece, roughly one file change or one command with a check.\n")
	b.WriteString("- Every phase, task and step needs a digest: one or two sentences saying why it exists relative to its parent, understandable without reading the parent.\n")
	b.WriteString("- Add only what this part of the brief supports. Do not invent scope. If this part adds nothing new, return an empty phases list.\n")
	b.WriteString("- Every task needs an objective and at least one step, even if this part of the brief only outlines it.\n")
	fmt.Fprintf(&b, "- Rewrite the overview so it covers the whole brief so far, in under %d characters. Keep decisions, constraints and non-goals; drop detail that the plan itself now holds.\n", OverviewCapBytes)
	b.WriteString("- Ask a question only when something in this part of the brief is genuinely ambiguous and the answer changes the plan. Never ask what the brief already answers. At most 5 questions, each with 2 to 8 options (or none for a free-text question), \"recommended\" (option labels) if you have a recommendation, and \"affects\" naming the phase or task titles the answer changes. If nothing is ambiguous, return an empty questions array.\n")
	b.WriteString("- Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"phases":[{"title":"","digest":"","objective":"","tasks":[{"title":"","digest":"","objective":"","steps":[{"title":"","digest":""}]}]}],"overview":"","questions":[{"question":"","why":"","options":[{"label":"","description":""}],"multi_select":false,"recommended":[],"rationale":"","affects":[]}]}`)
	b.WriteString("\n")
	return b.String()
}

// Bounds on the text a retry prompt adds to the original prompt.
const (
	retryReplyExcerptBytes = 1500
	retryProblemBytes      = 600
)

// cutBytes shortens s to at most max bytes at a rune boundary, adding "..." if
// it cut anything.
func cutBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// RetryPrompt asks the model to correct a reply that was rejected. It quotes
// the start of the rejected reply so the model can see what it got wrong; the
// text it adds to original is bounded whatever the reply's size.
func RetryPrompt(original, reply, problem string) string {
	return original + "\n\nYour previous reply was rejected: " + cutBytes(problem, retryProblemBytes) +
		"\nYour previous reply began:\n" + cutBytes(reply, retryReplyExcerptBytes) +
		"\nReply again with ONE JSON object only, fixing that problem."
}

// CompressPrompt asks the model to shorten an overview that grew past its cap.
func CompressPrompt(overview string, cap int) string {
	return fmt.Sprintf("Compress this project overview to under %d characters. Keep decisions, constraints, non-goals and the shape of the plan; drop detail. Reply with the compressed overview text only.\n\n%s\n", cap, strings.TrimRight(overview, "\n"))
}
```

Replace the whole of `plantree/plan/runner.go`:

```go
package plan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// Completer runs one prompt and returns the model's reply. Every call must
// start from a fresh context: no history from any earlier call. ClientCompleter
// is the production implementation.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// ErrBriefChanged is returned when a run is resumed against a brief or chunk
// size different from the one it started with. The cursor is only valid against
// identical chunk boundaries (same brief and same chunk size). Resuming against
// a different brief or chunk size would mix two plans.
var ErrBriefChanged = errors.New("plan: the brief or chunk size changed since this run started")

// Options tunes RunPass1. Zero values pick the defaults.
type Options struct {
	ProjectName string
	// ChunkBytes is the most brief text one pass reads. One pass costs about
	// ChunkBytes + OverviewCap + 4000 (outline) + 1000 (instructions) bytes; at
	// 3 to 4 bytes per token that must leave room for the reply inside the
	// model's window, so choose ChunkBytes at most (window_tokens * 3) - 11000.
	// The caller that knows the model derives it; RunPass1 does not.
	ChunkBytes  int // default DefaultChunkBytes
	OverviewCap int // default OverviewCapBytes
}

// Result summarizes one RunPass1 call.
type Result struct {
	Chunks    int     // chunks in the whole brief
	Processed int     // chunks processed by this call
	Created   Created // nodes added by this call
	Questions int     // new questions asked by this call
}

const (
	briefFile = "brief.md"
	stateFile = "pass1.json"
)

// pass1State is the resume cursor for a skeleton run. It is only a cursor:
// the tree and overview hold the real state, and a chunk that was merged but
// not yet recorded here is simply merged again, which changes nothing. The cursor
// is only valid against identical chunk boundaries (same brief and same chunk size).
type pass1State struct {
	BriefSHA256 string `json:"brief_sha256"`
	Chunks      int    `json:"chunks"`
	ChunkBytes  int    `json:"chunk_bytes"`
	Next        int    `json:"next"`
}

func statePath(repo *plantree.Repo) string {
	return filepath.Join(repo.Dir(), "_state", stateFile)
}

func loadState(repo *plantree.Repo) (pass1State, bool, error) {
	b, err := os.ReadFile(statePath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return pass1State{}, false, nil
	}
	if err != nil {
		return pass1State{}, false, err
	}
	var s pass1State
	if err := json.Unmarshal(b, &s); err != nil {
		return pass1State{}, false, fmt.Errorf("plan: reading %s (delete that file to restart the run): %w", statePath(repo), err)
	}
	return s, true, nil
}

func saveState(repo *plantree.Repo, s pass1State) error {
	if err := os.MkdirAll(filepath.Dir(statePath(repo)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return lockfile.WriteAtomic(statePath(repo), append(b, '\n'), 0o644)
}

// RunPass1 reads the brief in bounded chunks, one fresh-context pass per
// chunk, and builds the skeleton tree and running overview. It can be called
// again after any error and continues from the first unprocessed chunk.
func RunPass1(ctx context.Context, repo *plantree.Repo, brief string, c Completer, opt Options) (Result, error) {
	if opt.ProjectName == "" {
		opt.ProjectName = "project"
	}
	if opt.ChunkBytes < 1 {
		opt.ChunkBytes = DefaultChunkBytes
	}
	if opt.OverviewCap < 1 {
		opt.OverviewCap = OverviewCapBytes
	}
	chunks := SplitBrief(brief, opt.ChunkBytes)
	if len(chunks) == 0 {
		return Result{}, errors.New("plan: the brief is empty")
	}
	sum := sha256.Sum256([]byte(brief))
	digest := hex.EncodeToString(sum[:])

	if err := ensureRoot(repo, opt.ProjectName); err != nil {
		return Result{}, err
	}
	state, found, err := loadState(repo)
	if err != nil {
		return Result{}, err
	}
	if found && (state.BriefSHA256 != digest || state.Chunks != len(chunks) || state.ChunkBytes != opt.ChunkBytes) {
		return Result{}, ErrBriefChanged
	}
	if found && (state.Next < 0 || state.Next > len(chunks)) {
		return Result{}, fmt.Errorf("plan: resume cursor %d is outside 0..%d in %s; delete that file to restart the run", state.Next, len(chunks), statePath(repo))
	}
	if found && state.Next > 0 {
		// A cursor with no tree behind it means the tree was lost. Merge is
		// idempotent, so redoing the chunks is safe. The only false positive is
		// a brief whose every chunk returned no phases, which costs model calls
		// and nothing else.
		kids, err := repo.Children(plantree.RootID)
		if err != nil {
			return Result{}, err
		}
		if len(kids) == 0 {
			state.Next = 0
			if err := saveState(repo, state); err != nil {
				return Result{}, err
			}
		}
	}
	if !found {
		state = pass1State{BriefSHA256: digest, Chunks: len(chunks), ChunkBytes: opt.ChunkBytes}
		if err := saveBrief(repo, brief); err != nil {
			return Result{}, err
		}
		if err := saveState(repo, state); err != nil {
			return Result{}, err
		}
	}

	res := Result{Chunks: len(chunks)}
	for i := state.Next; i < len(chunks); i++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		cr, err := runChunk(ctx, repo, c, opt, chunks[i], len(chunks))
		if err != nil {
			hint := ""
			if _, ok := llm.ContextLimitFromError(err); ok {
				hint = " (the server's context window is smaller than one pass needs: lower Options.ChunkBytes)"
			}
			return res, fmt.Errorf("plan: chunk %d of %d: %w%s", i+1, len(chunks), err, hint)
		}
		res.Created.add(cr.created)
		res.Questions += cr.questions
		res.Processed++
		state.Next = i + 1
		if err := saveState(repo, state); err != nil {
			return res, err
		}
	}
	return res, nil
}

func ensureRoot(repo *plantree.Repo, name string) error {
	if _, err := repo.Get(plantree.RootID); err == nil {
		return nil
	} else if !errors.Is(err, plantree.ErrNotFound) {
		return err
	}
	return repo.Init(plantree.Node{
		SchemaVersion: plantree.SchemaVersion,
		ID:            plantree.RootID,
		Title:         name,
		NodeRevision:  1,
		ContextDigest: "The plan for " + name + ".",
		DependsOn:     []string{},
		Planning:      plantree.Planning{Stage: plantree.StageSkeleton},
	})
}

func saveBrief(repo *plantree.Repo, brief string) error {
	if err := os.MkdirAll(repo.Dir(), 0o755); err != nil {
		return err
	}
	return lockfile.WriteAtomic(filepath.Join(repo.Dir(), briefFile), []byte(brief), 0o644)
}

// askJSON sends prompt and parses the reply. If parsing fails it asks once more,
// showing the model the reason and an excerpt of the reply it got wrong.
// Transport errors are returned as they are so a later resume can retry them.
func askJSON[T any](ctx context.Context, c Completer, prompt string, parse func(reply string) (T, error)) (T, error) {
	var zero T
	reply, err := c.Complete(ctx, prompt)
	if err != nil {
		return zero, err
	}
	v, perr := parse(reply)
	if perr == nil {
		return v, nil
	}
	reply, err = c.Complete(ctx, RetryPrompt(prompt, reply, perr.Error()))
	if err != nil {
		return zero, err
	}
	v, err = parse(reply)
	if err != nil {
		return zero, fmt.Errorf("the model's reply was rejected twice: %w", err)
	}
	return v, nil
}

// chunkResult is what one skeleton pass added.
type chunkResult struct {
	created   Created
	questions int
}

// runChunk performs one skeleton pass: build the prompt, ask, merge, record
// which nodes the chunk produced, record the questions it asked, refresh the
// overview.
func runChunk(ctx context.Context, repo *plantree.Repo, c Completer, opt Options, chunk Chunk, total int) (chunkResult, error) {
	overview, err := ReadOverview(repo.Dir())
	if err != nil {
		return chunkResult{}, err
	}
	outline, err := Outline(repo)
	if err != nil {
		return chunkResult{}, err
	}
	prompt := Pass1Prompt(opt.ProjectName, overview, outline, chunk, total)

	out, err := askJSON(ctx, c, prompt, ParsePass1)
	if err != nil {
		return chunkResult{}, err
	}

	created, touched, err := mergeTracked(repo, out)
	if err != nil {
		return chunkResult{created: created}, err
	}
	if err := recordProvenance(repo, chunk.Index, touched); err != nil {
		return chunkResult{created: created}, err
	}
	asked, err := applyPass1Questions(repo, chunk, out, touched)
	if err != nil {
		return chunkResult{created: created, questions: asked}, err
	}
	text := out.Overview
	if len(text) > opt.OverviewCap {
		if shorter, cerr := c.Complete(ctx, CompressPrompt(FitOverview(text, 4*opt.OverviewCap), opt.OverviewCap)); cerr == nil && shorter != "" {
			text = shorter
		}
		text = FitOverview(text, opt.OverviewCap)
	}
	if err := WriteOverview(repo.Dir(), text); err != nil {
		return chunkResult{created: created, questions: asked}, err
	}
	return chunkResult{created: created, questions: asked}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/questions_apply.go gophermind-lib/plantree/plan/questions_apply_test.go gophermind-lib/plantree/plan/pass1json.go gophermind-lib/plantree/plan/pass1json_test.go gophermind-lib/plantree/plan/prompt.go gophermind-lib/plantree/plan/overview_test.go gophermind-lib/plantree/plan/runner.go
git commit -m "feat(plan): pass 1 asks questions and holds the steps they affect

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 5: Pass 2 asks instead of guessing and sees the decisions made

**Files:**
- Create: `gophermind-lib/plantree/plan/decisions.go`, `decisions_test.go`
- Replace: `pass2json.go`, `pass2json_test.go`, `prompt2.go`, `prompt2_test.go`, `runner2.go`, `runner2_test.go`, `questions_apply.go` (same directory)

**Interfaces:**
- Consumes: everything from Tasks 1 to 4.
- Produces:
  - `Pass2Output.Questions []QuestionOut`; `ParsePass2` accepts a reply that leaves a step out of `steps` if a question names it in `affects`, rejects a step that is both specified and asked about, and rejects an `affects` entry that is not in the batch
  - `func decisionsFor(qs []Question, ids []string) string` (answered questions that affect any of `ids`, at most 6 lines of 300 bytes, in the order asked)
  - `Pass2Input.Decisions`; the prompt gains a "Decisions already made" block, the question rules and `"questions":[]` in the shape; a step waiting for an answer is tagged `[waiting for an answer]` in the step list
  - `func recordPass2Questions(repo, taskID string, out Pass2Output) (int, error)`, `func markAsked(steps []plantree.Node, out Pass2Output)` (in `questions_apply.go`)
  - `Result2.Questions int`; `Result2.Steps` now counts the steps actually specified
  - defaults: `defaultBriefBytes` 4000, `siblingListCapBytes` 3000; the worst-case tests include a maximal decisions block and pin 27,000 bytes

Pass 2 records its questions and holds the steps they name before it writes the specifications of the others.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/decisions_test.go`:

```go
package plan

import (
	"strings"
	"testing"
)

func answered(question string, affects []string, ids []string, text string) Question {
	return Question{
		ID: "q-001", Question: question, Status: QuestionAnswered, Affects: affects,
		Options: []Option{{ID: "opt-1", Label: "SQLite"}, {ID: "opt-2", Label: "Postgres"}},
		Answer:  &Answer{OptionIDs: ids, Text: text},
	}
}

func TestDecisionsForShowsOnlyAnsweredQuestionsThatAffectTheNodes(t *testing.T) {
	open := answered("Open one?", []string{"phase-001.task-001"}, nil, "")
	open.Status, open.Answer = QuestionOpen, nil
	qs := []Question{
		answered("Which database?", []string{"phase-001.task-001"}, []string{"opt-2"}, ""),
		answered("Elsewhere?", []string{"phase-009"}, []string{"opt-1"}, ""),
		open,
		answered("Deadline?", []string{"phase-001"}, nil, "end of the quarter"),
		answered("Both?", []string{"phase-001.task-001.step-001"}, []string{"opt-1", "opt-2"}, "keep both in sync"),
	}
	got := decisionsFor(qs, []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001"})
	want := "- Which database? -> Postgres\n- Deadline? -> end of the quarter\n- Both? -> SQLite; Postgres (note: keep both in sync)"
	if got != want {
		t.Errorf("decisionsFor =\n%q\nwant\n%q", got, want)
	}
	if got := decisionsFor(qs, []string{"phase-777"}); got != "" {
		t.Errorf("nothing affects phase-777, got %q", got)
	}
	if got := decisionsFor(nil, []string{"phase-001"}); got != "" {
		t.Errorf("no questions: %q", got)
	}
}

func TestDecisionsForIsBounded(t *testing.T) {
	var qs []Question
	for i := 0; i < maxDecisions+3; i++ {
		qs = append(qs, answered(strings.Repeat("q", 900), []string{"phase-001"}, []string{"opt-1"}, strings.Repeat("n", 900)))
	}
	got := decisionsFor(qs, []string{"phase-001"})
	lines := strings.Split(got, "\n")
	if len(lines) != maxDecisions+1 || !strings.Contains(lines[len(lines)-1], "3 more decisions not shown") {
		t.Fatalf("%d lines, last %q", len(lines), lines[len(lines)-1])
	}
	for _, l := range lines[:maxDecisions] {
		if len(l) > decisionLineBytes+3+3 { // "- " prefix and the cut marker
			t.Errorf("a decision line is %d bytes, cap %d", len(l), decisionLineBytes)
		}
	}
}
```

Replace the whole of `plantree/plan/pass2json_test.go`:

```go
package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	s1 = "phase-001.task-001.step-001"
	s2 = "phase-001.task-001.step-002"
	s3 = "phase-001.task-001.step-003"
)

var (
	batch23  = []string{s2, s3}
	siblings = []string{s1, s2, s3}
)

func goodSpec(id string) StepSpecOut {
	return StepSpecOut{
		ID:                 id,
		Description:        "build " + id,
		TargetPaths:        []string{"internal/x/x.go"},
		AcceptanceCriteria: []string{"it compiles"},
		TestCommand:        []string{"go", "test", "./..."},
		DependsOn:          []string{},
	}
}

func replyOf(t *testing.T, steps ...StepSpecOut) string {
	t.Helper()
	b, err := json.Marshal(Pass2Output{Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParsePass2Accepts(t *testing.T) {
	a, b := goodSpec(s2), goodSpec(s3)
	b.DependsOn = []string{s1, s2}
	out, err := ParsePass2("Here you go:\n```json\n"+replyOf(t, a, b)+"\n```", batch23, siblings)
	if err != nil || len(out.Steps) != 2 || out.Steps[1].DependsOn[1] != s2 {
		t.Fatalf("ParsePass2 = %+v, %v", out, err)
	}
	out, err = ParsePass2("I checked {} first. "+replyOf(t, a, goodSpec(s3)), batch23, siblings)
	if err != nil || len(out.Steps) != 2 {
		t.Errorf("an earlier object that is not the reply must be skipped: %+v, %v", out, err)
	}
	empty := goodSpec(s2)
	empty.TargetPaths, empty.TestCommand = nil, nil
	if _, err := ParsePass2(replyOf(t, empty, goodSpec(s3)), batch23, siblings); err != nil {
		t.Errorf("no target paths and no test command are allowed: %v", err)
	}
}

func TestParsePass2Rejects(t *testing.T) {
	bad := func(f func(*StepSpecOut)) string {
		a := goodSpec(s2)
		f(&a)
		return replyOf(t, a, goodSpec(s3))
	}
	cases := map[string]struct {
		reply string
		want  string
	}{
		"missing step":      {replyOf(t, goodSpec(s2)), "missing from the reply"},
		"extra step":        {replyOf(t, goodSpec(s2), goodSpec(s3), goodSpec(s1)), "not one of the steps asked for"},
		"duplicate step":    {replyOf(t, goodSpec(s2), goodSpec(s2), goodSpec(s3)), "more than once"},
		"empty description": {bad(func(s *StepSpecOut) { s.Description = " " }), "description is empty"},
		"long description":  {bad(func(s *StepSpecOut) { s.Description = strings.Repeat("d", maxDescRunes+1) }), "description is longer"},
		"no criteria":       {bad(func(s *StepSpecOut) { s.AcceptanceCriteria = nil }), "acceptance_criteria needs"},
		"too many criteria": {bad(func(s *StepSpecOut) {
			s.AcceptanceCriteria = make([]string, maxCriteria+1)
			for i := range s.AcceptanceCriteria {
				s.AcceptanceCriteria[i] = "c"
			}
		}), "too many acceptance criteria"},
		"empty criterion":   {bad(func(s *StepSpecOut) { s.AcceptanceCriteria = []string{"ok", ""} }), "criterion is empty"},
		"long criterion":    {bad(func(s *StepSpecOut) { s.AcceptanceCriteria = []string{strings.Repeat("c", maxCriterionRunes+1)} }), "criterion is longer"},
		"absolute path":     {bad(func(s *StepSpecOut) { s.TargetPaths = []string{"/etc/passwd"} }), "not absolute"},
		"backslash root":    {bad(func(s *StepSpecOut) { s.TargetPaths = []string{`\windows`} }), "not absolute"},
		"drive path":        {bad(func(s *StepSpecOut) { s.TargetPaths = []string{`C:\x`} }), "not drive-qualified"},
		"dotdot":            {bad(func(s *StepSpecOut) { s.TargetPaths = []string{"a/../../b"} }), "must not contain"},
		"control character": {bad(func(s *StepSpecOut) { s.TargetPaths = []string{"a\nb"} }), "control character"},
		"empty path":        {bad(func(s *StepSpecOut) { s.TargetPaths = []string{""} }), "is empty"},
		"too many paths": {bad(func(s *StepSpecOut) {
			s.TargetPaths = make([]string, maxTargetPaths+1)
			for i := range s.TargetPaths {
				s.TargetPaths[i] = "p"
			}
		}), "too many target_paths"},
		"empty command arg": {bad(func(s *StepSpecOut) { s.TestCommand = []string{"go", ""} }), "empty argument"},
		"too many command": {bad(func(s *StepSpecOut) {
			s.TestCommand = make([]string, maxCommandArgs+1)
			for i := range s.TestCommand {
				s.TestCommand[i] = "a"
			}
		}), "too many arguments"},
		"unknown dependency":   {bad(func(s *StepSpecOut) { s.DependsOn = []string{"phase-009.task-001.step-001"} }), "not a step of this task"},
		"self dependency":      {bad(func(s *StepSpecOut) { s.DependsOn = []string{s2} }), "earlier step"},
		"later dependency":     {bad(func(s *StepSpecOut) { s.DependsOn = []string{s3} }), "earlier step"},
		"duplicate dependency": {bad(func(s *StepSpecOut) { s.DependsOn = []string{s1, s1} }), "twice"},
		"unknown field":        {`{"steps":[],"extra":1}`, "does not match the schema"},
		"not json":             {"no braces here", "no JSON object"},
	}
	for name, c := range cases {
		_, err := ParsePass2(c.reply, batch23, siblings)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, c.want)
		}
	}
}

func TestParsePass2ErrorsStayBoundedForHugeValues(t *testing.T) {
	huge := strings.Repeat("x", 5_000_000)
	a := goodSpec(s2)
	a.TargetPaths = []string{"/" + huge}
	_, err := ParsePass2(replyOf(t, a, goodSpec(s3)), batch23, siblings)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("error length = %d, want a bounded error", len(err.Error()))
	}
	_, err = ParsePass2(replyOf(t, goodSpec(huge), goodSpec(s3)), batch23, siblings)
	if err == nil || len(err.Error()) > 500 {
		t.Errorf("huge step id: error length = %d", len(err.Error()))
	}
}

func TestParsePass2BoundsTheSchemaError(t *testing.T) {
	reply := `{"steps":[],"` + strings.Repeat("k", 200000) + `":1}`
	_, err := ParsePass2(reply, []string{"s"}, []string{"s"})
	if err == nil || !strings.Contains(err.Error(), "does not match the schema") {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) >= 700 {
		t.Errorf("error is %d bytes", len(err.Error()))
	}
}

func askAbout(ids ...string) QuestionOut {
	q := goodQ()
	q.Affects = ids
	return q
}

func replyWithQuestions(t *testing.T, steps []StepSpecOut, qs ...QuestionOut) string {
	t.Helper()
	b, err := json.Marshal(Pass2Output{Steps: steps, Questions: qs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParsePass2AcceptsAQuestionInPlaceOfAStep(t *testing.T) {
	reply := replyWithQuestions(t, []StepSpecOut{goodSpec(s2)}, askAbout(s3))
	out, err := ParsePass2(reply, batch23, siblings)
	if err != nil || len(out.Steps) != 1 || len(out.Questions) != 1 || out.Questions[0].Affects[0] != s3 {
		t.Fatalf("ParsePass2 = %+v, %v", out, err)
	}
	// Every step of the batch may be asked about instead of specified.
	all := replyWithQuestions(t, nil, askAbout(s2, s3))
	if _, err := ParsePass2(all, batch23, siblings); err != nil {
		t.Errorf("a reply that only asks must be accepted when it names every step: %v", err)
	}
}

func TestParsePass2QuestionRules(t *testing.T) {
	cases := map[string]struct {
		reply string
		want  string
	}{
		"specified and asked":   {replyWithQuestions(t, []StepSpecOut{goodSpec(s2), goodSpec(s3)}, askAbout(s3)), "both specified and named in a question"},
		"asks about a stranger": {replyWithQuestions(t, []StepSpecOut{goodSpec(s2), goodSpec(s3)}, askAbout(s1)), "not one of the steps asked for"},
		"still missing a step":  {replyWithQuestions(t, []StepSpecOut{goodSpec(s2)}, askAbout(s1)), "not one of the steps asked for"},
		"one step left over":    {replyWithQuestions(t, nil, askAbout(s2)), "missing from the reply"},
		"a bad question":        {replyWithQuestions(t, []StepSpecOut{goodSpec(s2)}, QuestionOut{Question: "", Affects: []string{s3}}), "a question is empty"},
	}
	for name, c := range cases {
		_, err := ParsePass2(c.reply, batch23, siblings)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, c.want)
		}
	}
	if _, err := ParsePass2(replyOf(t, goodSpec(s2)), batch23, siblings); err == nil || !strings.Contains(err.Error(), "ask a question that names it") {
		t.Errorf("the missing-step error must tell the model it can ask: %v", err)
	}
}
```

Replace the whole of `plantree/plan/prompt2_test.go`:

```go
package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/plantree"
)

func node(t *testing.T, id, title, digest, objective string) plantree.Node {
	t.Helper()
	n, err := newSkeleton(id, title, digest, objective)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPass2PromptCarriesOneTaskAndItsContext(t *testing.T) {
	phase := node(t, "phase-001", "Foundation", "the base of everything", "set up the base")
	task := node(t, "phase-001.task-001", "Repo layout", "where code lives", "")
	st1 := node(t, s1, "Create module", "needed to compile", "")
	st2 := node(t, s2, "Add CI", "catch breakage", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "an overview", Excerpts: "[part 1 of 1]\nthe brief text", Phase: phase, Task: task, Siblings: []plantree.Node{st1, st2}, Batch: []plantree.Node{st2}})
	for _, want := range []string{
		`"demo"`, "an overview", "Phase: Foundation", "the base of everything", "Task: Repo layout", "where code lives",
		"- " + s1 + ": Create module", "Steps to specify now:", "- " + s2 + ": Add CI. Why: catch breakage",
		"<<<BRIEF EXCERPTS", "the brief text", "EARLIER step", `"depends_on"`, "ONE JSON object",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	section := p[strings.Index(p, "Steps to specify now:"):strings.Index(p, "Brief excerpts")]
	if strings.Contains(section, s1) {
		t.Error("the steps to specify now must list only the batch")
	}
}

func TestPass2PromptWithoutExcerptsSaysSo(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}})
	if !strings.Contains(p, "(not available)") || strings.Contains(p, "<<<BRIEF EXCERPTS") {
		t.Errorf("no excerpts: %q", p[strings.Index(p, "Brief excerpts"):])
	}
}

func TestPass2PromptBoundsTheStepList(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("a long step title ", 10), "d", ""))
	}
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: steps, Batch: steps[:1]})
	if !strings.Contains(p, "more steps not shown") {
		t.Error("an oversize step list must say how many steps it left out")
	}
	list := p[strings.Index(p, "in order."):strings.Index(p, "Steps to specify now:")]
	if len(list) > siblingListCapBytes+200 {
		t.Errorf("the step list is %d bytes, cap %d", len(list), siblingListCapBytes)
	}
}

// TestPass2PromptWorstCaseSize pins the largest prompt one pass can produce
// with the default caps, so a change that lets it grow shows up here.
// worstDecisions is the largest decisions block a prompt can carry: more
// answered questions than fit, each with a long question and long answer.
func worstDecisions(unit string) string {
	var qs []Question
	for i := 0; i < maxDecisions+2; i++ {
		qs = append(qs, Question{
			ID: "q", Question: strings.Repeat(unit, 600), Status: QuestionAnswered,
			Options: []Option{{ID: "opt-1", Label: strings.Repeat(unit, 100)}},
			Answer:  &Answer{OptionIDs: []string{"opt-1"}, Text: strings.Repeat(unit, 600)},
			Affects: []string{"phase-001.task-001"},
		})
	}
	return decisionsFor(qs, []string{"phase-001.task-001"})
}

func TestPass2PromptWorstCaseSize(t *testing.T) {
	phase := node(t, "phase-001", strings.Repeat("p", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	task := node(t, "phase-001.task-001", strings.Repeat("t", 200), strings.Repeat("d", 500), strings.Repeat("o", 1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), strings.Repeat("s", 200), strings.Repeat("d", 500), ""))
	}
	chunks := []Chunk{{Index: 0, Text: strings.Repeat("brief text line\n", 2000)}}
	excerpts := Excerpts(chunks, []int{0}, defaultBriefBytes)
	overview := FitOverview(strings.Repeat("o", 20000), OverviewCapBytes)
	p := Pass2Prompt(Pass2Input{Project: strings.Repeat("n", 100), Overview: overview, Facts: strings.Repeat("f", FactsCapBytes), Decisions: worstDecisions("q"), Excerpts: excerpts, Phase: phase, Task: task, Siblings: steps, Batch: steps[:defaultStepsPerPass]})
	t.Logf("worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 27000 {
		t.Errorf("worst-case pass-2 prompt is %d bytes, want at most 27000", len(p))
	}
}

func TestPass2PromptWorstCaseSizeWithMultibyteText(t *testing.T) {
	r := func(n int) string { return strings.Repeat("\U0001D11E", n) }
	phase := node(t, "phase-001", r(200), r(500), r(1000))
	task := node(t, "phase-001.task-001", r(200), r(500), r(1000))
	var steps []plantree.Node
	for i := 1; i <= 100; i++ {
		steps = append(steps, node(t, fmt.Sprintf("phase-001.task-001.step-%03d", i), r(200), r(500), ""))
	}
	chunks := []Chunk{{Index: 0, Text: strings.Repeat("brief text line\n", 2000)}}
	excerpts := Excerpts(chunks, []int{0}, defaultBriefBytes)
	overview := FitOverview(strings.Repeat("o", 20000), OverviewCapBytes)
	p := Pass2Prompt(Pass2Input{Project: r(100), Overview: overview, Facts: r(FactsCapBytes / 4), Decisions: worstDecisions("\U0001D11E"), Excerpts: excerpts, Phase: phase, Task: task, Siblings: steps, Batch: steps[:defaultStepsPerPass]})
	t.Logf("multibyte worst-case pass-2 prompt: %d bytes", len(p))
	if len(p) > 27000 {
		t.Errorf("multibyte worst-case pass-2 prompt is %d bytes, want at most 27000", len(p))
	}
	if !utf8.ValidString(p) {
		t.Error("the prompt is not valid UTF-8")
	}
}

func TestPass2PromptTellsTheModelWhatTheParserEnforces(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}})
	for _, want := range []string{"2000 characters", "300 characters", "never put an empty string", "Do not depend on a step marked on hold"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if strings.Contains(p, `[""]`) {
		t.Error("the shape example must not contain an empty string")
	}
	found := false
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(line, `{"steps"`) {
			found = true
			if !json.Valid([]byte(line)) {
				t.Errorf("the shape line is not valid JSON: %s", line)
			}
		}
	}
	if !found {
		t.Error("no shape line found")
	}
}

func TestPass2PromptTagsEachSiblingByStageAndHold(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	skipped := node(t, "phase-001.task-001.step-001", "One", "d", "")
	skipped.Status = plantree.StatusSkipped
	drafted := node(t, "phase-001.task-001.step-002", "Two", "d", "")
	drafted.Planning.Stage = plantree.StageDrafted
	skel := node(t, "phase-001.task-001.step-003", "Three", "d", "")
	p := Pass2Prompt(Pass2Input{Project: "demo", Overview: "", Excerpts: "", Phase: phase, Task: task, Siblings: []plantree.Node{skipped, drafted, skel}, Batch: []plantree.Node{skel}})
	for _, want := range []string{
		"step-001: One [on hold: skipped]", "step-002: Two [specified]", "step-003: Three [to specify]",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}

func TestPass2PromptShowsFactsOrSaysNone(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	base := Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}}

	without := Pass2Prompt(base)
	if !strings.Contains(without, "Repository facts") || !strings.Contains(without, "(not provided)") {
		t.Error("with no facts the prompt must say so")
	}
	if !strings.Contains(without, "rather than guessing") {
		t.Error("the prompt must tell the model not to guess a test command")
	}

	base.Facts = "Go 1.25. Test: go test ./..."
	with := Pass2Prompt(base)
	if !strings.Contains(with, "Go 1.25. Test: go test ./...") || strings.Contains(with, "(not provided)") {
		t.Error("the facts must appear and replace the placeholder")
	}

	base.Facts = strings.Repeat("f", 10*FactsCapBytes)
	if got := Pass2Prompt(base); len(got) > len(with)+FactsCapBytes+200 {
		t.Errorf("oversize facts must be cut to FactsCapBytes, prompt grew to %d", len(got))
	}
}

func TestPass2PromptShowsDecisionsAndTeachesQuestions(t *testing.T) {
	phase := node(t, "phase-001", "P", "d", "")
	task := node(t, "phase-001.task-001", "T", "d", "")
	st := node(t, s1, "S", "d", "")
	base := Pass2Input{Project: "demo", Phase: phase, Task: task, Siblings: []plantree.Node{st}, Batch: []plantree.Node{st}}

	none := Pass2Prompt(base)
	if !strings.Contains(none, "Decisions already made by the project owner") || !strings.Contains(none, "(none)") {
		t.Error("with no decisions the prompt must say so")
	}
	for _, want := range []string{"leave that step out of \"steps\"", "At most 5 questions", "Never ask what the decisions above", `"questions":[]`} {
		if !strings.Contains(none, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}

	base.Decisions = "- Which database? -> Postgres"
	if got := Pass2Prompt(base); !strings.Contains(got, "- Which database? -> Postgres") {
		t.Error("the decisions must appear in the prompt")
	}
}

func TestStepListMarksAStepThatWaitsForAnAnswer(t *testing.T) {
	waiting := node(t, s1, "Waits", "d", "")
	waiting.Planning.Stage = plantree.StageAwaitingAnswers
	if got := stepList([]plantree.Node{waiting}); !strings.Contains(got, "[waiting for an answer]") {
		t.Errorf("stepList = %q", got)
	}
}
```

Replace the whole of `plantree/plan/runner2_test.go`:

```go
package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

var stepIDRE = regexp.MustCompile(`phase-\d{3}\.task-\d{3}\.step-\d{3}`)

// stepsToSpecify returns the step ids listed under "Steps to specify now".
func stepsToSpecify(prompt string) []string {
	start := strings.Index(prompt, "Steps to specify now:")
	end := strings.Index(prompt, "Brief excerpts")
	if start < 0 || end < start {
		return nil
	}
	return stepIDRE.FindAllString(prompt[start:end], -1)
}

// pass2Reply answers a pass-2 prompt with a valid specification for every step
// it asks for. Step 2 of a task depends on step 1.
func pass2Reply(prompt string) string {
	var steps []StepSpecOut
	for _, id := range stepsToSpecify(prompt) {
		s := StepSpecOut{
			ID:                 id,
			Description:        "implement " + id,
			TargetPaths:        []string{"pkg/" + id[len(id)-8:] + ".go"},
			AcceptanceCriteria: []string{"it works"},
			TestCommand:        []string{"go", "test", "./..."},
			DependsOn:          []string{},
		}
		if strings.HasSuffix(id, "step-002") {
			s.DependsOn = []string{strings.TrimSuffix(id, "step-002") + "step-001"}
		}
		steps = append(steps, s)
	}
	b, _ := json.Marshal(Pass2Output{Steps: steps})
	return string(b)
}

func specFake() *fake {
	return &fake{reply: func(_ int, p string) (string, error) { return pass2Reply(p), nil }}
}

// planned runs pass 1 over threePartBrief and returns the repo and the planning
// directory it lives in, so a test can reopen it as a new process would.
func planned(t *testing.T) (*plantree.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func actionKinds(t *testing.T, r *plantree.Repo) string {
	t.Helper()
	a, err := r.NextActions()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range append(a.Runnable, a.Blocked...) {
		out = append(out, string(x.Kind)+":"+x.NodeID)
	}
	return fmt.Sprint(out)
}

func TestRunPass2SpecifiesEveryStepAndLeavesOnlyApproval(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result2{Tasks: 1, Steps: 2, Passes: 1}) {
		t.Errorf("Result2 = %+v", res)
	}
	a, _ := r.Get(s1)
	b, _ := r.Get(s2)
	if a.Planning.Stage != plantree.StageDrafted || a.Status != plantree.StatusUntouched || a.Work == nil ||
		a.Work.Description != "implement "+s1 || len(a.Work.TestCommand) != 3 || a.Work.TargetPaths[0] != "pkg/step-001.go" {
		t.Errorf("step 1 = %+v work=%+v", a, a.Work)
	}
	if len(b.DependsOn) != 1 || b.DependsOn[0] != s1 {
		t.Errorf("step 2 depends_on = %v", b.DependsOn)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if !strings.Contains(f.prompts[0], `"demo"`) {
		t.Error("the project name defaults to the root title")
	}
}

func TestRunPass2BatchesStepsOfOneTask(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	res, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passes != 2 || res.Steps != 2 || res.Tasks != 1 || len(f.prompts) != 2 {
		t.Errorf("Result2 = %+v, %d calls", res, len(f.prompts))
	}
	if got := stepsToSpecify(f.prompts[1]); len(got) != 1 || got[0] != s2 {
		t.Errorf("the second batch must be only step 2, got %v", got)
	}
}

func TestRunPass2ResumesFromTheTreeAfterAFailure(t *testing.T) {
	r, dir := planned(t)
	broken := &fake{reply: func(_ int, p string) (string, error) {
		if strings.Contains(p, "Task: T2") {
			return "", errors.New("model unavailable")
		}
		return pass2Reply(p), nil
	}}
	res, err := RunPass2(context.Background(), r, broken, Options2{})
	if err == nil || !strings.Contains(err.Error(), "task phase-001.task-002") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want it to name the task and the cause", err)
	}
	if res.Tasks != 1 || res.Steps != 1 {
		t.Errorf("Result2 = %+v", res)
	}
	first, _ := r.Get("phase-001.task-001.step-001")
	second, _ := r.Get("phase-001.task-002.step-001")
	if first.Planning.Stage != plantree.StageDrafted || second.Planning.Stage != plantree.StageSkeleton {
		t.Errorf("stages after failure: %s and %s", first.Planning.Stage, second.Planning.Stage)
	}

	good := specFake() // a new process, a new client
	res, err = RunPass2(context.Background(), plantree.Open(dir), good, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if len(good.prompts) != 2 || res.Tasks != 2 || res.Steps != 2 {
		t.Errorf("resume made %d calls, Result2 = %+v; want 2 calls for the 2 tasks left", len(good.prompts), res)
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s", got)
	}
}

func TestRunPass2ShowsEachTaskOnlyTheBriefThatProducedIt(t *testing.T) {
	r, _ := planned(t)
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 3 {
		t.Fatalf("%d calls, want 3", len(f.prompts))
	}
	// Task order: phase-001.task-001 (chunk 1), phase-001.task-002 (chunk 2), phase-002.task-001 (chunk 3).
	want := []struct{ has, not string }{
		{"first part text", "third part text"},
		{"second part text", "third part text"},
		{"third part text", "first part text"},
	}
	for i, w := range want {
		if !strings.Contains(f.prompts[i], w.has) || strings.Contains(f.prompts[i], w.not) {
			t.Errorf("prompt %d must contain %q and not %q", i, w.has, w.not)
		}
	}
	if !strings.Contains(f.prompts[0], "[part 1 of 3]") {
		t.Error("excerpts are labeled with their place in the brief")
	}
}

func TestRunPass2WithoutAStoredBriefStillWorks(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "(not available)") {
		t.Error("a plan with no stored brief must say the excerpts are not available")
	}
}

func TestRunPass2RetriesAMalformedReplyOnce(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := &fake{reply: func(n int, p string) (string, error) {
		if n == 0 {
			return "Sorry, here is prose only.", nil
		}
		return pass2Reply(p), nil
	}}
	res, err := RunPass2(context.Background(), r, f, Options2{})
	if err != nil || len(f.prompts) != 2 || res.Steps != 2 {
		t.Fatalf("err=%v calls=%d res=%+v", err, len(f.prompts), res)
	}
	if !strings.Contains(f.prompts[1], "Sorry, here is prose only.") {
		t.Error("the retry must quote the rejected reply")
	}
}

func TestRunPass2RejectedTwiceLeavesTheStepsAlone(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	onlyOne := func(_ int, p string) (string, error) {
		ids := stepsToSpecify(p)
		s := StepSpecOut{ID: ids[0], Description: "d", AcceptanceCriteria: []string{"c"}, DependsOn: []string{}}
		b, _ := json.Marshal(Pass2Output{Steps: []StepSpecOut{s}}) // the second step is missing
		return string(b), nil
	}
	res, err := RunPass2(context.Background(), r, &fake{reply: onlyOne}, Options2{})
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "missing from the reply") {
		t.Fatalf("err = %v", err)
	}
	if res.Steps != 0 {
		t.Errorf("Result2 = %+v", res)
	}
	a, _ := r.Get(s1)
	if a.Planning.Stage != plantree.StageSkeleton || a.Work != nil {
		t.Errorf("a rejected reply must write nothing: %+v", a)
	}
}

func TestRunPass2SkipsStepsThatAreDraftedOrOnHold(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s2)
	if _, err := r.Update(s2, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if got := stepsToSpecify(f.prompts[0]); len(got) != 1 || got[0] != s1 {
		t.Errorf("only step 1 should be asked for, got %v", got)
	}
	skipped, _ := r.Get(s2)
	if skipped.Work != nil {
		t.Error("a skipped step must not be specified")
	}

	again := specFake()
	res, err := RunPass2(context.Background(), r, again, Options2{})
	if err != nil || len(again.prompts) != 0 || res != (Result2{}) {
		t.Errorf("nothing left to specify: err=%v calls=%d res=%+v", err, len(again.prompts), res)
	}
}

func TestRunPass2AddsAHintForAContextWindowError(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("server said no")
	f := &fake{reply: func(int, string) (string, error) {
		return "", fmt.Errorf("%w: status 400: {\"error\":{\"type\":\"exceed_context_size_error\",\"n_ctx\":4096}}", sentinel)
	}}
	_, err := RunPass2(context.Background(), r, f, Options2{})
	if err == nil || !strings.Contains(err.Error(), "lower Options2.BriefBytes") || !errors.Is(err, sentinel) {
		t.Errorf("err = %v", err)
	}
}

func TestRunPass2NeedsAPlanAndHonorsCancellation(t *testing.T) {
	empty := plantree.Open(t.TempDir())
	if _, err := RunPass2(context.Background(), empty, specFake(), Options2{}); !errors.Is(err, plantree.ErrNotFound) {
		t.Errorf("no plan: err = %v, want ErrNotFound", err)
	}
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := specFake()
	if _, err := RunPass2(ctx, r, f, Options2{}); !errors.Is(err, context.Canceled) || len(f.prompts) != 0 {
		t.Errorf("cancelled: err=%v calls=%d", err, len(f.prompts))
	}
}

func TestRunPass2RefusesADependencyOnAHeldStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s1)
	if _, err := r.Update(s1, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dependsOnHeld := func(_ int, p string) (string, error) {
		s := StepSpecOut{ID: stepsToSpecify(p)[0], Description: "d", AcceptanceCriteria: []string{"c"}, DependsOn: []string{s1}}
		b, _ := json.Marshal(Pass2Output{Steps: []StepSpecOut{s}})
		return string(b), nil
	}
	_, err := RunPass2(context.Background(), r, &fake{reply: dependsOnHeld}, Options2{})
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "not a step of this task") {
		t.Fatalf("err = %v", err)
	}
	b, _ := r.Get(s2)
	if b.Planning.Stage != plantree.StageSkeleton || b.Work != nil {
		t.Errorf("step 2 must stay a skeleton: %+v", b)
	}
}

func TestRunPass2SecondBatchSeesTheFirstBatchAsSpecified(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 2 {
		t.Fatalf("%d prompts", len(f.prompts))
	}
	second := f.prompts[1]
	if !regexp.MustCompile(`step-001: .* \[specified\]`).MatchString(second) || !regexp.MustCompile(`step-002: .* \[to specify\]`).MatchString(second) {
		t.Errorf("second prompt step list:\n%s", second)
	}
	if strings.Contains(f.prompts[0], "[specified]") {
		t.Error("the first prompt must not show any step as specified")
	}
}

func TestRunPass2ReportsTasksWithoutSteps(t *testing.T) {
	r := newRepo(t)
	out := sampleOut()
	out.Phases[0].Tasks = append(out.Phases[0].Tasks, TaskOut{Title: "Docs", Digest: "explain it"})
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), r, specFake(), Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EmptyTasks != 1 {
		t.Errorf("EmptyTasks = %d, want 1", res.EmptyTasks)
	}
	got, err := EmptyTasks(r)
	if err != nil || len(got) != 1 || got[0] != "phase-001.task-002" {
		t.Errorf("EmptyTasks(repo) = %v, %v", got, err)
	}
	kinds := actionKinds(t, r)
	if !strings.Contains(kinds, "decompose:phase-001.task-002") || strings.Contains(kinds, "approve") {
		t.Errorf("NextActions = %s", kinds)
	}
}

func TestRunPass2ShowsTheStoredFactsAndAnOptionOverridesThem(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if err := WriteFacts(r, "STORED FACTS: go test ./..."); err != nil {
		t.Fatal(err)
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "STORED FACTS: go test ./...") {
		t.Error("RunPass2 must show the stored facts")
	}

	r2 := newRepo(t)
	if _, err := Merge(r2, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if err := WriteFacts(r2, "STORED FACTS"); err != nil {
		t.Fatal(err)
	}
	f2 := specFake()
	if _, err := RunPass2(context.Background(), r2, f2, Options2{Facts: "OVERRIDE FACTS"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f2.prompts[0], "OVERRIDE FACTS") || strings.Contains(f2.prompts[0], "STORED FACTS") {
		t.Error("Options2.Facts must override the stored facts")
	}
}

// askingFake specifies every step of a batch except askID, which it asks a
// question about instead.
func askingFake(askID string) *fake {
	return &fake{reply: func(_ int, p string) (string, error) {
		var steps []StepSpecOut
		var asks []string
		for _, id := range stepsToSpecify(p) {
			if id == askID {
				asks = append(asks, id)
				continue
			}
			steps = append(steps, StepSpecOut{
				ID: id, Description: "implement " + id, AcceptanceCriteria: []string{"it works"},
				TestCommand: []string{"go", "test"}, DependsOn: []string{},
			})
		}
		var qs []QuestionOut
		if len(asks) > 0 {
			q := goodQ()
			q.Affects = asks
			qs = append(qs, q)
		}
		b, _ := json.Marshal(Pass2Output{Steps: steps, Questions: qs})
		return string(b), nil
	}}
}

func TestRunPass2AsksInsteadOfGuessingAndHoldsThatStep(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), r, askingFake(s1), Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Questions != 1 || res.Steps != 1 || res.Passes != 1 {
		t.Errorf("Result2 = %+v, want 1 question, 1 step specified, 1 pass", res)
	}
	if got := stageOf(t, r, s1); got != plantree.StageAwaitingAnswers {
		t.Errorf("the step asked about is %s, want awaiting_answers", got)
	}
	if got := stageOf(t, r, s2); got != plantree.StageDrafted {
		t.Errorf("the other step is %s, want drafted", got)
	}
	if n, _ := r.Get(s1); n.Work != nil {
		t.Error("a step asked about must get no specification")
	}
	qs, _ := LoadQuestions(r)
	if len(qs) != 1 || qs[0].Source != "specifying task phase-001.task-001" || len(qs[0].Affects) != 1 || qs[0].Affects[0] != s1 {
		t.Errorf("questions = %+v", qs)
	}
	if got := actionKinds(t, r); !strings.Contains(got, "answer:"+s1) || strings.Contains(got, "approve") {
		t.Errorf("NextActions = %s; want an answer action for the held step and no approve", got)
	}
}

func TestReplayingAPass2QuestionAsksNothingTwice(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	out := Pass2Output{Questions: []QuestionOut{askAbout(s1)}}
	first, err := recordPass2Questions(r, "phase-001.task-001", out)
	second, err2 := recordPass2Questions(r, "phase-001.task-001", out)
	if err != nil || err2 != nil || first != 1 || second != 0 {
		t.Errorf("first=%d second=%d errs=%v %v; want 1 then 0", first, second, err, err2)
	}
	if qs, _ := LoadQuestions(r); len(qs) != 1 {
		t.Errorf("%d questions, want 1", len(qs))
	}
}

func TestRunPass2ShowsTheOwnersDecisionsToTheTasksTheyAffect(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	q := twoOptions()
	q.Question = "Which database should the module use?"
	q.Affects = []string{"phase-001.task-001"}
	if _, err := AddQuestions(r, []NewQuestion{q}); err != nil {
		t.Fatal(err)
	}
	other := twoOptions()
	other.Question = "Unrelated decision about billing?"
	other.Affects = []string{"phase-009"}
	if _, err := AddQuestions(r, []NewQuestion{other}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"q-001", "q-002"} {
		if _, err := AnswerQuestion(r, id, Answer{OptionIDs: []string{"opt-2"}}); err != nil {
			t.Fatal(err)
		}
	}
	f := specFake()
	if _, err := RunPass2(context.Background(), r, f, Options2{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.prompts[0], "Which database should the module use? -> Postgres") {
		t.Error("the decision that affects this task must be shown")
	}
	if strings.Contains(f.prompts[0], "billing") {
		t.Error("a decision about another part of the plan must not be shown")
	}
}

func TestASecondBatchSeesAnAskedStepAsWaiting(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	f := askingFake(s1)
	if _, err := RunPass2(context.Background(), r, f, Options2{StepsPerPass: 1}); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 2 || !strings.Contains(f.prompts[1], s1+": Create module [waiting for an answer]") {
		t.Errorf("%d prompts; the second must show step 1 as waiting for an answer", len(f.prompts))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: decisionsFor` (and `recordPass2Questions`, `Decisions`).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/decisions.go`:

```go
package plan

import (
	"fmt"
	"strings"
)

const (
	maxDecisions      = 6
	decisionLineBytes = 300
	decisionQuestion  = 200
	decisionNoteBytes = 150
)

// decisionsFor renders the answered questions that affect any of ids as short
// lines for a prompt: what was asked and what the owner chose. ids are the
// nodes the prompt is about (its phase, task and steps). At most maxDecisions
// lines are shown, each cut to decisionLineBytes, in the order the questions
// were asked. It returns "" when there is nothing to show.
func decisionsFor(qs []Question, ids []string) string {
	want := setOf(ids)
	var lines []string
	omitted := 0
	for _, q := range qs {
		if q.Status != QuestionAnswered || q.Answer == nil || !affectsAny(q, want) {
			continue
		}
		if len(lines) == maxDecisions {
			omitted++
			continue
		}
		lines = append(lines, cutBytes(decisionLine(q), decisionLineBytes))
	}
	if len(lines) == 0 {
		return ""
	}
	out := "- " + strings.Join(lines, "\n- ")
	if omitted > 0 {
		out += fmt.Sprintf("\n  (%d more decisions not shown)", omitted)
	}
	return out
}

func affectsAny(q Question, want map[string]bool) bool {
	for _, id := range q.Affects {
		if want[id] {
			return true
		}
	}
	return false
}

func decisionLine(q Question) string {
	labels := map[string]string{}
	for _, o := range q.Options {
		labels[o.ID] = o.Label
	}
	var chosen []string
	for _, id := range q.Answer.OptionIDs {
		if l, ok := labels[id]; ok {
			chosen = append(chosen, oneLine(l))
		}
	}
	line := cutBytes(oneLine(q.Question), decisionQuestion) + " -> "
	if len(chosen) > 0 {
		line += strings.Join(chosen, "; ")
	}
	if note := oneLine(q.Answer.Text); note != "" {
		if len(chosen) > 0 {
			line += " (note: " + cutBytes(note, decisionNoteBytes) + ")"
		} else {
			line += cutBytes(note, decisionNoteBytes)
		}
	}
	return line
}
```

Replace the whole of `plantree/plan/pass2json.go`:

```go
package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on what one specification pass may return.
const (
	maxDescRunes      = 2000
	maxCriteria       = 10
	maxCriterionRunes = 300
	maxTargetPaths    = 20
	maxPathRunes      = 300
	maxCommandArgs    = 20
	maxArgRunes       = 200
)

// StepSpecOut is the specification a pass proposes for one step.
type StepSpecOut struct {
	ID                 string   `json:"id"`
	Description        string   `json:"description"`
	TargetPaths        []string `json:"target_paths"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	TestCommand        []string `json:"test_command"`
	DependsOn          []string `json:"depends_on"`
}

// Pass2Output is what one specification pass returns for a batch of steps.
type Pass2Output struct {
	Steps     []StepSpecOut `json:"steps"`
	Questions []QuestionOut `json:"questions"` // optional; affects are step ids from the batch
}

// ParsePass2 finds, strictly decodes and validates a specification pass reply.
// batch is the ids of the steps that were asked for; the reply must cover
// exactly those, once each. siblings is the ids of every step of the task: a
// step may depend only on an earlier-numbered sibling, which makes a dependency
// cycle impossible by construction. Its errors are specific enough to send back
// to the model as a correction.
func ParsePass2(reply string, batch, siblings []string) (Pass2Output, error) {
	return parseFirst(reply, func(raw string) (Pass2Output, error) {
		return decodePass2(raw, batch, siblings)
	})
}

func decodePass2(raw string, batch, siblings []string) (Pass2Output, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var out Pass2Output
	if err := dec.Decode(&out); err != nil {
		return Pass2Output{}, fmt.Errorf("the JSON does not match the schema: %s", cutBytes(err.Error(), 400))
	}
	if err := validatePass2(out, batch, siblings); err != nil {
		return Pass2Output{}, err
	}
	return out, nil
}

func validatePass2(out Pass2Output, batch, siblings []string) error {
	want := setOf(batch)
	sibling := setOf(siblings)
	asked := map[string]bool{}
	err := validateQuestionOuts(out.Questions, func(where, affect string) error {
		if !want[affect] {
			return fmt.Errorf("%s: affects %q, which is not one of the steps asked for", where, clip(affect))
		}
		asked[affect] = true
		return nil
	})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range out.Steps {
		id := clip(s.ID)
		if !want[s.ID] {
			return fmt.Errorf("step %q is not one of the steps asked for", id)
		}
		if seen[s.ID] {
			return fmt.Errorf("step %q appears more than once", id)
		}
		if asked[s.ID] {
			return fmt.Errorf("step %q is both specified and named in a question; leave it out of steps until the question is answered, or drop the question", id)
		}
		seen[s.ID] = true
		where := fmt.Sprintf("step %q", id)
		if err := checkSpec(where, s, sibling); err != nil {
			return err
		}
	}
	for _, id := range batch {
		if !seen[id] && !asked[id] {
			return fmt.Errorf("step %q was asked for but is missing from the reply (specify it, or ask a question that names it)", clip(id))
		}
	}
	return nil
}

func checkSpec(where string, s StepSpecOut, sibling map[string]bool) error {
	if strings.TrimSpace(s.Description) == "" {
		return fmt.Errorf("%s: description is empty", where)
	}
	if utf8.RuneCountInString(s.Description) > maxDescRunes {
		return fmt.Errorf("%s: description is longer than %d characters", where, maxDescRunes)
	}
	if len(s.AcceptanceCriteria) == 0 {
		return fmt.Errorf("%s: acceptance_criteria needs at least one check a reviewer can verify", where)
	}
	if len(s.AcceptanceCriteria) > maxCriteria {
		return fmt.Errorf("%s: too many acceptance criteria (%d, at most %d)", where, len(s.AcceptanceCriteria), maxCriteria)
	}
	for _, c := range s.AcceptanceCriteria {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("%s: an acceptance criterion is empty", where)
		}
		if utf8.RuneCountInString(c) > maxCriterionRunes {
			return fmt.Errorf("%s: an acceptance criterion is longer than %d characters", where, maxCriterionRunes)
		}
	}
	if len(s.TargetPaths) > maxTargetPaths {
		return fmt.Errorf("%s: too many target_paths (%d, at most %d)", where, len(s.TargetPaths), maxTargetPaths)
	}
	for _, p := range s.TargetPaths {
		if utf8.RuneCountInString(p) > maxPathRunes {
			return fmt.Errorf("%s: a target path is longer than %d characters", where, maxPathRunes)
		}
		if err := safeRelPath(p); err != nil {
			return fmt.Errorf("%s: target path %q %w", where, clip(p), err)
		}
	}
	if len(s.TestCommand) > maxCommandArgs {
		return fmt.Errorf("%s: test_command has too many arguments (%d, at most %d)", where, len(s.TestCommand), maxCommandArgs)
	}
	for _, a := range s.TestCommand {
		if strings.TrimSpace(a) == "" {
			return fmt.Errorf("%s: test_command has an empty argument", where)
		}
		if utf8.RuneCountInString(a) > maxArgRunes {
			return fmt.Errorf("%s: a test_command argument is longer than %d characters", where, maxArgRunes)
		}
	}
	return checkDeps(where, s, sibling)
}

// checkDeps requires every dependency to be an earlier-numbered sibling step.
func checkDeps(where string, s StepSpecOut, sibling map[string]bool) error {
	seen := map[string]bool{}
	for _, d := range s.DependsOn {
		if !sibling[d] {
			return fmt.Errorf("%s: depends_on %q is not a step of this task", where, clip(d))
		}
		if segmentNumber(d) >= segmentNumber(s.ID) {
			return fmt.Errorf("%s: depends_on %q must be an earlier step than this one", where, clip(d))
		}
		if seen[d] {
			return fmt.Errorf("%s: depends_on lists %q twice", where, clip(d))
		}
		seen[d] = true
	}
	return nil
}

// safeRelPath rejects a target path that is empty, absolute, drive-qualified,
// contains a control character, or climbs out with "..". Target paths only
// declare scope; this keeps the declaration honest.
func safeRelPath(p string) error {
	switch {
	case strings.TrimSpace(p) == "":
		return fmt.Errorf("is empty")
	case strings.ContainsAny(p, "\x00\r\n"):
		return fmt.Errorf("contains a control character")
	case strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`):
		return fmt.Errorf("must be relative to the repository, not absolute")
	case len(p) >= 2 && p[1] == ':':
		return fmt.Errorf("must be relative to the repository, not drive-qualified")
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("must not contain %q", "..")
		}
	}
	return nil
}

func setOf(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
```

Replace the whole of `plantree/plan/prompt2.go`:

```go
package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// Pass-2 defaults, sized so the worst-case prompt stays near 27,000 bytes.
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 4000
)

// siblingListCapBytes bounds the list of every step of the task in a prompt.
const siblingListCapBytes = 3000

// fit makes a node field safe for a prompt: one line, at most n bytes.
func fit(s string, n int) string { return cutBytes(oneLine(s), n) }

// stepTag tells the model whether a sibling can be depended on.
func stepTag(s plantree.Node) string {
	switch {
	case onHold(s):
		return "on hold: " + string(s.Status)
	case s.Planning.Stage == plantree.StageDrafted || s.Planning.Stage == plantree.StageApproved:
		return "specified"
	case s.Planning.Stage == plantree.StageAwaitingAnswers:
		return "waiting for an answer"
	}
	return "to specify"
}

func stepList(steps []plantree.Node) string {
	var b strings.Builder
	omitted := 0
	for _, s := range steps {
		line := "- " + s.ID + ": " + fit(s.Title, 200) + " [" + stepTag(s) + "]\n"
		if b.Len()+len(line) > siblingListCapBytes {
			omitted++
			continue
		}
		b.WriteString(line)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  (%d more steps not shown)\n", omitted)
	}
	return b.String()
}

// Pass2Input is everything one specification pass shows the model. Facts and
// Excerpts may be empty. Nothing about any other task belongs here.
type Pass2Input struct {
	Project   string
	Overview  string
	Facts     string // project facts: language, build and test commands, layout
	Decisions string // answers already given that affect this task
	Excerpts  string // brief excerpts that produced the task
	Phase     plantree.Node
	Task      plantree.Node
	Siblings  []plantree.Node // every step of the task
	Batch     []plantree.Node // the steps to specify now
}

// Pass2Prompt builds the prompt for one specification pass. It carries the
// overview, the project facts, the phase and task the steps belong to, the
// list of the task's steps, the steps to specify now, and optionally excerpts
// of the brief. It carries nothing about any other task.
func Pass2Prompt(in Pass2Input) string {
	phase, task, siblings, batch := in.Phase, in.Task, in.Siblings, in.Batch
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing the work specification for some steps of ONE task in a project plan for %q. You see only this task.\n\n", fit(in.Project, 100))
	b.WriteString("Running overview of the whole project:\n")
	b.WriteString(orNone(in.Overview))
	b.WriteString("\n\nRepository facts (language, build and test commands, layout):\n")
	if strings.TrimSpace(in.Facts) == "" {
		b.WriteString("(not provided)\n")
	} else {
		b.WriteString(cutBytes(strings.TrimSpace(in.Facts), FactsCapBytes) + "\n")
	}
	b.WriteString("\nDecisions already made by the project owner (follow them; do not ask again):\n")
	if strings.TrimSpace(in.Decisions) == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(in.Decisions + "\n")
	}
	fmt.Fprintf(&b, "\nPhase: %s\nWhy: %s\nObjective: %s\n", fit(phase.Title, 200), fit(phase.ContextDigest, 500), orNone(fit(phase.Objective, 1000)))
	fmt.Fprintf(&b, "\nTask: %s\nWhy: %s\nObjective: %s\n", fit(task.Title, 200), fit(task.ContextDigest, 500), orNone(fit(task.Objective, 1000)))
	b.WriteString("\nAll steps of this task, in order. A step may depend only on an EARLIER step in this list:\n")
	b.WriteString(stepList(siblings))
	b.WriteString("\nSteps to specify now:\n")
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, fit(s.Title, 200), fit(s.ContextDigest, 500))
	}
	b.WriteString("\nBrief excerpts that produced this task (context only, may be partial):\n")
	if strings.TrimSpace(in.Excerpts) == "" {
		b.WriteString("(not available)\n")
	} else {
		fmt.Fprintf(&b, "<<<BRIEF EXCERPTS\n%s\nBRIEF EXCERPTS>>>\n", in.Excerpts)
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Return exactly the steps listed under \"Steps to specify now\", each once, using its id.\n")
	b.WriteString("- description: what to build or change, concrete enough that an agent can start without asking.\n")
	b.WriteString("- target_paths: repository-relative files or directories the step touches. Never absolute, never containing \"..\".\n")
	b.WriteString("- acceptance_criteria: 1 to 10 checks a reviewer can verify.\n")
	b.WriteString("- test_command: the command as an array of arguments that verifies the step, taken from the repository facts; if the facts do not say, use an empty array rather than guessing.\n")
	b.WriteString("- depends_on: ids of EARLIER steps of this task that must be done first, or an empty array.\n")
	b.WriteString("- description: at most 2000 characters. Each acceptance criterion: at most 300 characters, and at most 10 criteria.\n")
	b.WriteString("- target_paths: at most 20 paths of at most 300 characters each. test_command: at most 20 arguments of at most 200 characters each.\n")
	b.WriteString("- In every array, never put an empty string.\n")
	b.WriteString("- Do not depend on a step marked on hold.\n")
	b.WriteString("- If you cannot specify a step because something is unknown that only the project owner can decide, do not guess: leave that step out of \"steps\" and ask a question whose \"affects\" lists its id. At most 5 questions, each with 2 to 8 options (or none for a free-text question) and \"recommended\" (option labels) if you have one. Never ask what the decisions above or the brief already answer.\n")
	b.WriteString("- Do not invent scope the task does not need. Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"steps":[{"id":"<step id>","description":"<what to build>","target_paths":["<path/to/file>"],"acceptance_criteria":["<a check a reviewer can verify>"],"test_command":["<command>","<arg>"],"depends_on":[]}],"questions":[]}`)
	b.WriteString("\n")
	return b.String()
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
	// With the defaults one pass is at most about 27,000 bytes: BriefBytes +
	// OverviewCapBytes + FactsCapBytes + 3000 (step list) + about 2,000
	// (decisions) + about 8,000 (phase, task and the steps being specified) +
	// 3,500 (instructions). At 3 to 4 bytes per token that must leave room for
	// the reply inside the model's window.
	BriefBytes int
	// Facts is text describing the repository (language, how to build and test,
	// layout) shown to every pass, cut to FactsCapBytes. If empty, RunPass2
	// reads the facts stored with WriteFacts, if any.
	Facts string
}

// Result2 summarizes one RunPass2 call.
type Result2 struct {
	Tasks  int // tasks whose pending steps were all specified by this call
	Steps  int // steps specified by this call
	Passes int // model passes made by this call
	// Questions counts new questions asked by this call. The steps they name
	// wait for an answer and are not specified.
	Questions int
	// EmptyTasks counts tasks that have no steps at all; pass 2 cannot specify
	// them, and NextActions keeps offering decompose for them.
	EmptyTasks int
}

// taskWork is one task with the steps still waiting for a specification.
type taskWork struct {
	phase   plantree.Node
	task    plantree.Node
	steps   []plantree.Node // every step of the task
	pending []plantree.Node // steps that need a specification
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

// pendingWork lists, in tree order, every task that has a step needing a
// specification. It is derived from the tree alone, so a resumed run finds
// exactly what is left.
func pendingWork(repo *plantree.Repo) ([]taskWork, error) {
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
			var pending []plantree.Node
			for _, s := range steps {
				if needsSpec(s) {
					pending = append(pending, s)
				}
			}
			if len(pending) > 0 {
				out = append(out, taskWork{phase: phase, task: n, steps: steps, pending: pending})
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
	facts := opt.Facts
	if strings.TrimSpace(facts) == "" {
		if facts, err = ReadFacts(repo); err != nil {
			return Result2{}, err
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
	work, err := pendingWork(repo)
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
	res := Result2{EmptyTasks: len(empty)}
	for _, w := range work {
		ids := append([]string{w.task.ID}, idsOf(w.steps)...)
		excerpts := Excerpts(chunks, prov.chunksFor(ids), opt.BriefBytes)
		decisions := decisionsFor(allQuestions, append([]string{w.phase.ID}, ids...))
		for start := 0; start < len(w.pending); start += opt.StepsPerPass {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			end := start + opt.StepsPerPass
			if end > len(w.pending) {
				end = len(w.pending)
			}
			batch := w.pending[start:end]
			batchIDs := idsOf(batch)
			var live []plantree.Node
			for _, s := range w.steps {
				if !onHold(s) {
					live = append(live, s)
				}
			}
			siblingIDs := idsOf(live)
			prompt := Pass2Prompt(Pass2Input{
				Project: opt.ProjectName, Overview: overview, Facts: facts, Decisions: decisions, Excerpts: excerpts,
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
			if err := applySpecs(repo, out); err != nil {
				return res, taskError(w.task.ID, err)
			}
			markSpecified(w.steps, out)
			markAsked(w.steps, out)
			res.Steps += len(out.Steps)
		}
		res.Tasks++
	}
	return res, nil
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
// half-specified reaches the tree.
func applySpecs(repo *plantree.Repo, out Pass2Output) error {
	for _, s := range out.Steps {
		cur, err := repo.Get(s.ID)
		if err != nil {
			return err
		}
		spec := s
		if _, err := repo.Update(s.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Work = &plantree.Work{
				Description:        spec.Description,
				TargetPaths:        spec.TargetPaths,
				AcceptanceCriteria: spec.AcceptanceCriteria,
				TestCommand:        spec.TestCommand,
			}
			n.DependsOn = spec.DependsOn
			n.Planning.Stage = plantree.StageDrafted
			return nil
		}); err != nil {
			return fmt.Errorf("writing the specification of %s: %w", s.ID, err)
		}
	}
	return nil
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

Replace the whole of `plantree/plan/questions_apply.go`:

```go
package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// stepsUnder returns every step that is one of ids or lies below one of them.
func stepsUnder(repo *plantree.Repo, ids []string) ([]plantree.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []plantree.Node
	err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		for _, id := range ids {
			if n.ID == id || strings.HasPrefix(n.ID, id+".") {
				out = append(out, n)
				return nil
			}
		}
		return nil
	})
	return out, err
}

// holdSteps moves every step under ids that is still waiting for its
// specification (a skeleton or inspected step, not on hold) to the
// awaiting-answers stage, so pass 2 leaves it alone until its questions are
// answered. It returns how many steps it moved. Calling it again changes
// nothing.
func holdSteps(repo *plantree.Repo, ids []string) (int, error) {
	steps, err := stepsUnder(repo, ids)
	if err != nil {
		return 0, err
	}
	held := 0
	for _, s := range steps {
		if !needsSpec(s) {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageAwaitingAnswers
			return nil
		}); err != nil {
			return held, fmt.Errorf("holding %s for an answer: %w", s.ID, err)
		}
		held++
	}
	return held, nil
}

// applyPass1Questions records the questions a skeleton pass asked and holds
// the steps they affect. A question's affects are phase or task titles; they
// are matched, ignoring case and spacing, against the nodes this chunk created
// or reused, and a title that matches none is ignored. It returns how many
// questions were new. Replaying the same pass adds nothing and holds nothing
// new, and a question that is already answered holds nothing.
func applyPass1Questions(repo *plantree.Repo, chunk Chunk, out Pass1Output, touched []string) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	byTitle := map[string][]string{}
	for _, id := range touched {
		n, err := repo.Get(id)
		if err != nil {
			return 0, err
		}
		key := NormalizeTitle(n.Title)
		byTitle[key] = append(byTitle[key], id)
	}
	source := fmt.Sprintf("chunk %d of the brief", chunk.Index+1)
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		var ids []string
		seen := map[string]bool{}
		for _, title := range q.Affects {
			for _, id := range byTitle[NormalizeTitle(title)] {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		nq := newQuestion(q, ids)
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}

// recordPass2Questions records the questions a specification pass asked and
// holds the steps they name: those steps get no specification until the
// questions are answered. It returns how many questions were new. Replaying the
// same pass adds nothing new.
func recordPass2Questions(repo *plantree.Repo, taskID string, out Pass2Output) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	source := "specifying task " + taskID
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		nq := newQuestion(q, append([]string{}, q.Affects...))
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}

// markAsked records in the in-memory snapshot that the steps named by out's
// questions now wait for an answer, so a later batch of the same task sees them
// so.
func markAsked(steps []plantree.Node, out Pass2Output) {
	asked := map[string]bool{}
	for _, q := range out.Questions {
		for _, id := range q.Affects {
			asked[id] = true
		}
	}
	for i := range steps {
		if asked[steps[i].ID] && needsSpec(steps[i]) {
			steps[i].Planning.Stage = plantree.StageAwaitingAnswers
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/decisions.go gophermind-lib/plantree/plan/decisions_test.go gophermind-lib/plantree/plan/pass2json.go gophermind-lib/plantree/plan/pass2json_test.go gophermind-lib/plantree/plan/prompt2.go gophermind-lib/plantree/plan/prompt2_test.go gophermind-lib/plantree/plan/runner2.go gophermind-lib/plantree/plan/runner2_test.go gophermind-lib/plantree/plan/questions_apply.go
git commit -m "feat(plan): pass 2 asks instead of guessing and sees the decisions already made

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 6: Releasing answered steps

**Files:**
- Create: `gophermind-lib/plantree/plan/release_test.go`
- Replace: `questions_apply.go`, `runner2.go` (same directory)

**Interfaces:**
- Consumes: `OpenQuestions`, `holdSteps`, `onHold`, `plantree.ParentID`.
- Produces:
  - `func ReleaseAnswered(repo *plantree.Repo) (int, error)` (moves every step waiting for an answer back to `inspected` once no open question affects it, directly or through the task or phase above it; a step on hold stays; idempotent)
  - `Result2.Released int`; `RunPass2` calls `ReleaseAnswered` first, so a step released by an answer is specified by the same run
  - `TestPass1QuestionAnswerThenPass2` in `release_test.go` walks the whole loop with fakes

Together with Tasks 4 and 5 this closes the loop: ask, wait, answer, release, specify with the decision shown.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/release_test.go`:

```go
package plan

import (
	"context"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// heldRepo is a repo whose steps s1 and s2 both wait for an answer.
func heldRepo(t *testing.T) *plantree.Repo {
	t.Helper()
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	if _, err := holdSteps(r, []string{"phase-001"}); err != nil {
		t.Fatal(err)
	}
	return r
}

func ask(t *testing.T, r *plantree.Repo, text string, affects ...string) string {
	t.Helper()
	q := twoOptions()
	q.Question = text
	q.Affects = affects
	got, err := AddQuestions(r, []NewQuestion{q})
	if err != nil {
		t.Fatal(err)
	}
	return got[0].ID
}

func TestReleaseAnsweredWaitsForEveryQuestionThatAffectsAStep(t *testing.T) {
	r := heldRepo(t)
	first := ask(t, r, "First?", s1)
	second := ask(t, r, "Second?", s1, s2)

	if n, err := ReleaseAnswered(r); err != nil || n != 0 {
		t.Fatalf("nothing answered: released %d, err %v", n, err)
	}
	if _, err := AnswerQuestion(r, first, Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("step 1 still has an open question, released %d", n)
	}
	if _, err := AnswerQuestion(r, second, Answer{Text: "either"}); err != nil {
		t.Fatal(err)
	}
	n, err := ReleaseAnswered(r)
	if err != nil || n != 2 {
		t.Fatalf("released %d, err %v; want 2", n, err)
	}
	for _, id := range []string{s1, s2} {
		if got := stageOf(t, r, id); got != plantree.StageInspected {
			t.Errorf("%s = %s, want inspected", id, got)
		}
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("a second call released %d, want 0", n)
	}
}

func TestReleaseAnsweredHonorsAQuestionAboveTheStep(t *testing.T) {
	r := heldRepo(t)
	id := ask(t, r, "About the whole task?", "phase-001.task-001")
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("an open question on the task holds its steps, released %d", n)
	}
	if _, err := AnswerQuestion(r, id, Answer{Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 2 {
		t.Errorf("released %d, want 2", n)
	}
	ask(t, r, "Plan-wide?", plantree.RootID)
	if _, err := holdSteps(r, []string{"phase-001"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("a plan-wide open question holds everything, released %d", n)
	}
}

func TestReleaseAnsweredLeavesOtherStepsAlone(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil { // both steps are skeletons, not waiting
		t.Fatal(err)
	}
	if n, err := ReleaseAnswered(r); err != nil || n != 0 {
		t.Fatalf("released %d, err %v", n, err)
	}
	if _, err := holdSteps(r, []string{s2}); err != nil {
		t.Fatal(err)
	}
	cur, _ := r.Get(s2)
	if _, err := r.Update(s2, cur.NodeRevision, func(n *plantree.Node) error {
		n.Status = plantree.StatusSkipped
		n.Reason = "out of scope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ReleaseAnswered(r); n != 0 {
		t.Errorf("a step on hold must not be released, released %d", n)
	}
	if got := stageOf(t, r, s2); got != plantree.StageAwaitingAnswers {
		t.Errorf("stage = %s", got)
	}
}

// TestPass1QuestionAnswerThenPass2 walks the whole question loop with fakes:
// pass 1 asks, pass 2 skips the held step, the owner answers, pass 2 releases
// and specifies it with the decision in its prompt, and only approval is left.
func TestPass1QuestionAnswerThenPass2(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunkQ}, opts); err != nil {
		t.Fatal(err)
	}

	first := specFake()
	res, err := RunPass2(context.Background(), r, first, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps != 2 || res.Released != 0 || len(first.prompts) != 2 {
		t.Errorf("first pass 2: %+v, %d calls; want 2 steps, none released, 2 calls (the held task is skipped)", res, len(first.prompts))
	}
	if got := actionKinds(t, r); !strings.Contains(got, "answer:phase-001.task-002.step-001") || strings.Contains(got, "approve") {
		t.Errorf("NextActions = %s", got)
	}

	if _, err := AnswerQuestion(r, "q-001", Answer{OptionIDs: []string{"opt-2"}, Text: "B fits our stack"}); err != nil {
		t.Fatal(err)
	}
	second := specFake()
	res, err = RunPass2(context.Background(), plantree.Open(dir), second, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Released != 1 || res.Steps != 1 || len(second.prompts) != 1 {
		t.Errorf("second pass 2: %+v, %d calls; want 1 released, 1 step, 1 call", res, len(second.prompts))
	}
	if !strings.Contains(second.prompts[0], "Which framework? -> B (note: B fits our stack)") {
		t.Error("the owner's decision must reach the prompt of the task it affects")
	}
	if got := actionKinds(t, r); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: ReleaseAnswered` (and `Released`).

- [ ] **Step 3: Write the implementation**

Replace the whole of `plantree/plan/questions_apply.go`:

```go
package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// stepsUnder returns every step that is one of ids or lies below one of them.
func stepsUnder(repo *plantree.Repo, ids []string) ([]plantree.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []plantree.Node
	err := repo.Walk(func(n plantree.Node) error {
		if n.Kind() != plantree.KindStep {
			return nil
		}
		for _, id := range ids {
			if n.ID == id || strings.HasPrefix(n.ID, id+".") {
				out = append(out, n)
				return nil
			}
		}
		return nil
	})
	return out, err
}

// holdSteps moves every step under ids that is still waiting for its
// specification (a skeleton or inspected step, not on hold) to the
// awaiting-answers stage, so pass 2 leaves it alone until its questions are
// answered. It returns how many steps it moved. Calling it again changes
// nothing.
func holdSteps(repo *plantree.Repo, ids []string) (int, error) {
	steps, err := stepsUnder(repo, ids)
	if err != nil {
		return 0, err
	}
	held := 0
	for _, s := range steps {
		if !needsSpec(s) {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageAwaitingAnswers
			return nil
		}); err != nil {
			return held, fmt.Errorf("holding %s for an answer: %w", s.ID, err)
		}
		held++
	}
	return held, nil
}

// applyPass1Questions records the questions a skeleton pass asked and holds
// the steps they affect. A question's affects are phase or task titles; they
// are matched, ignoring case and spacing, against the nodes this chunk created
// or reused, and a title that matches none is ignored. It returns how many
// questions were new. Replaying the same pass adds nothing and holds nothing
// new, and a question that is already answered holds nothing.
func applyPass1Questions(repo *plantree.Repo, chunk Chunk, out Pass1Output, touched []string) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	byTitle := map[string][]string{}
	for _, id := range touched {
		n, err := repo.Get(id)
		if err != nil {
			return 0, err
		}
		key := NormalizeTitle(n.Title)
		byTitle[key] = append(byTitle[key], id)
	}
	source := fmt.Sprintf("chunk %d of the brief", chunk.Index+1)
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		var ids []string
		seen := map[string]bool{}
		for _, title := range q.Affects {
			for _, id := range byTitle[NormalizeTitle(title)] {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		nq := newQuestion(q, ids)
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}

// recordPass2Questions records the questions a specification pass asked and
// holds the steps they name: those steps get no specification until the
// questions are answered. It returns how many questions were new. Replaying the
// same pass adds nothing new.
func recordPass2Questions(repo *plantree.Repo, taskID string, out Pass2Output) (int, error) {
	if len(out.Questions) == 0 {
		return 0, nil
	}
	source := "specifying task " + taskID
	nqs := make([]NewQuestion, 0, len(out.Questions))
	for _, q := range out.Questions {
		nq := newQuestion(q, append([]string{}, q.Affects...))
		nq.Source = source
		nqs = append(nqs, nq)
	}
	before, err := LoadQuestions(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, q := range before {
		known[q.ID] = true
	}
	asked, err := AddQuestions(repo, nqs)
	if err != nil {
		return 0, err
	}
	added := 0
	var hold []string
	for _, q := range asked {
		if !known[q.ID] {
			added++
		}
		if q.Status == QuestionOpen {
			hold = append(hold, q.Affects...)
		}
	}
	if _, err := holdSteps(repo, hold); err != nil {
		return added, err
	}
	return added, nil
}

// markAsked records in the in-memory snapshot that the steps named by out's
// questions now wait for an answer, so a later batch of the same task sees them
// so.
func markAsked(steps []plantree.Node, out Pass2Output) {
	asked := map[string]bool{}
	for _, q := range out.Questions {
		for _, id := range q.Affects {
			asked[id] = true
		}
	}
	for i := range steps {
		if asked[steps[i].ID] && needsSpec(steps[i]) {
			steps[i].Planning.Stage = plantree.StageAwaitingAnswers
		}
	}
}

// ReleaseAnswered moves every step that waits for an answer back to the
// inspected stage once no open question still affects it, directly or through
// the task or phase above it, so pass 2 will specify it. It returns how many
// steps it released. A step on hold stays where it is. Calling it again changes
// nothing.
func ReleaseAnswered(repo *plantree.Repo) (int, error) {
	open, err := OpenQuestions(repo)
	if err != nil {
		return 0, err
	}
	blocked := map[string]bool{}
	for _, q := range open {
		for _, id := range q.Affects {
			blocked[id] = true
		}
	}
	var waiting []plantree.Node
	err = repo.Walk(func(n plantree.Node) error {
		if n.Kind() == plantree.KindStep && n.Planning.Stage == plantree.StageAwaitingAnswers && !onHold(n) {
			waiting = append(waiting, n)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	released := 0
	for _, s := range waiting {
		if affectedBy(blocked, s.ID) {
			continue
		}
		if _, err := repo.Update(s.ID, s.NodeRevision, func(n *plantree.Node) error {
			n.Planning.Stage = plantree.StageInspected
			return nil
		}); err != nil {
			return released, fmt.Errorf("releasing %s: %w", s.ID, err)
		}
		released++
	}
	return released, nil
}

// affectedBy reports whether id, or any node above it, is in blocked.
func affectedBy(blocked map[string]bool, id string) bool {
	for id != "" {
		if blocked[id] {
			return true
		}
		parent, err := plantree.ParentID(id)
		if err != nil {
			return false
		}
		id = parent
	}
	return false
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
	// With the defaults one pass is at most about 27,000 bytes: BriefBytes +
	// OverviewCapBytes + FactsCapBytes + 3000 (step list) + about 2,000
	// (decisions) + about 8,000 (phase, task and the steps being specified) +
	// 3,500 (instructions). At 3 to 4 bytes per token that must leave room for
	// the reply inside the model's window.
	BriefBytes int
	// Facts is text describing the repository (language, how to build and test,
	// layout) shown to every pass, cut to FactsCapBytes. If empty, RunPass2
	// reads the facts stored with WriteFacts, if any.
	Facts string
}

// Result2 summarizes one RunPass2 call.
type Result2 struct {
	Tasks  int // tasks whose pending steps were all specified by this call
	Steps  int // steps specified by this call
	Passes int // model passes made by this call
	// Released counts steps that were waiting for an answer and were released
	// because their questions are now answered; they are specified by this call.
	Released int
	// Questions counts new questions asked by this call. The steps they name
	// wait for an answer and are not specified.
	Questions int
	// EmptyTasks counts tasks that have no steps at all; pass 2 cannot specify
	// them, and NextActions keeps offering decompose for them.
	EmptyTasks int
}

// taskWork is one task with the steps still waiting for a specification.
type taskWork struct {
	phase   plantree.Node
	task    plantree.Node
	steps   []plantree.Node // every step of the task
	pending []plantree.Node // steps that need a specification
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

// pendingWork lists, in tree order, every task that has a step needing a
// specification. It is derived from the tree alone, so a resumed run finds
// exactly what is left.
func pendingWork(repo *plantree.Repo) ([]taskWork, error) {
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
			var pending []plantree.Node
			for _, s := range steps {
				if needsSpec(s) {
					pending = append(pending, s)
				}
			}
			if len(pending) > 0 {
				out = append(out, taskWork{phase: phase, task: n, steps: steps, pending: pending})
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
		return Result2{}, err
	}
	facts := opt.Facts
	if strings.TrimSpace(facts) == "" {
		if facts, err = ReadFacts(repo); err != nil {
			return Result2{}, err
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
	work, err := pendingWork(repo)
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
		for start := 0; start < len(w.pending); start += opt.StepsPerPass {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			end := start + opt.StepsPerPass
			if end > len(w.pending) {
				end = len(w.pending)
			}
			batch := w.pending[start:end]
			batchIDs := idsOf(batch)
			var live []plantree.Node
			for _, s := range w.steps {
				if !onHold(s) {
					live = append(live, s)
				}
			}
			siblingIDs := idsOf(live)
			prompt := Pass2Prompt(Pass2Input{
				Project: opt.ProjectName, Overview: overview, Facts: facts, Decisions: decisions, Excerpts: excerpts,
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
			if err := applySpecs(repo, out); err != nil {
				return res, taskError(w.task.ID, err)
			}
			markSpecified(w.steps, out)
			markAsked(w.steps, out)
			res.Steps += len(out.Steps)
		}
		res.Tasks++
	}
	return res, nil
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
// half-specified reaches the tree.
func applySpecs(repo *plantree.Repo, out Pass2Output) error {
	for _, s := range out.Steps {
		cur, err := repo.Get(s.ID)
		if err != nil {
			return err
		}
		spec := s
		if _, err := repo.Update(s.ID, cur.NodeRevision, func(n *plantree.Node) error {
			n.Work = &plantree.Work{
				Description:        spec.Description,
				TargetPaths:        spec.TargetPaths,
				AcceptanceCriteria: spec.AcceptanceCriteria,
				TestCommand:        spec.TestCommand,
			}
			n.DependsOn = spec.DependsOn
			n.Planning.Stage = plantree.StageDrafted
			return nil
		}); err != nil {
			return fmt.Errorf("writing the specification of %s: %w", s.ID, err)
		}
	}
	return nil
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

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/release_test.go gophermind-lib/plantree/plan/questions_apply.go gophermind-lib/plantree/plan/runner2.go
git commit -m "feat(plan): release steps whose questions are answered and specify them

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

### Task 7: The question loop end to end over HTTP

**Files:**
- Create: `gophermind-lib/plantree/plan/integration3_test.go`

**Interfaces:**
- Consumes: `RunPass1`, `RunPass2`, `AnswerQuestion`, `WriteFacts`, `ClientCompleter`, and the test helpers `writeSSE` (`integration_test.go`), `byChunkQ` (`questions_apply_test.go`), `pass2Reply`, `actionKinds` (`runner2_test.go`).
- Produces: `TestQuestionLoopEndToEndOverHTTP`, which runs pass 1 (asks one question), pass 2 (skips the held task), the answer, and a second pass 2 (releases and specifies it) through the real completer against a fake model server, and checks that the project facts and the decision reach the right prompts.

- [ ] **Step 1: Write the test**

Create `plantree/plan/integration3_test.go`:

```go
package plan

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// TestQuestionLoopEndToEndOverHTTP runs the whole question loop through the
// real completer against a fake model server: pass 1 asks a question, pass 2
// skips the held task, the owner answers, and a second pass 2 releases and
// specifies it with the decision and the project facts in its prompt.
func TestQuestionLoopEndToEndOverHTTP(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		if strings.Contains(body, "Steps to specify now") { // pass 2 bodies carry brief text, so test them first
			writeSSE(w, "```json\n"+pass2Reply(body)+"\n```")
			return
		}
		reply, err := byChunkQ(0, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeSSE(w, reply)
	}))
	defer srv.Close()

	dir := t.TempDir()
	repo := plantree.Open(dir)
	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}

	res1, err := RunPass1(context.Background(), repo, threePartBrief, c, opts)
	if err != nil || res1.Questions != 1 {
		t.Fatalf("RunPass1: %+v, %v", res1, err)
	}
	if err := WriteFacts(repo, "FACTS: Go 1.25; test with go test ./..."); err != nil {
		t.Fatal(err)
	}
	res2, err := RunPass2(context.Background(), plantree.Open(dir), c, Options2{})
	if err != nil || res2.Steps != 2 || res2.Passes != 2 || res2.Released != 0 {
		t.Fatalf("first RunPass2: %+v, %v", res2, err)
	}
	if _, err := AnswerQuestion(repo, "q-001", Answer{OptionIDs: []string{"opt-1"}}); err != nil {
		t.Fatal(err)
	}
	res3, err := RunPass2(context.Background(), plantree.Open(dir), c, Options2{})
	if err != nil || res3.Steps != 1 || res3.Released != 1 || res3.Passes != 1 {
		t.Fatalf("second RunPass2: %+v, %v", res3, err)
	}
	if got := actionKinds(t, repo); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 6 {
		t.Fatalf("%d requests, want 6 (three of pass 1, two then one of pass 2)", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d carries tools", i)
		}
	}
	for _, i := range []int{3, 4, 5} {
		if !strings.Contains(bodies[i], "FACTS: Go 1.25") {
			t.Errorf("pass-2 request %d must carry the project facts", i)
		}
	}
	// The request body is JSON, which writes ">" as \u003e.
	decided := func(body string) bool {
		return strings.Contains(strings.ReplaceAll(body, `\u003e`, ">"), "Which framework? -> A")
	}
	if decided(bodies[3]) || decided(bodies[4]) {
		t.Error("the decision does not exist yet when the first two pass-2 requests are made")
	}
	if !decided(bodies[5]) {
		t.Error("the last request must carry the owner's decision")
	}
	if strings.Contains(bodies[3], "phase-001.task-002.step-001:") || strings.Contains(bodies[4], "phase-001.task-002.step-001:") {
		// the held step is listed as a sibling of nothing else, so it must not be asked for
		t.Error("the held task must not be specified before the answer")
	}
}
```

- [ ] **Step 2: Run it**

Run: `gofmt -w plantree && go test ./plantree/plan/... -run EndToEnd -count=1 -v`
Expected: all end-to-end tests PASS. (This test adds no production code, so it has no failing phase: it checks that Tasks 1 to 6 work together.)

- [ ] **Step 3: Run every check**

Run: `go test ./plantree/... -count=1 && go test -race ./plantree/... -short -count=1 && gofmt -l plantree && go vet ./plantree/... && go build ./...`
Expected: all `ok`, no gofmt output, vet clean, the whole module builds.

- [ ] **Step 4: Commit**

```bash
git add gophermind-lib/plantree/plan/integration3_test.go
git commit -m "test(plan): run the question loop against the real completer over HTTP

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

## Self-review (M4)

- **Design coverage:** questions collected as the passes read (pass 1 and pass 2), each with options, an optional recommendation and free text; the owner's answers recorded; affected steps held until answered and then released; answers shown to the passes that need them; project facts given to pass 2. The question-round UI and re-planning are M5 by design.
- **M3 carry-forward resolved here:** project facts (the largest product risk), `Pass2Prompt` as a struct, questions in package `plan` so the shared helpers stay private, the transition out of `awaiting_answers` (`ReleaseAnswered`), and the wider end-to-end fixture. `Result2.Steps` now counts the steps actually specified, not the ones asked about.
- **Placeholders:** none. Every step carries full code, and the code was run green task by task in this order before the plan was generated.
- **Types:** `Question`, `NewQuestion`, `QuestionOut`, `Answer`, `Pass2Input`, `Result2` and the helpers are defined once and used with the same names later.
- **Known limits:** a question's `affects` in pass 1 resolves by title among the chunk's own nodes, so a title the chunk did not touch is ignored; an answer only informs pass 2 (it cannot restructure the plan, which is M5); a step named in a question is held whole (no partial specification); two concurrent runs can duplicate work (the run lock is M6); the pass-2 prompt threshold has about 660 bytes of headroom, so new prompt text needs a re-measure.

## Definition of done (M4)

`go test ./plantree/... -count=1`, `go test -race ./plantree/... -short -count=1`, `gofmt -l plantree` (empty), `go vet ./plantree/...` and `go build ./...` all clean; seven commits; a test proves pass 1 asks a question, pass 2 skips the held task, an answer releases it, and the next pass 2 specifies it with the decision in its prompt and ends with `NextActions` offering only approval; and the same loop runs through the real completer over HTTP.
