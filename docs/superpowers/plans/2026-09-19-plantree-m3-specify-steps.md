# plantree M3: specify every step Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After pass 1 has built the skeleton, write the work specification (description, target paths, acceptance criteria, test command, dependencies) of every step, one fresh-context model pass per batch of steps of one task, resumable after any error.

**Architecture:** Pass 2 extends the package `gophermind-lib/plantree/plan`. Pass 1 now also records which brief chunk produced or reused each node (a sidecar, `_state/provenance.json`), so a task can be shown the part of the brief that gave rise to it. `RunPass2` derives what is left from the tree alone (steps still at stage skeleton or inspected), so it needs no cursor. Each pass gets the overview, the task's phase and task, the task's step list, the batch of steps to specify, and bounded brief excerpts; the reply is parsed strictly, validated, and written with `repo.Update`, moving each step to the drafted stage. Shared plumbing from M2's carry-forward list lands first: candidate-based JSON parsing and one ask-parse-retry helper.

**Tech Stack:** Go (module `gophermind/gophermind-lib`), standard library, the existing `plantree`, `lockfile` and `llm` packages.

**Spec:** `docs/superpowers/specs/2026-09-19-brief-workflow-design.md` (approved design), `docs/superpowers/plans/2026-09-19-brief-workflow-roadmap.md` (M1 and M2 outcomes and the carry-forward decisions this plan implements).

## Global Constraints

- All commands run from `/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com/gophermind-lib`.
- Test command: `go test ./plantree/... -count=1` (about 12 seconds). Fast loop: add `-short`. Race check: `go test -race ./plantree/... -short -count=1`.
- Run `gofmt -w plantree` before every check, then `gofmt -l plantree` must print nothing. `go vet ./plantree/...` and `go build ./...` must be clean.
- Package `plantree/plan` may import `plantree`, `lockfile` and `llm` only. Nothing in `plantree` may import `phaseflow`.
- The code and tests in this plan were written and run green, task by task in this order, before the plan was generated. Copy them exactly. If a real compile or vet error appears, fix it minimally and disclose the fix in your report. If a test fails, report BLOCKED with specifics instead of editing the test.
- Three existing files are modified (`pass1json.go`, `merge.go`, `runner.go`); for those the plan names the exact region to replace. Everything else is a new file.
- Do NOT run `git switch`, `git checkout`, `git branch`, `git reset` or `git stash`, and do not use `--gw-force`: a git wrapper blocks them. Only `git add`, `git commit`, `git status`, `git diff` and `git log` are needed.
- No em dashes and no emojis in code, comments or commit messages.
- Commit messages end with these two lines:
  `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr`

## Decisions made for M3

1. **Pass 2 keeps no cursor.** Steps at stage skeleton or inspected, not on hold, are the work list. Calling `RunPass2` again after any error continues with what is left. A step moves to drafted only when its whole specification is written and valid.
2. **Provenance is a sidecar,** `_state/provenance.json`, mapping chunk index to node ids, not a node field: `plantree.Node` rejects unknown fields and pins its schema version. Pass 1 writes it after the merge and before the cursor moves, replacing a chunk's list on replay.
3. **Dependencies stay inside a task and point backward.** A step may depend only on an earlier-numbered step of the same task, which makes a cycle impossible by construction. Cross-task dependencies are out of scope.
4. **Bounded input.** At most `StepsPerPass` steps per call (default 6); brief excerpts at most `BriefBytes` (default 6000), whole chunks while they fit and the first one that does not is cut and marked (excerpts are context, not requirements); the list of the task's steps is capped at 4000 bytes. The worst-case prompt with the defaults is about 25,000 bytes (a test pins it below 26,000).
5. **Stage and status.** A specified step goes to stage drafted; its status stays untouched. `reviewed` comes from approval (M6).
6. **Resolved from the M2 carry-forward list:** `ReadBrief` and `BriefChunks` are exported; a shared `askJSON` replaces the inline retry loop; JSON extraction tries every candidate object so prose or a stray `{}` before the real object cannot hide it (`parseFirst`); `Merge` documents that its input must come from `ParsePass1`.
7. **Out of scope:** questions (M4), re-planning a specified step (M5), any UI, wiring into `/project` (M6), cross-task dependencies, a run lock (M6).

---

## File structure

| File | Responsibility |
|---|---|
| `plantree/plan/pass1json.go` (modify) | add `candidates`, `parseFirst`; `ParsePass1` uses them |
| `plantree/plan/runner.go` (modify) | add `askJSON`; `runChunk` uses it, then records provenance |
| `plantree/plan/merge.go` (modify) | add `mergeTracked` (Merge wraps it) |
| `plantree/plan/provenance.go` | chunk to node-id sidecar: `recordProvenance`, `loadProvenance`, `chunksFor` |
| `plantree/plan/brief.go` | `ReadBrief`, `BriefChunks`, `Excerpts` |
| `plantree/plan/pass2json.go` | `Pass2Output`, `ParsePass2`, validation, `safeRelPath` |
| `plantree/plan/prompt2.go` | `Pass2Prompt`, pass-2 defaults, the bounded step list |
| `plantree/plan/runner2.go` | `Options2`, `Result2`, `RunPass2`, `pendingWork`, `applySpecs` |

---
### Task 1: Find the real JSON object (parseFirst)

**Files:**
- Modify: `gophermind-lib/plantree/plan/pass1json.go` (replace `ParsePass1`)
- Modify: `gophermind-lib/plantree/plan/pass1json_test.go` (append three tests)

**Interfaces:**
- Consumes: `balancedEnd` (existing), `validatePass1` (existing).
- Produces:
  - `func candidates(reply string) (list []string, seen bool)`
  - `func parseFirst[T any](reply string, decode func(raw string) (T, error)) (T, error)`
  - `func decodePass1(raw string) (Pass1Output, error)`
  - `ParsePass1(reply string) (Pass1Output, error)` unchanged in signature; it now returns the first candidate object that decodes and validates, and otherwise the first candidate's error.
  - `ExtractJSON` is unchanged and stays exported (existing tests use it); `ParsePass1` no longer calls it.

- [ ] **Step 1: Write the failing tests**

Append to `plantree/plan/pass1json_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: candidates`.

- [ ] **Step 3: Write the implementation**

In `plantree/plan/pass1json.go`, replace the function `ParsePass1` (its doc comment through its closing brace, which sits directly above `// NormalizeTitle is the key`) with:

```go
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

```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier `ParsePass1` and `ExtractJSON` tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/pass1json.go gophermind-lib/plantree/plan/pass1json_test.go
git commit -m "refactor(plan): parse the first candidate object that is valid

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 2: One ask-parse-retry helper (askJSON)

**Files:**
- Modify: `gophermind-lib/plantree/plan/runner.go` (replace `runChunk`, add `askJSON`)
- Test: `gophermind-lib/plantree/plan/askjson_test.go` (new)

**Interfaces:**
- Consumes: `Completer`, `RetryPrompt` (existing), the `fake` test type from `runner_test.go`.
- Produces: `func askJSON[T any](ctx context.Context, c Completer, prompt string, parse func(reply string) (T, error)) (T, error)` (asks once; on a parse failure asks once more with the rejection reason and an excerpt of the bad reply; transport errors are returned unchanged; a second parse failure returns an error containing "rejected twice").

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/askjson_test.go`:

```go
package plan

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func parseNumber(reply string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(reply))
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", reply)
	}
	return n, nil
}

func TestAskJSONReturnsAGoodFirstReply(t *testing.T) {
	f := &fake{reply: func(int, string) (string, error) { return "7", nil }}
	n, err := askJSON(context.Background(), f, "PROMPT", parseNumber)
	if err != nil || n != 7 || len(f.prompts) != 1 {
		t.Errorf("n=%d err=%v calls=%d", n, err, len(f.prompts))
	}
}

func TestAskJSONRetriesOnceShowingTheBadReply(t *testing.T) {
	f := &fake{reply: func(n int, _ string) (string, error) {
		if n == 0 {
			return "seven", nil
		}
		return "7", nil
	}}
	n, err := askJSON(context.Background(), f, "PROMPT", parseNumber)
	if err != nil || n != 7 || len(f.prompts) != 2 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, len(f.prompts))
	}
	if !strings.Contains(f.prompts[1], "PROMPT") || !strings.Contains(f.prompts[1], "rejected") || !strings.Contains(f.prompts[1], "seven") {
		t.Errorf("the retry prompt must carry the original, the reason and the bad reply: %q", f.prompts[1])
	}
}

func TestAskJSONGivesUpAfterTwoRejections(t *testing.T) {
	f := &fake{reply: func(int, string) (string, error) { return "nope", nil }}
	_, err := askJSON(context.Background(), f, "PROMPT", parseNumber)
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || len(f.prompts) != 2 {
		t.Errorf("err=%v calls=%d", err, len(f.prompts))
	}
}

func TestAskJSONReturnsTransportErrorsUnchanged(t *testing.T) {
	boom := errors.New("connection refused")
	f := &fake{reply: func(int, string) (string, error) { return "", boom }}
	if _, err := askJSON(context.Background(), f, "P", parseNumber); !errors.Is(err, boom) || len(f.prompts) != 1 {
		t.Errorf("first call: err=%v calls=%d", err, len(f.prompts))
	}
	f2 := &fake{reply: func(n int, _ string) (string, error) {
		if n == 0 {
			return "bad", nil
		}
		return "", boom
	}}
	if _, err := askJSON(context.Background(), f2, "P", parseNumber); !errors.Is(err, boom) || len(f2.prompts) != 2 {
		t.Errorf("retry call: err=%v calls=%d", err, len(f2.prompts))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: askJSON`.

- [ ] **Step 3: Write the implementation**

In `plantree/plan/runner.go`, replace everything from the comment line `// runChunk performs one skeleton pass` to the end of the file with:

```go
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

// runChunk performs one skeleton pass: build the prompt, ask, merge, refresh
// the overview.
func runChunk(ctx context.Context, repo *plantree.Repo, c Completer, opt Options, chunk Chunk, total int) (Created, error) {
	overview, err := ReadOverview(repo.Dir())
	if err != nil {
		return Created{}, err
	}
	outline, err := Outline(repo)
	if err != nil {
		return Created{}, err
	}
	prompt := Pass1Prompt(opt.ProjectName, overview, outline, chunk, total)

	out, err := askJSON(ctx, c, prompt, ParsePass1)
	if err != nil {
		return Created{}, err
	}

	created, err := Merge(repo, out)
	if err != nil {
		return created, err
	}
	text := out.Overview
	if len(text) > opt.OverviewCap {
		if shorter, cerr := c.Complete(ctx, CompressPrompt(FitOverview(text, 4*opt.OverviewCap), opt.OverviewCap)); cerr == nil && shorter != "" {
			text = shorter
		}
		text = FitOverview(text, opt.OverviewCap)
	}
	if err := WriteOverview(repo.Dir(), text); err != nil {
		return created, err
	}
	return created, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier runner tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/runner.go gophermind-lib/plantree/plan/askjson_test.go
git commit -m "refactor(plan): share one ask-parse-retry helper

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 3: Provenance and the stored brief

**Files:**
- Modify: `gophermind-lib/plantree/plan/merge.go` (replace `Merge`)
- Modify: `gophermind-lib/plantree/plan/merge_test.go` (append one test)
- Modify: `gophermind-lib/plantree/plan/runner.go` (replace `runChunk` again)
- Create: `gophermind-lib/plantree/plan/provenance.go`, `provenance_test.go`, `brief.go`, `brief_test.go`

**Interfaces:**
- Consumes: `Merge` internals (`ensureChild`), `loadState`, `statePath`, `briefFile`, `SplitBrief`, `cutPoint`, `lockfile.WriteAtomic`.
- Produces:
  - `func mergeTracked(repo *plantree.Repo, out Pass1Output) (Created, []string, error)` (also returns the ids of every node created or reused, in order, without repeats); `Merge` now wraps it.
  - `func recordProvenance(repo *plantree.Repo, chunk int, ids []string) error` (replaces the chunk's list), `func loadProvenance(repo *plantree.Repo) (provenance, error)`, `func (p provenance) chunksFor(ids []string) []int` (brief order)
  - `func ReadBrief(repo *plantree.Repo) (string, error)`
  - `func BriefChunks(repo *plantree.Repo) ([]Chunk, error)` (the stored brief cut exactly as the run cut it; wraps `os.ErrNotExist` when there is no stored brief or state)
  - `func Excerpts(chunks []Chunk, idx []int, budget int) string` (at most `budget` bytes; whole chunks while they fit, the first that does not is cut and marked)
  - `runChunk` now records provenance after the merge and before the overview write.

- [ ] **Step 1: Write the failing tests**

Append to `plantree/plan/merge_test.go`:

```go
func TestMergeTrackedListsCreatedAndReusedNodesOnce(t *testing.T) {
	r := newRepo(t)
	_, first, err := mergeTracked(r, sampleOut())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-001", "phase-001.task-001.step-002"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("touched = %v, want %v", first, want)
	}
	again := sampleOut()
	again.Phases[0].Tasks[0].Steps = []StepOut{{Title: "Add lint", Digest: "style"}}
	c, second, err := mergeTracked(r, again)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"phase-001", "phase-001.task-001", "phase-001.task-001.step-003"}
	if c != (Created{Steps: 1}) || !reflect.DeepEqual(second, want) {
		t.Errorf("Created=%+v touched=%v, want one new step and %v", c, second, want)
	}
}
```

Create `plantree/plan/provenance_test.go`:

```go
package plan

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func TestProvenanceRoundTripAndReplace(t *testing.T) {
	r := newRepo(t)
	if p, err := loadProvenance(r); err != nil || len(p.Chunks) != 0 {
		t.Fatalf("missing file: %+v, %v", p, err)
	}
	if err := recordProvenance(r, 0, []string{"phase-001", "phase-001.task-001"}); err != nil {
		t.Fatal(err)
	}
	if err := recordProvenance(r, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := recordProvenance(r, 0, []string{"phase-002"}); err != nil { // a replayed chunk replaces its list
		t.Fatal(err)
	}
	p, err := loadProvenance(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"0": {"phase-002"}, "1": {}}
	if !reflect.DeepEqual(p.Chunks, want) {
		t.Errorf("Chunks = %v, want %v", p.Chunks, want)
	}
}

func TestProvenanceCorruptFileSaysHowToRecover(t *testing.T) {
	r := newRepo(t)
	if err := os.MkdirAll(r.Dir()+"/_state", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provenancePath(r), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProvenance(r); err == nil || !strings.Contains(err.Error(), "delete that file") {
		t.Errorf("err = %v", err)
	}
}

func TestChunksForReturnsBriefOrder(t *testing.T) {
	p := provenance{Chunks: map[string][]string{"2": {"x"}, "0": {"y", "x"}, "5": {"z"}, "10": {"x"}, "bad": {"x"}}}
	if got := p.chunksFor([]string{"x"}); !reflect.DeepEqual(got, []int{0, 2, 10}) {
		t.Errorf("chunksFor = %v, want [0 2 10]", got)
	}
	if got := p.chunksFor([]string{"nothing"}); len(got) != 0 {
		t.Errorf("chunksFor(nothing) = %v", got)
	}
}

func TestRunPass1RecordsWhichNodesEachChunkProduced(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	p, err := loadProvenance(r)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"0": {"phase-001", "phase-001.task-001", "phase-001.task-001.step-001"},
		"1": {"phase-001", "phase-001.task-002", "phase-001.task-002.step-001"},
		"2": {"phase-002", "phase-002.task-001", "phase-002.task-001.step-001"},
	}
	if !reflect.DeepEqual(p.Chunks, want) {
		t.Errorf("Chunks = %v", p.Chunks)
	}
}
```

Create `plantree/plan/brief_test.go`:

```go
package plan

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func TestReadBriefAndBriefChunksMatchTheRun(t *testing.T) {
	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	brief, err := ReadBrief(r)
	if err != nil || brief != threePartBrief {
		t.Fatalf("ReadBrief = %q, %v", brief, err)
	}
	chunks, err := BriefChunks(r)
	if err != nil || len(chunks) != 3 || !strings.Contains(chunks[1].Text, "second part text") {
		t.Fatalf("BriefChunks = %v, %v", chunks, err)
	}
	if joined(chunks) != threePartBrief {
		t.Error("the chunks must reproduce the brief")
	}
}

func TestBriefChunksWithoutARunWrapsNotExist(t *testing.T) {
	r := newRepo(t)
	if _, err := BriefChunks(r); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist", err)
	}
}

func TestExcerpts(t *testing.T) {
	chunks := []Chunk{
		{Index: 0, Text: "zero text\n"},
		{Index: 1, Text: "one text\n"},
		{Index: 2, Text: "two text\n"},
	}
	got := Excerpts(chunks, []int{0, 2}, 10000)
	if !strings.Contains(got, "[part 1 of 3]") || !strings.Contains(got, "zero text") ||
		!strings.Contains(got, "[part 3 of 3]") || strings.Contains(got, "one text") {
		t.Errorf("Excerpts = %q", got)
	}
	if got := Excerpts(chunks, nil, 10000); got != "" {
		t.Errorf("no indexes: %q", got)
	}
	if got := Excerpts(chunks, []int{-1, 9, 1}, 10000); !strings.Contains(got, "one text") || strings.Contains(got, "zero") {
		t.Errorf("out-of-range indexes must be skipped: %q", got)
	}
}

func TestExcerptsCutsTheChunkThatDoesNotFitAndStaysInBudget(t *testing.T) {
	long := strings.Repeat("line of brief text\n\n", 40) // 800 bytes
	chunks := []Chunk{{Index: 0, Text: "short\n"}, {Index: 1, Text: long}, {Index: 2, Text: "never reached\n"}}
	got := Excerpts(chunks, []int{0, 1, 2}, 300)
	if len(got) > 300 || !strings.Contains(got, "short") || !strings.Contains(got, "[excerpt cut to fit the budget]") || strings.Contains(got, "never reached") {
		t.Errorf("len=%d %q", len(got), got)
	}
	if got := Excerpts(chunks, []int{1}, 50); got != "" {
		t.Errorf("a budget too small to be useful must give nothing, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: mergeTracked` (and `recordProvenance`, `ReadBrief`, `Excerpts`).

- [ ] **Step 3: Write the implementation**

In `plantree/plan/merge.go`, replace the function `Merge` (its doc comment through its closing brace, directly above `// ensureChild returns the id`) with:

```go
// Merge applies a skeleton pass to the tree. A proposed node whose title
// (ignoring case and spacing) matches an existing sibling is reused unchanged,
// so replaying the same pass after a crash adds nothing twice. New nodes start
// as untouched skeletons. The output must come from ParsePass1: Merge does not
// re-validate it.
func Merge(repo *plantree.Repo, out Pass1Output) (Created, error) {
	created, _, err := mergeTracked(repo, out)
	return created, err
}

// mergeTracked is Merge that also returns the ids of every node the pass
// created or reused, in order and without repeats. RunPass1 records them so
// pass 2 can show a task the part of the brief that produced it.
func mergeTracked(repo *plantree.Repo, out Pass1Output) (Created, []string, error) {
	var created Created
	var touched []string
	seen := map[string]bool{}
	note := func(id string) {
		if !seen[id] {
			seen[id] = true
			touched = append(touched, id)
		}
	}
	for _, p := range out.Phases {
		id, made, err := ensureChild(repo, plantree.RootID, p.Title, p.Digest, p.Objective)
		if err != nil {
			return created, touched, err
		}
		note(id)
		if made {
			created.Phases++
		}
		for _, t := range p.Tasks {
			tid, made, err := ensureChild(repo, id, t.Title, t.Digest, t.Objective)
			if err != nil {
				return created, touched, err
			}
			note(tid)
			if made {
				created.Tasks++
			}
			for _, s := range t.Steps {
				sid, made, err := ensureChild(repo, tid, s.Title, s.Digest, "")
				if err != nil {
					return created, touched, err
				}
				note(sid)
				if made {
					created.Steps++
				}
			}
		}
	}
	return created, touched, nil
}

```

In `plantree/plan/runner.go`, replace everything from the comment line `// runChunk performs one skeleton pass` to the end of the file with:

```go
// runChunk performs one skeleton pass: build the prompt, ask, merge, record
// which nodes the chunk produced, refresh the overview.
func runChunk(ctx context.Context, repo *plantree.Repo, c Completer, opt Options, chunk Chunk, total int) (Created, error) {
	overview, err := ReadOverview(repo.Dir())
	if err != nil {
		return Created{}, err
	}
	outline, err := Outline(repo)
	if err != nil {
		return Created{}, err
	}
	prompt := Pass1Prompt(opt.ProjectName, overview, outline, chunk, total)

	out, err := askJSON(ctx, c, prompt, ParsePass1)
	if err != nil {
		return Created{}, err
	}

	created, touched, err := mergeTracked(repo, out)
	if err != nil {
		return created, err
	}
	if err := recordProvenance(repo, chunk.Index, touched); err != nil {
		return created, err
	}
	text := out.Overview
	if len(text) > opt.OverviewCap {
		if shorter, cerr := c.Complete(ctx, CompressPrompt(FitOverview(text, 4*opt.OverviewCap), opt.OverviewCap)); cerr == nil && shorter != "" {
			text = shorter
		}
		text = FitOverview(text, opt.OverviewCap)
	}
	if err := WriteOverview(repo.Dir(), text); err != nil {
		return created, err
	}
	return created, nil
}
```

Create `plantree/plan/provenance.go`:

```go
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

const provenanceFile = "provenance.json"

// provenance records which nodes each chunk of the brief produced or reused, so
// pass 2 can show a task the part of the brief that gave rise to it. It lives
// beside the tree rather than in the nodes, whose schema is fixed.
type provenance struct {
	Chunks map[string][]string `json:"chunks"` // chunk index (decimal) to node ids
}

func provenancePath(repo *plantree.Repo) string {
	return filepath.Join(repo.Dir(), "_state", provenanceFile)
}

func loadProvenance(repo *plantree.Repo) (provenance, error) {
	b, err := os.ReadFile(provenancePath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return provenance{Chunks: map[string][]string{}}, nil
	}
	if err != nil {
		return provenance{}, err
	}
	var p provenance
	if err := json.Unmarshal(b, &p); err != nil {
		return provenance{}, fmt.Errorf("plan: reading %s (delete that file to drop the brief excerpts pass 2 shows): %w", provenancePath(repo), err)
	}
	if p.Chunks == nil {
		p.Chunks = map[string][]string{}
	}
	return p, nil
}

// recordProvenance sets the ids for one chunk, replacing any earlier list, so
// replaying a chunk after a crash leaves the same record.
func recordProvenance(repo *plantree.Repo, chunk int, ids []string) error {
	p, err := loadProvenance(repo)
	if err != nil {
		return err
	}
	if ids == nil {
		ids = []string{}
	}
	p.Chunks[strconv.Itoa(chunk)] = ids
	if err := os.MkdirAll(filepath.Dir(provenancePath(repo)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return lockfile.WriteAtomic(provenancePath(repo), append(b, '\n'), 0o644)
}

// chunksFor returns, in brief order, the indexes of the chunks that produced or
// reused any of ids.
func (p provenance) chunksFor(ids []string) []int {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []int
	for key, list := range p.Chunks {
		n, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		for _, id := range list {
			if want[id] {
				out = append(out, n)
				break
			}
		}
	}
	sort.Ints(out)
	return out
}
```

Create `plantree/plan/brief.go`:

```go
package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

const excerptCutMarker = "\n[excerpt cut to fit the budget]\n"

// ReadBrief returns the brief RunPass1 stored beside the tree. A resumed run
// must be given a brief identical to it.
func ReadBrief(repo *plantree.Repo) (string, error) {
	b, err := os.ReadFile(filepath.Join(repo.Dir(), briefFile))
	return string(b), err
}

// BriefChunks returns the stored brief cut exactly as the run cut it (same
// chunk size), so a chunk index means the same text as when provenance was
// recorded. It wraps os.ErrNotExist when there is no stored brief or no run
// state.
func BriefChunks(repo *plantree.Repo) ([]Chunk, error) {
	brief, err := ReadBrief(repo)
	if err != nil {
		return nil, err
	}
	st, found, err := loadState(repo)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("plan: no pass-1 state in %s: %w", statePath(repo), os.ErrNotExist)
	}
	return SplitBrief(brief, st.ChunkBytes), nil
}

// Excerpts joins the chunks at idx (in the order given) into one block of at
// most budget bytes. Whole chunks are used while they fit; the first one that
// does not is cut at a paragraph or line end and marked. Excerpts are context
// for a model, not requirements, so cutting is allowed here.
func Excerpts(chunks []Chunk, idx []int, budget int) string {
	var b strings.Builder
	for _, i := range idx {
		if i < 0 || i >= len(chunks) {
			continue
		}
		room := budget - b.Len()
		head := fmt.Sprintf("[part %d of %d]\n", i+1, len(chunks))
		room -= len(head)
		if room < 100 {
			break
		}
		text := strings.TrimRight(chunks[i].Text, "\n")
		if len(text)+2 <= room {
			b.WriteString(head)
			b.WriteString(text)
			b.WriteString("\n\n")
			continue
		}
		limit := room - len(excerptCutMarker)
		if limit < 1 {
			break
		}
		cut := cutPoint(text, limit)
		b.WriteString(head)
		b.WriteString(strings.TrimRight(text[:cut], "\n"))
		b.WriteString(excerptCutMarker)
		break
	}
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean. All earlier tests still pass.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/merge.go gophermind-lib/plantree/plan/merge_test.go gophermind-lib/plantree/plan/runner.go gophermind-lib/plantree/plan/provenance.go gophermind-lib/plantree/plan/provenance_test.go gophermind-lib/plantree/plan/brief.go gophermind-lib/plantree/plan/brief_test.go
git commit -m "feat(plan): record which brief chunk produced each node, and export the stored brief

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 4: Parse and validate a specification reply

**Files:**
- Create: `gophermind-lib/plantree/plan/pass2json.go`
- Test: `gophermind-lib/plantree/plan/pass2json_test.go`

**Interfaces:**
- Consumes: `parseFirst` (Task 1), `clip`, `segmentNumber`, `oneLine` (existing).
- Produces:
  - `type StepSpecOut struct{ ID, Description string; TargetPaths, AcceptanceCriteria, TestCommand, DependsOn []string }` (JSON: `id`, `description`, `target_paths`, `acceptance_criteria`, `test_command`, `depends_on`), `type Pass2Output struct{ Steps []StepSpecOut }`
  - `func ParsePass2(reply string, batch, siblings []string) (Pass2Output, error)` (the reply must cover exactly the `batch` ids, once each; `depends_on` may name only earlier-numbered ids from `siblings`; errors are bounded and specific enough to send back to the model)
  - package-private `checkSpec`, `checkDeps`, `safeRelPath`, `setOf`

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/pass2json_test.go`:

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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: StepSpecOut` (and `ParsePass2`).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/pass2json.go`:

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
	Steps []StepSpecOut `json:"steps"`
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
	seen := map[string]bool{}
	for _, s := range out.Steps {
		id := clip(s.ID)
		if !want[s.ID] {
			return fmt.Errorf("step %q is not one of the steps asked for", id)
		}
		if seen[s.ID] {
			return fmt.Errorf("step %q appears more than once", id)
		}
		seen[s.ID] = true
		where := fmt.Sprintf("step %q", id)
		if err := checkSpec(where, s, sibling); err != nil {
			return err
		}
	}
	for _, id := range batch {
		if !seen[id] {
			return fmt.Errorf("step %q was asked for but is missing from the reply", clip(id))
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/pass2json.go gophermind-lib/plantree/plan/pass2json_test.go
git commit -m "feat(plan): strict parsing and validation of step specifications

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 5: The specification prompt

**Files:**
- Create: `gophermind-lib/plantree/plan/prompt2.go`
- Test: `gophermind-lib/plantree/plan/prompt2_test.go`

**Interfaces:**
- Consumes: `orNone`, `oneLine` (existing), `Excerpts` (Task 3), `newSkeleton` (existing, used by the test), `plantree.Node`.
- Produces:
  - `const defaultStepsPerPass = 6`, `const defaultBriefBytes = 6000`, `const siblingListCapBytes = 4000`
  - `func Pass2Prompt(project, overview string, phase, task plantree.Node, siblings, batch []plantree.Node, excerpts string) string` (carries the overview, the phase, the task, the task's step list, the steps to specify now, optional brief excerpts, the rules and the JSON shape; nothing about any other task)
  - `TestPass2PromptWorstCaseSize` pins the largest prompt the defaults can produce below 26,000 bytes.

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/prompt2_test.go`:

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
	p := Pass2Prompt("demo", "an overview", phase, task, []plantree.Node{st1, st2}, []plantree.Node{st2}, "[part 1 of 1]\nthe brief text")
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
	p := Pass2Prompt("demo", "", phase, task, []plantree.Node{st}, []plantree.Node{st}, "")
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
	p := Pass2Prompt("demo", "", phase, task, steps, steps[:1], "")
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
	p := Pass2Prompt(strings.Repeat("n", 100), overview, phase, task, steps, steps[:defaultStepsPerPass], excerpts)
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
	p := Pass2Prompt(r(100), overview, phase, task, steps, steps[:defaultStepsPerPass], excerpts)
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
	p := Pass2Prompt("demo", "", phase, task, []plantree.Node{st}, []plantree.Node{st}, "")
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
	p := Pass2Prompt("demo", "", phase, task, []plantree.Node{skipped, drafted, skel}, []plantree.Node{skel}, "")
	for _, want := range []string{
		"step-001: One [on hold: skipped]", "step-002: Two [specified]", "step-003: Three [to specify]",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: Pass2Prompt`.

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/prompt2.go`:

```go
package plan

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

// Pass-2 defaults, sized so the worst-case prompt stays near 25,000 bytes.
const (
	defaultStepsPerPass = 6
	defaultBriefBytes   = 6000
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

// Pass2Prompt builds the prompt for one specification pass. It carries the
// overview, the phase and task the steps belong to, the list of the task's
// steps, the steps to specify now, and optionally excerpts of the brief. It
// carries nothing about any other task.
func Pass2Prompt(project, overview string, phase, task plantree.Node, siblings, batch []plantree.Node, excerpts string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing the work specification for some steps of ONE task in a project plan for %q. You see only this task.\n\n", fit(project, 100))
	b.WriteString("Running overview of the whole project:\n")
	b.WriteString(orNone(overview))
	fmt.Fprintf(&b, "\n\nPhase: %s\nWhy: %s\nObjective: %s\n", fit(phase.Title, 200), fit(phase.ContextDigest, 500), orNone(fit(phase.Objective, 1000)))
	fmt.Fprintf(&b, "\nTask: %s\nWhy: %s\nObjective: %s\n", fit(task.Title, 200), fit(task.ContextDigest, 500), orNone(fit(task.Objective, 1000)))
	b.WriteString("\nAll steps of this task, in order. A step may depend only on an EARLIER step in this list:\n")
	b.WriteString(stepList(siblings))
	b.WriteString("\nSteps to specify now:\n")
	for _, s := range batch {
		fmt.Fprintf(&b, "- %s: %s. Why: %s\n", s.ID, fit(s.Title, 200), fit(s.ContextDigest, 500))
	}
	b.WriteString("\nBrief excerpts that produced this task (context only, may be partial):\n")
	if strings.TrimSpace(excerpts) == "" {
		b.WriteString("(not available)\n")
	} else {
		fmt.Fprintf(&b, "<<<BRIEF EXCERPTS\n%s\nBRIEF EXCERPTS>>>\n", excerpts)
	}
	b.WriteString("\nRules:\n")
	b.WriteString("- Return exactly the steps listed under \"Steps to specify now\", each once, using its id.\n")
	b.WriteString("- description: what to build or change, concrete enough that an agent can start without asking.\n")
	b.WriteString("- target_paths: repository-relative files or directories the step touches. Never absolute, never containing \"..\".\n")
	b.WriteString("- acceptance_criteria: 1 to 10 checks a reviewer can verify.\n")
	b.WriteString("- test_command: the command as an array of arguments that verifies the step, or an empty array if there is none.\n")
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/prompt2.go gophermind-lib/plantree/plan/prompt2_test.go
git commit -m "feat(plan): bounded prompt for specifying a task's steps

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 6: RunPass2

**Files:**
- Create: `gophermind-lib/plantree/plan/runner2.go`
- Test: `gophermind-lib/plantree/plan/runner2_test.go` (also defines `pass2Reply`, `specFake`, `planned`, `actionKinds`, `stepsToSpecify`, which Task 7 reuses)

**Interfaces:**
- Consumes: everything from Tasks 1 to 5, `askJSON`, `Completer`, `plantree.Repo` (`Get`, `Update`, `Walk`, `Children`), `llm.ContextLimitFromError`.
- Produces:
  - `type Options2 struct{ ProjectName string; StepsPerPass, BriefBytes int }`
  - `type Result2 struct{ Tasks, Steps, Passes int }`
  - `func RunPass2(ctx context.Context, repo *plantree.Repo, c Completer, opt Options2) (Result2, error)` (specifies every step still at stage skeleton or inspected and not on hold; derives the work from the tree so a second call resumes; writes the specification and moves each step to drafted only when the whole reply is valid)
  - package-private `taskWork`, `needsSpec`, `pendingWork`, `idsOf`, `taskError`, `applySpecs`

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/runner2_test.go`:

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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: RunPass2` (and `Options2`, `Result2`).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/runner2.go`:

```go
package plan

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/plantree"
)

// Options2 tunes RunPass2. Zero values pick the defaults.
type Options2 struct {
	ProjectName string // default: the root node's title
	// StepsPerPass bounds how many steps one model call specifies (default 6),
	// so the reply stays small however large a task is.
	StepsPerPass int
	// BriefBytes bounds the brief excerpts shown with a task (default 6000).
	// With the defaults one pass is at most about 25,000 bytes: BriefBytes +
	// OverviewCapBytes + 4000 (step list) + about 8,000 (phase, task and the
	// steps being specified) + 2,500 (instructions). At 3 to 4 bytes per token
	// that must leave room for the reply inside the model's window.
	BriefBytes int
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
			prompt := Pass2Prompt(opt.ProjectName, overview, w.phase, w.task, w.steps, batch, excerpts)
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

- [ ] **Step 4: Run the tests, including the race detector**

Run: `gofmt -w plantree && go test ./plantree/... -count=1 && go test -race ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: all `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/runner2.go gophermind-lib/plantree/plan/runner2_test.go
git commit -m "feat(plan): resumable pass 2 that specifies every step

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 7: Both passes end to end over HTTP

**Files:**
- Test: `gophermind-lib/plantree/plan/integration2_test.go` (new)

**Interfaces:**
- Consumes: `RunPass1`, `RunPass2`, `ClientCompleter`, and the test helpers `writeSSE` (`integration_test.go`), `byChunk`, `threePartBrief`, `opts` (`runner_test.go`), `pass2Reply`, `actionKinds` (`runner2_test.go`).
- Produces: `TestPass1ThenPass2EndToEndOverHTTP`, which runs both passes through the real completer against a fake model server and checks that only approval is left.

- [ ] **Step 1: Write the test**

Create `plantree/plan/integration2_test.go`:

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

// TestPass1ThenPass2EndToEndOverHTTP runs both passes through the real
// completer against a fake model server: the brief becomes a skeleton, every
// step gets a specification, and only approval is left.
func TestPass1ThenPass2EndToEndOverHTTP(t *testing.T) {
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
		reply, err := byChunk(0, body)
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
	if _, err := RunPass1(context.Background(), repo, threePartBrief, c, opts); err != nil {
		t.Fatal(err)
	}
	res, err := RunPass2(context.Background(), plantree.Open(dir), c, Options2{})
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result2{Tasks: 3, Steps: 3, Passes: 3}) {
		t.Errorf("Result2 = %+v", res)
	}
	if got := actionKinds(t, repo); got != "[approve:plan]" {
		t.Errorf("NextActions = %s, want only approval", got)
	}
	for _, id := range []string{"phase-001.task-001.step-001", "phase-001.task-002.step-001", "phase-002.task-001.step-001"} {
		n, err := repo.Get(id)
		if err != nil || n.Work == nil || n.Planning.Stage != plantree.StageDrafted {
			t.Errorf("%s = %+v, %v", id, n, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 6 {
		t.Fatalf("%d requests, want 6 (three per pass)", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d carries tools", i)
		}
	}
	first := bodies[3] // the first pass-2 request: task 1, whose brief is chunk 1
	if !strings.Contains(first, "first part text") || strings.Contains(first, "second part text") || strings.Contains(first, "third part text") {
		t.Error("the first pass-2 request must carry only the brief chunk that produced its task")
	}
}
```

- [ ] **Step 2: Run it**

Run: `gofmt -w plantree && go test ./plantree/plan/... -run EndToEnd -count=1 -v`
Expected: both end-to-end tests PASS. (This test adds no production code, so it has no failing phase: it checks that Tasks 1 to 6 work together.)

- [ ] **Step 3: Run every check**

Run: `go test ./plantree/... -count=1 && go test -race ./plantree/... -short -count=1 && gofmt -l plantree && go vet ./plantree/... && go build ./...`
Expected: all `ok`, no gofmt output, vet clean, the whole module builds.

- [ ] **Step 4: Commit**

```bash
git add gophermind-lib/plantree/plan/integration2_test.go
git commit -m "test(plan): run both passes against the real completer over HTTP

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

## Self-review (M3)

- **Design coverage:** fresh context per pass (each call is a new two-message conversation), the tree as state (no cursor: pass 2 derives its work list), bounded prompts (step batches, excerpts, step list, worst case pinned at 25 KB), and resume after any error (a step is written only when its whole reply is valid). Questions, re-planning, UI and wiring are M4 to M6 by design.
- **M2 carry-forward resolved here:** `ReadBrief` and `BriefChunks`, node-to-chunk provenance, the shared `askJSON`, candidate-based JSON extraction, `Merge` documented as needing `ParsePass1` output, the pass-2 validator guaranteeing a work description and an acceptance criterion before `Update`.
- **Placeholders:** none. Every step carries full code, and the code was run green task by task in this order before the plan was generated.
- **Types:** `StepSpecOut`, `Pass2Output`, `Options2`, `Result2`, `taskWork` and the helpers are defined once and used with the same names later.
- **Known limits:** a step is specified from the brief chunks that produced it, so a brief cut differently from the recorded run would misattribute excerpts (`BriefChunks` re-cuts with the recorded chunk size, and pass 1 refuses a changed brief or chunk size); dependencies are same-task and backward only; two concurrent `RunPass2` calls can duplicate work (the run lock is M6); `ExtractJSON` remains exported but unused by the parsers.

## Definition of done (M3)

`go test ./plantree/... -count=1`, `go test -race ./plantree/... -short -count=1`, `gofmt -l plantree` (empty), `go vet ./plantree/...` and `go build ./...` all clean; ten commits (seven tasks and three final-review fix commits); a test proves pass 1 followed by pass 2 over the real completer leaves every step drafted and `NextActions` offering only approval, and another proves a pass 2 that fails on the second task resumes in a new process with exactly the tasks that were left.

---

## Amendments after review

The tasks above were built and reviewed as written, then changed by the final whole-branch review. The code blocks in this plan match the committed files. Changes since the plan was first written (commits `5988ec4`, `fa5275e`, `3b7cca5`):

- **Parser errors are bounded.** `decodePass1` and `decodePass2` cut the decoder's message to 400 bytes (a model-supplied unknown key could otherwise return a 200,000 byte error to the caller).
- **`parseFirst` reports the longest candidate's error** (ties go to the earliest) instead of the first, so a stray `{}` before a real but invalid object no longer hides the real defect. `ExtractJSON` carries a note that the parsers use `parseFirst`.
- **The pass-2 prompt is bounded in bytes.** Every variable text field (project name, phase, task, step titles, digests, objectives) is cut in bytes with `fit`, so a CJK or emoji brief cannot make it about twice the size the test pins. Measured worst cases: 25,553 bytes (ASCII) and 25,658 (multibyte), against a 26,000 threshold.
- **The prompt agrees with the parser.** Its rules state every limit the parser enforces, and its JSON shape uses non-empty placeholders and `[]` for `depends_on` (the first shape example was itself rejected by the validator).
- **Held steps.** The step list tags each sibling `[specified]`, `[to specify]` or `[on hold: <status>]`; steps on hold are excluded from the dependency set; `onHold` is shared with `needsSpec`; the in-memory snapshot is updated after each batch so a later batch sees its predecessors as specified.
- **Tasks with no steps are reported.** `Pass1Prompt` says every task needs an objective and at least one step; `EmptyTasks(repo)` lists childless tasks; `Result2.EmptyTasks` counts them (an `int`, so `Result2` stays comparable). `RunPass2` still succeeds when one exists; `NextActions` keeps offering `decompose` for it.
- Files changed outside this plan's blocks: `prompt.go` (one rule line) and `overview_test.go` (its test).

### Carried forward

See the roadmap sections "M3 outcome" and the carry-forward lists for M4 to M6. The first item for M4 is a bounded "project facts" block in the pass-2 prompt (language, build and test commands, file layout), because `test_command` and `target_paths` are otherwise guesses.
