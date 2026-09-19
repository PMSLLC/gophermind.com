# plantree M2: brief to skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read a brief in bounded chunks, one fresh-context model pass per chunk, and build the skeleton plan tree and a terse running overview, resumable after any error.

**Architecture:** A new package `gophermind-lib/plantree/plan`. `SplitBrief` cuts the brief losslessly at headings and paragraph breaks. `RunPass1` asks a `Completer` (fresh context each call, no tools) for one JSON object per chunk, parses it strictly (one correction retry), merges it into the `plantree` tree by title so a replay adds nothing twice, rewrites `overview.md`, and only then advances a small cursor file. The tree and overview hold the real state; the cursor is a hint.

**Tech Stack:** Go (module `gophermind/gophermind-lib`), standard library, the existing `plantree`, `lockfile` and `llm` packages.

**Spec:** `docs/superpowers/specs/2026-09-19-brief-workflow-design.md` (approved design), `docs/superpowers/plans/2026-09-19-brief-workflow-roadmap.md` (milestones, M1 outcome, carry-forward decisions).

## Global Constraints

- All commands run from `/Users/jbrahy/OtherProjects/PMSLLC/gophermind.com/gophermind-lib`.
- Test command: `go test ./plantree/... -count=1` (about 8 seconds; the sibling-limit test creates 999 nodes). Fast loop: add `-short`. Race check: `go test -race ./plantree/... -short -count=1`.
- Run `gofmt -w plantree` before every check, then `gofmt -l plantree` must print nothing. `go vet ./plantree/...` and `go build ./...` must be clean.
- Package `plantree/plan` may import `plantree`, `lockfile` and `llm` only. Nothing in `plantree` may import `phaseflow`.
- The code and tests in this plan were written and run green before the plan was generated. Copy them exactly. If a real compile or vet error appears, fix it minimally and disclose the fix in your report. If a test fails, report BLOCKED with specifics instead of editing the test.
- No em dashes and no emojis in code, comments or commit messages.
- Commit messages end with these two lines:
  `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr`

## Decisions made for M2 (from the roadmap's carry-forward list)

1. **No lock export.** Passes run one at a time in one process, so M2 needs no consistent multi-node read. A second process reading during a run can see a partially merged chunk; the next `NextActions` call is always correct.
2. **Child numbers are max+1 under the parent** (`ensureChild`), not race-safe across processes. Fine for a single writer; `repo.Create` still fails safely with `ErrExists` on a collision.
3. **Digests come from the pass.** Every proposed node must carry a non-empty `digest` (validated), because `context_digest` is required on every node.
4. **A pass uses the model client directly**, not `agent.Agent`: the agent wraps every prompt in the coding-agent system prompt ("call tools, read files first, edit"), which is wrong for a JSON planning pass. `ClientCompleter` sends a two-message conversation with no tool definitions.
5. **Out of scope for M2:** questions (M4), dependencies between nodes, pass 2 specs (M3), any UI, wiring into `/project` (M6).

---

## File structure

| File | Responsibility |
|---|---|
| `plantree/store.go` (modify) | add `Repo.Dir()` |
| `plantree/plan/chunk.go` | `SplitBrief`, lossless bounded chunks |
| `plantree/plan/pass1json.go` | pass output types, `ExtractJSON`, `ParsePass1`, validation, `NormalizeTitle`, `oneLine` |
| `plantree/plan/merge.go` | `Merge`: idempotent title-matched upsert into the tree |
| `plantree/plan/overview.go` | read, write and cap the running overview |
| `plantree/plan/prompt.go` | `Outline`, `Pass1Prompt`, `RetryPrompt`, `CompressPrompt` |
| `plantree/plan/runner.go` | `Completer`, `Options`, `RunPass1`, resume cursor |
| `plantree/plan/clientcompleter.go` | `ClientCompleter`: fresh two-message, no-tool calls |

---
### Task 1: Repo.Dir and the brief chunker

**Files:**
- Modify: `gophermind-lib/plantree/store.go` (add `Dir` before `metaPath`)
- Modify: `gophermind-lib/plantree/store_test.go` (append one test)
- Create: `gophermind-lib/plantree/plan/chunk.go`
- Test: `gophermind-lib/plantree/plan/chunk_test.go`

**Interfaces:**
- Produces:
  - `func (r *Repo) Dir() string` (in package `plantree`)
  - `const DefaultChunkBytes = 12000`
  - `type Chunk struct{ Index int; Title, Text string }`
  - `func SplitBrief(brief string, maxBytes int) []Chunk` (lossless: joining the chunks' `Text` reproduces `brief`)
  - package-private `func cutPoint(rest string, max int) int` (also used by `overview.go`)

- [ ] **Step 1: Write the failing tests**

Append to `plantree/store_test.go`:

```go
func TestDirIsThePlanDirectory(t *testing.T) {
	dir := t.TempDir()
	if got := Open(dir).Dir(); got != filepath.Join(dir, "plan") {
		t.Errorf("Dir() = %q, want %q", got, filepath.Join(dir, "plan"))
	}
}
```

Create `plantree/plan/chunk_test.go`:

```go
package plan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func joined(cs []Chunk) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Text)
	}
	return b.String()
}

const sampleBrief = "Intro paragraph before any heading.\n\n" +
	"# Alpha\nalpha line one\nalpha line two\n\n" +
	"## Beta\nbeta body\n\n" +
	"# Gamma\ngamma body that is a bit longer than the others\n"

func TestSplitBriefIsLosslessAtEveryBudget(t *testing.T) {
	for _, max := range []int{1, 7, 20, 40, 80, 200, 5000} {
		cs := SplitBrief(sampleBrief, max)
		if got := joined(cs); got != sampleBrief {
			t.Errorf("max=%d: chunks do not reproduce the brief:\n%q", max, got)
		}
		for i, c := range cs {
			if c.Index != i {
				t.Errorf("max=%d: chunk %d has Index %d", max, i, c.Index)
			}
			if len(c.Text) > max && max >= 4 {
				t.Errorf("max=%d: chunk %d is %d bytes", max, i, len(c.Text))
			}
			if !utf8.ValidString(c.Text) {
				t.Errorf("max=%d: chunk %d is not valid UTF-8", max, i)
			}
		}
	}
}

func TestSplitBriefPacksSmallSectionsAndBreaksAtHeadings(t *testing.T) {
	one := SplitBrief(sampleBrief, 5000)
	if len(one) != 1 || one[0].Title != "" {
		t.Fatalf("a brief that fits is one chunk with the first section's title, got %d chunks", len(one))
	}
	cs := SplitBrief(sampleBrief, 70)
	if len(cs) < 2 {
		t.Fatalf("expected several chunks, got %d", len(cs))
	}
	for i, c := range cs[1:] {
		if !strings.HasPrefix(c.Text, "#") && !strings.HasPrefix(c.Text, "gamma") && !strings.HasPrefix(c.Text, "alpha") && !strings.HasPrefix(c.Text, "beta") {
			t.Errorf("chunk %d starts mid-sentence: %q", i+1, c.Text)
		}
	}
	titles := map[string]bool{}
	for _, c := range cs {
		titles[c.Title] = true
	}
	for _, want := range []string{"Alpha", "Gamma"} {
		if !titles[want] {
			t.Errorf("no chunk titled %q; titles = %v", want, titles)
		}
	}
}

func TestSplitBriefSplitsAnOversizedSectionAtParagraphs(t *testing.T) {
	para := strings.Repeat("word ", 20) + "\n\n" // 102 bytes
	brief := "# Big\n" + strings.Repeat(para, 6)
	cs := SplitBrief(brief, 250)
	if joined(cs) != brief {
		t.Fatal("not lossless")
	}
	for i, c := range cs {
		if len(c.Text) > 250 {
			t.Errorf("chunk %d is %d bytes", i, len(c.Text))
		}
		if c.Title != "Big" {
			t.Errorf("chunk %d title = %q, want Big", i, c.Title)
		}
		if i < len(cs)-1 && !strings.HasSuffix(c.Text, "\n\n") {
			t.Errorf("chunk %d does not end at a paragraph break: %q", i, c.Text[len(c.Text)-10:])
		}
	}
}

func TestSplitBriefNeverSplitsARune(t *testing.T) {
	brief := strings.Repeat("é", 100) // one 200-byte line, no newlines
	cs := SplitBrief(brief, 31)
	if joined(cs) != brief {
		t.Fatal("not lossless")
	}
	for i, c := range cs {
		if !utf8.ValidString(c.Text) {
			t.Errorf("chunk %d splits a rune", i)
		}
	}
}

func TestSplitBriefEmpty(t *testing.T) {
	for _, in := range []string{"", "   \n\n  "} {
		if cs := SplitBrief(in, 100); cs != nil {
			t.Errorf("SplitBrief(%q) = %v, want nil", in, cs)
		}
	}
}

func TestHeadingTitle(t *testing.T) {
	good := map[string]string{"# A\n": "A", "###### deep\r\n": "deep", "## spaced   title  \n": "spaced   title", "# \n": ""}
	for line, want := range good {
		if got, ok := headingTitle(line); !ok || got != want {
			t.Errorf("headingTitle(%q) = %q, %v; want %q", line, got, ok, want)
		}
	}
	for _, line := range []string{"#nospace\n", "####### seven\n", "text\n", "#\n"} {
		if title, ok := headingTitle(line); ok {
			t.Errorf("headingTitle(%q) accepted a non-heading as %q", line, title)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/... -short -count=1`
Expected: FAIL to build with `undefined: SplitBrief` (package `plan`) and `Open(dir).Dir undefined` (package `plantree`).

- [ ] **Step 3: Write the implementation**

In `plantree/store.go`, add directly above `func (r *Repo) metaPath(`:

```go
// Dir returns the plan directory (<planningDir>/plan), so callers can keep
// sibling documents such as overview.md and brief.md next to the tree.
func (r *Repo) Dir() string { return r.dir }
```

Create `plantree/plan/chunk.go`:

```go
// Package plan turns a brief into the plantree skeleton, one bounded slice at
// a time. Every pass runs in a fresh context and everything durable lives in
// the tree, so a run can stop and resume without conversation history.
package plan

import (
	"strings"
	"unicode/utf8"
)

// DefaultChunkBytes bounds one chunk of the brief (about 3,000 tokens).
const DefaultChunkBytes = 12000

// Chunk is one bounded slice of a brief.
type Chunk struct {
	Index int
	Title string // the nearest heading at the chunk's start, "" before the first
	Text  string
}

type section struct {
	title string
	text  string
}

// SplitBrief splits brief into chunks of at most maxBytes without dropping or
// changing any text: joining the chunks' Text values reproduces brief exactly.
// Headings ("# " to "###### ") start new sections, sections are packed
// together while they fit, and a section larger than maxBytes is split at
// blank lines, then line ends, then rune boundaries. A "#" line inside a code
// fence is treated as a heading; that only changes where chunks break.
func SplitBrief(brief string, maxBytes int) []Chunk {
	if strings.TrimSpace(brief) == "" {
		return nil
	}
	if maxBytes < 1 {
		maxBytes = DefaultChunkBytes
	}

	var secs []section
	for _, line := range strings.SplitAfter(brief, "\n") {
		if line == "" {
			continue
		}
		title, isHeading := headingTitle(line)
		if isHeading || len(secs) == 0 {
			secs = append(secs, section{title: title})
		}
		secs[len(secs)-1].text += line
	}

	var chunks []Chunk
	var buf strings.Builder
	title := ""
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		chunks = append(chunks, Chunk{Index: len(chunks), Title: title, Text: buf.String()})
		buf.Reset()
	}
	for _, s := range secs {
		if len(s.text) > maxBytes {
			flush()
			for _, piece := range splitLarge(s.text, maxBytes) {
				chunks = append(chunks, Chunk{Index: len(chunks), Title: s.title, Text: piece})
			}
			continue
		}
		if buf.Len() > 0 && buf.Len()+len(s.text) > maxBytes {
			flush()
		}
		if buf.Len() == 0 {
			title = s.title
		}
		buf.WriteString(s.text)
	}
	flush()
	return chunks
}

// headingTitle reports whether line is a markdown heading and returns its text.
func headingTitle(line string) (string, bool) {
	t := strings.TrimRight(line, "\r\n")
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(t) || t[n] != ' ' {
		return "", false
	}
	return strings.TrimSpace(t[n+1:]), true
}

// splitLarge cuts text into pieces of at most max bytes.
func splitLarge(text string, max int) []string {
	var out []string
	rest := text
	for len(rest) > max {
		cut := cutPoint(rest, max)
		out = append(out, rest[:cut])
		rest = rest[cut:]
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

// cutPoint picks where to cut the first max bytes of rest, which must be
// longer than max: after the last blank line, else after the last newline,
// else at a rune boundary.
func cutPoint(rest string, max int) int {
	w := rest[:max]
	if i := strings.LastIndex(w, "\n\n"); i > 0 {
		return i + 2
	}
	if i := strings.LastIndex(w, "\n"); i > 0 {
		return i + 1
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(rest[cut]) {
		cut--
	}
	if cut == 0 {
		cut = max
	}
	return cut
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok` for both packages, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/store.go gophermind-lib/plantree/store_test.go gophermind-lib/plantree/plan/chunk.go gophermind-lib/plantree/plan/chunk_test.go
git commit -m "feat(plan): lossless bounded brief chunker and Repo.Dir

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 2: Pass output parsing and validation

**Files:**
- Create: `gophermind-lib/plantree/plan/pass1json.go`
- Test: `gophermind-lib/plantree/plan/pass1json_test.go`

**Interfaces:**
- Produces:
  - `type Pass1Output struct{ Phases []PhaseOut; Overview string }`, `PhaseOut`, `TaskOut`, `StepOut`
  - `func ExtractJSON(reply string) (string, error)`
  - `func ParsePass1(reply string) (Pass1Output, error)` (errors are specific enough to send back to the model)
  - `func NormalizeTitle(s string) string`
  - package-private `func oneLine(s string) string`

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/pass1json_test.go`:

```go
package plan

import (
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: ExtractJSON` (and the other names).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/pass1json.go`:

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
	Phases   []PhaseOut `json:"phases"`
	Overview string     `json:"overview"`
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

// ExtractJSON returns the first complete top-level JSON object in reply,
// ignoring prose and code fences around it. Braces inside strings do not count.
func ExtractJSON(reply string) (string, error) {
	start := strings.IndexByte(reply, '{')
	if start < 0 {
		return "", errors.New("the reply contains no JSON object")
	}
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(reply); i++ {
		c := reply[i]
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
				return reply[start : i+1], nil
			}
		}
	}
	return "", errors.New("the JSON object in the reply is not closed")
}

// ParsePass1 extracts, strictly decodes and validates a skeleton pass reply.
// Its errors are specific enough to send back to the model as a correction.
func ParsePass1(reply string) (Pass1Output, error) {
	raw, err := ExtractJSON(reply)
	if err != nil {
		return Pass1Output{}, err
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	var out Pass1Output
	if err := dec.Decode(&out); err != nil {
		return Pass1Output{}, fmt.Errorf("the JSON does not match the schema: %w", err)
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

func validatePass1(o Pass1Output) error {
	if strings.TrimSpace(o.Overview) == "" {
		return errors.New(`"overview" must be a non-empty string`)
	}
	if len(o.Phases) > maxPhasesPerPass {
		return fmt.Errorf("too many phases (%d, at most %d)", len(o.Phases), maxPhasesPerPass)
	}
	seenPhase := map[string]bool{}
	for _, p := range o.Phases {
		where := fmt.Sprintf("phase %q", p.Title)
		if err := checkNode(where, p.Title, p.Digest, p.Objective, seenPhase); err != nil {
			return err
		}
		if len(p.Tasks) > maxTasksPerPhase {
			return fmt.Errorf("%s has too many tasks (%d, at most %d)", where, len(p.Tasks), maxTasksPerPhase)
		}
		seenTask := map[string]bool{}
		for _, t := range p.Tasks {
			twhere := fmt.Sprintf("task %q in %s", t.Title, where)
			if err := checkNode(twhere, t.Title, t.Digest, t.Objective, seenTask); err != nil {
				return err
			}
			if len(t.Steps) > maxStepsPerTask {
				return fmt.Errorf("%s has too many steps (%d, at most %d)", twhere, len(t.Steps), maxStepsPerTask)
			}
			seenStep := map[string]bool{}
			for _, s := range t.Steps {
				swhere := fmt.Sprintf("step %q in %s", s.Title, twhere)
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/pass1json.go gophermind-lib/plantree/plan/pass1json_test.go
git commit -m "feat(plan): strict parsing and validation of skeleton pass replies

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 3: Merge into the tree

**Files:**
- Create: `gophermind-lib/plantree/plan/merge.go`
- Test: `gophermind-lib/plantree/plan/merge_test.go` (also defines the helpers `newRepo`, `ids`, `sampleOut` that later test files reuse)

**Interfaces:**
- Consumes: `plantree.Repo` (`Children`, `Create`), `plantree.ChildID`, `plantree.ParentRef`, `NormalizeTitle`, `oneLine`, `Pass1Output`.
- Produces:
  - `type Created struct{ Phases, Tasks, Steps int }`
  - `func Merge(repo *plantree.Repo, out Pass1Output) (Created, error)` (idempotent: a proposed node whose normalized title matches an existing sibling is reused unchanged)
  - package-private `ensureChild`, `segmentNumber`, `newSkeleton`

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/merge_test.go`:

```go
package plan

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

func newRepo(t *testing.T) *plantree.Repo {
	t.Helper()
	r := plantree.Open(t.TempDir())
	root := plantree.Node{
		SchemaVersion: plantree.SchemaVersion, ID: plantree.RootID, Title: "demo", NodeRevision: 1,
		ContextDigest: "Plan for demo.", DependsOn: []string{}, Planning: plantree.Planning{Stage: plantree.StageSkeleton},
	}
	if err := r.Init(root); err != nil {
		t.Fatal(err)
	}
	return r
}

func ids(t *testing.T, r *plantree.Repo) []string {
	t.Helper()
	var out []string
	if err := r.Walk(func(n plantree.Node) error { out = append(out, n.ID+" "+n.Title); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

func sampleOut() Pass1Output {
	return Pass1Output{
		Overview: "o",
		Phases: []PhaseOut{{
			Title: "Foundation", Digest: "base", Objective: "set up",
			Tasks: []TaskOut{{
				Title: "Repo layout", Digest: "where code lives",
				Steps: []StepOut{{Title: "Create module", Digest: "needed to compile"}, {Title: "Add CI", Digest: "catch breakage"}},
			}},
		}},
	}
}

func TestMergeBuildsSkeletonTree(t *testing.T) {
	r := newRepo(t)
	c, err := Merge(r, sampleOut())
	if err != nil {
		t.Fatal(err)
	}
	if c != (Created{Phases: 1, Tasks: 1, Steps: 2}) {
		t.Errorf("Created = %+v", c)
	}
	want := []string{
		"plan demo", "phase-001 Foundation", "phase-001.task-001 Repo layout",
		"phase-001.task-001.step-001 Create module", "phase-001.task-001.step-002 Add CI",
	}
	if got := ids(t, r); !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	step, _ := r.Get("phase-001.task-001.step-001")
	if step.Status != plantree.StatusUntouched || step.Planning.Stage != plantree.StageSkeleton || step.ContextDigest != "needed to compile" {
		t.Errorf("step = %+v", step)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	before := ids(t, r)
	c, err := Merge(r, sampleOut())
	if err != nil {
		t.Fatal(err)
	}
	if c != (Created{}) {
		t.Errorf("replay created %+v, want nothing", c)
	}
	if after := ids(t, r); !reflect.DeepEqual(before, after) {
		t.Errorf("replay changed the tree:\n%v\n%v", before, after)
	}
}

func TestMergeMatchesTitlesIgnoringCaseAndSpacing(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, sampleOut()); err != nil {
		t.Fatal(err)
	}
	again := sampleOut()
	again.Phases[0].Title = "  foundation "
	again.Phases[0].Tasks[0].Steps = append(again.Phases[0].Tasks[0].Steps, StepOut{Title: "Add lint", Digest: "style"})
	c, err := Merge(r, again)
	if err != nil {
		t.Fatal(err)
	}
	if c != (Created{Steps: 1}) {
		t.Errorf("Created = %+v, want one new step under the existing task", c)
	}
	if got, _ := r.Get("phase-001.task-001.step-003"); got.Title != "Add lint" {
		t.Errorf("new step = %+v", got)
	}
}

func TestMergeNumbersAfterTheHighestSibling(t *testing.T) {
	r := newRepo(t)
	if _, err := Merge(r, Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "One", Digest: "d"}, {Title: "Two", Digest: "d"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(r, Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "Three", Digest: "d"}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Get("phase-003"); err != nil || got.Title != "Three" {
		t.Errorf("phase-003 = %+v, %v", got, err)
	}
}

func TestMergeCollapsesWhitespaceInTitlesAndDigests(t *testing.T) {
	r := newRepo(t)
	out := Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "Two\nline   title", Digest: "why\n it exists"}}}
	if _, err := Merge(r, out); err != nil {
		t.Fatal(err)
	}
	n, _ := r.Get("phase-001")
	if n.Title != "Two line title" || strings.Contains(n.ContextDigest, "\n") {
		t.Errorf("node = %+v", n)
	}
}

func TestMergeReportsTheSiblingLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("creates 999 nodes")
	}
	r := newRepo(t)
	for i := 1; i <= 999; i++ {
		id, _ := plantree.ChildID(plantree.RootID, i)
		n, err := newSkeleton(id, fmt.Sprintf("phase %d", i), "d", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Merge(r, Pass1Output{Overview: "o", Phases: []PhaseOut{{Title: "one too many", Digest: "d"}}})
	if err == nil || !strings.Contains(err.Error(), "one too many") {
		t.Errorf("Merge past 999 siblings = %v, want an error naming the title", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: Merge`.

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/merge.go`:

```go
package plan

import (
	"fmt"
	"strconv"

	"gophermind/gophermind-lib/plantree"
)

// Created counts the nodes a merge added to the tree.
type Created struct {
	Phases int
	Tasks  int
	Steps  int
}

func (c *Created) add(o Created) {
	c.Phases += o.Phases
	c.Tasks += o.Tasks
	c.Steps += o.Steps
}

// Merge applies a skeleton pass to the tree. A proposed node whose title
// (ignoring case and spacing) matches an existing sibling is reused unchanged,
// so replaying the same pass after a crash adds nothing twice. New nodes start
// as untouched skeletons.
func Merge(repo *plantree.Repo, out Pass1Output) (Created, error) {
	var created Created
	for _, p := range out.Phases {
		id, made, err := ensureChild(repo, plantree.RootID, p.Title, p.Digest, p.Objective)
		if err != nil {
			return created, err
		}
		if made {
			created.Phases++
		}
		for _, t := range p.Tasks {
			tid, made, err := ensureChild(repo, id, t.Title, t.Digest, t.Objective)
			if err != nil {
				return created, err
			}
			if made {
				created.Tasks++
			}
			for _, s := range t.Steps {
				_, made, err := ensureChild(repo, tid, s.Title, s.Digest, "")
				if err != nil {
					return created, err
				}
				if made {
					created.Steps++
				}
			}
		}
	}
	return created, nil
}

// ensureChild returns the id of parent's child with the given title, creating
// it if there is none. made reports whether it was created.
func ensureChild(repo *plantree.Repo, parent, title, digest, objective string) (id string, made bool, err error) {
	kids, err := repo.Children(parent)
	if err != nil {
		return "", false, err
	}
	want := NormalizeTitle(title)
	highest := 0
	for _, k := range kids {
		if NormalizeTitle(k.Title) == want {
			return k.ID, false, nil
		}
		if n := segmentNumber(k.ID); n > highest {
			highest = n
		}
	}
	id, err = plantree.ChildID(parent, highest+1)
	if err != nil {
		return "", false, fmt.Errorf("adding %q under %s: %w", title, parent, err)
	}
	node, err := newSkeleton(id, title, digest, objective)
	if err != nil {
		return "", false, err
	}
	if err := repo.Create(node); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// segmentNumber returns the trailing three-digit number of an id.
func segmentNumber(id string) int {
	if len(id) < 3 {
		return 0
	}
	n, _ := strconv.Atoi(id[len(id)-3:])
	return n
}

func newSkeleton(id, title, digest, objective string) (plantree.Node, error) {
	ref, err := plantree.ParentRef(id)
	if err != nil {
		return plantree.Node{}, err
	}
	n := plantree.Node{
		SchemaVersion: plantree.SchemaVersion,
		ID:            id,
		Title:         oneLine(title),
		NodeRevision:  1,
		ContextDigest: oneLine(digest),
		ParentRef:     &ref,
		DependsOn:     []string{},
		Planning:      plantree.Planning{Stage: plantree.StageSkeleton},
		Objective:     objective,
	}
	if n.Kind() == plantree.KindStep {
		n.Status = plantree.StatusUntouched
	}
	return n, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok` (this run includes the 999-node sibling-limit test, about 5 seconds), no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/merge.go gophermind-lib/plantree/plan/merge_test.go
git commit -m "feat(plan): idempotent title-matched merge of skeleton passes into the tree

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 4: Overview and prompts

**Files:**
- Create: `gophermind-lib/plantree/plan/overview.go`
- Create: `gophermind-lib/plantree/plan/prompt.go`
- Test: `gophermind-lib/plantree/plan/overview_test.go` (covers both files)

**Interfaces:**
- Consumes: `Chunk` (Task 1), `Merge`, `newRepo`, `sampleOut` (Task 3), `cutPoint` (Task 1), `oneLine` (Task 2), `lockfile.WriteAtomic`.
- Produces:
  - `const OverviewCapBytes = 6000`
  - `func ReadOverview(dir string) (string, error)`, `func WriteOverview(dir, text string) error`, `func FitOverview(text string, cap int) string`
  - `func Outline(repo *plantree.Repo) (string, error)` (phase and task titles only, bounded)
  - `func Pass1Prompt(project, overview, outline string, c Chunk, total int) string`
  - `func RetryPrompt(original, problem string) string`, `func CompressPrompt(overview string, cap int) string`

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/overview_test.go`:

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
	if p := RetryPrompt("ORIGINAL", "digest is empty"); !strings.Contains(p, "ORIGINAL") || !strings.Contains(p, "digest is empty") {
		t.Errorf("RetryPrompt = %q", p)
	}
	if p := CompressPrompt("long overview", 500); !strings.Contains(p, "Compress") || !strings.Contains(p, "500") || !strings.Contains(p, "long overview") {
		t.Errorf("CompressPrompt = %q", p)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: ReadOverview` (and the other names).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/overview.go`:

```go
package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/lockfile"
)

// OverviewCapBytes is the soft size limit of the running overview (about
// 1,500 tokens). It is prose for the model, not an authority for the tree.
const OverviewCapBytes = 6000

const (
	overviewFile = "overview.md"
	truncMarker  = "\n[overview truncated]"
)

// ReadOverview returns the overview stored in dir, or "" if there is none.
func ReadOverview(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, overviewFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// WriteOverview atomically replaces the overview stored in dir.
func WriteOverview(dir, text string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := strings.TrimRight(text, "\n") + "\n"
	return lockfile.WriteAtomic(filepath.Join(dir, overviewFile), []byte(body), 0o644)
}

// FitOverview returns text unchanged when it fits in cap bytes. Otherwise it
// cuts at the last paragraph or line end that fits and appends a marker, so the
// result is at most cap bytes and never ends mid-word or mid-rune.
func FitOverview(text string, cap int) string {
	if len(text) <= cap {
		return text
	}
	limit := cap - len(truncMarker)
	if limit < 1 {
		limit = 1
	}
	cut := cutPoint(text, limit)
	return strings.TrimRight(text[:cut], "\n ") + truncMarker
}
```

Create `plantree/plan/prompt.go`:

```go
package plan

import (
	"fmt"
	"strings"

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
	fmt.Fprintf(&b, "- Rewrite the overview so it covers the whole brief so far, in under %d characters. Keep decisions, constraints and non-goals; drop detail that the plan itself now holds.\n", OverviewCapBytes)
	b.WriteString("- Do not call tools. Reply with ONE JSON object and nothing else, in this shape:\n")
	b.WriteString(`{"phases":[{"title":"","digest":"","objective":"","tasks":[{"title":"","digest":"","objective":"","steps":[{"title":"","digest":""}]}]}],"overview":""}`)
	b.WriteString("\n")
	return b.String()
}

// RetryPrompt asks the model to correct a reply that was rejected.
func RetryPrompt(original, problem string) string {
	return original + "\n\nYour previous reply was rejected: " + problem + "\nReply again with ONE JSON object only, fixing that problem."
}

// CompressPrompt asks the model to shorten an overview that grew past its cap.
func CompressPrompt(overview string, cap int) string {
	return fmt.Sprintf("Compress this project overview to under %d characters. Keep decisions, constraints, non-goals and the shape of the plan; drop detail. Reply with the compressed overview text only.\n\n%s\n", cap, strings.TrimRight(overview, "\n"))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w plantree && go test ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/overview.go gophermind-lib/plantree/plan/prompt.go gophermind-lib/plantree/plan/overview_test.go
git commit -m "feat(plan): running overview, outline and pass prompts

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 5: The resumable runner

**Files:**
- Create: `gophermind-lib/plantree/plan/runner.go`
- Test: `gophermind-lib/plantree/plan/runner_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1 to 4, `plantree.Repo` (`Get`, `Init`, `Dir`), `plantree.ErrNotFound`, `lockfile.WriteAtomic`.
- Produces:
  - `type Completer interface{ Complete(ctx context.Context, prompt string) (string, error) }` (every call must start from a fresh context)
  - `var ErrBriefChanged error`
  - `type Options struct{ ProjectName string; ChunkBytes, OverviewCap int }`
  - `type Result struct{ Chunks, Processed int; Created Created }`
  - `func RunPass1(ctx context.Context, repo *plantree.Repo, brief string, c Completer, opt Options) (Result, error)`
  - package-private `pass1State`, `loadState`, `saveState`, `ensureRoot`, `saveBrief`, `runChunk`

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/runner_test.go`:

```go
package plan

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/plantree"
)

// fake is a Completer that records every prompt it is given.
type fake struct {
	prompts []string
	reply   func(n int, prompt string) (string, error)
}

func (f *fake) Complete(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.reply(len(f.prompts)-1, prompt)
}

const threePartBrief = "# One\nfirst part text\n\n# Two\nsecond part text\n\n# Three\nthird part text\n"

// opts forces one chunk per section of threePartBrief.
var opts = Options{ProjectName: "demo", ChunkBytes: 30}

const (
	reply1 = `{"phases":[{"title":"Alpha","digest":"first phase","objective":"","tasks":[{"title":"T1","digest":"task one","objective":"","steps":[{"title":"S1","digest":"step one"}]}]}],"overview":"overview 1"}`
	reply2 = `{"phases":[{"title":"alpha","digest":"ignored, already exists","objective":"","tasks":[{"title":"T2","digest":"task two","objective":"","steps":[{"title":"S2","digest":"step two"}]}]}],"overview":"overview 2"}`
	reply3 = `{"phases":[{"title":"Beta","digest":"second phase","objective":"","tasks":[{"title":"T3","digest":"task three","objective":"","steps":[{"title":"S3","digest":"step three"}]}]}],"overview":"overview 3"}`
)

// byChunk answers each chunk's pass with the reply written for it.
func byChunk(_ int, prompt string) (string, error) {
	switch {
	case strings.Contains(prompt, "first part text"):
		return reply1, nil
	case strings.Contains(prompt, "second part text"):
		return reply2, nil
	case strings.Contains(prompt, "third part text"):
		return reply3, nil
	}
	return "", errors.New("prompt has no known chunk")
}

var wantTree = []string{
	"plan demo",
	"phase-001 Alpha",
	"phase-001.task-001 T1",
	"phase-001.task-001.step-001 S1",
	"phase-001.task-002 T2",
	"phase-001.task-002.step-001 S2",
	"phase-002 Beta",
	"phase-002.task-001 T3",
	"phase-002.task-001.step-001 S3",
}

func TestRunPass1BuildsTheWholeSkeleton(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: byChunk}
	res, err := RunPass1(context.Background(), r, threePartBrief, f, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Chunks != 3 || res.Processed != 3 || res.Created != (Created{Phases: 2, Tasks: 3, Steps: 3}) {
		t.Errorf("Result = %+v", res)
	}
	if got := ids(t, r); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree = %v", got)
	}
	if ov, _ := ReadOverview(r.Dir()); ov != "overview 3\n" {
		t.Errorf("overview = %q", ov)
	}
	if st, found, _ := loadState(r); !found || st.Next != 3 {
		t.Errorf("state = %+v, found=%v", st, found)
	}
	acts, err := r.NextActions()
	if err != nil || len(acts.Runnable) != 3 || acts.Runnable[0].Kind != plantree.ActionDraft {
		t.Errorf("NextActions = %+v, %v; want three draft actions", acts, err)
	}
	if err := r.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestEveryPassIsFreshAndBounded(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, threePartBrief, f, opts); err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 3 {
		t.Fatalf("%d prompts, want 3", len(f.prompts))
	}
	if strings.Contains(f.prompts[1], "first part text") || strings.Contains(f.prompts[1], "third part text") {
		t.Error("the second prompt carries text from another chunk")
	}
	if !strings.Contains(f.prompts[1], "overview 1") || !strings.Contains(f.prompts[1], "- Phase: Alpha") {
		t.Error("the second prompt must carry the running overview and the outline so far")
	}
	if strings.Contains(f.prompts[0], "overview 1") {
		t.Error("the first prompt cannot know the later overview")
	}
	for i, p := range f.prompts {
		if len(p) > 4000 {
			t.Errorf("prompt %d is %d bytes for a tiny brief", i, len(p))
		}
	}
}

func TestResumeAfterAFailureContinuesWhereItStopped(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	broken := &fake{reply: func(n int, p string) (string, error) {
		if strings.Contains(p, "second part text") {
			return "", errors.New("model unavailable")
		}
		return byChunk(n, p)
	}}
	res, err := RunPass1(context.Background(), r, threePartBrief, broken, opts)
	if err == nil || !strings.Contains(err.Error(), "chunk 2 of 3") || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want it to name chunk 2 of 3 and the cause", err)
	}
	if res.Processed != 1 {
		t.Errorf("Processed = %d, want 1", res.Processed)
	}
	if st, _, _ := loadState(r); st.Next != 1 {
		t.Errorf("state.Next = %d, want 1", st.Next)
	}

	good := &fake{reply: byChunk} // a new process, a new model client
	res, err = RunPass1(context.Background(), plantree.Open(dir), threePartBrief, good, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(good.prompts) != 2 || res.Processed != 2 {
		t.Errorf("resume made %d calls, Processed=%d; want 2 and 2", len(good.prompts), res.Processed)
	}
	if got := ids(t, plantree.Open(dir)); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree after resume = %v", got)
	}
}

func TestReplayOfAMergedButUnrecordedChunkAddsNothingTwice(t *testing.T) {
	r := plantree.Open(t.TempDir())
	// Simulate a crash after chunk 1 was merged but before the cursor moved.
	if err := ensureRoot(r, "demo"); err != nil {
		t.Fatal(err)
	}
	first, err := ParsePass1(reply1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(r, first); err != nil {
		t.Fatal(err)
	}
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, r); !reflect.DeepEqual(got, wantTree) {
		t.Errorf("tree = %v", got)
	}
}

func TestMalformedReplyGetsOneCorrectionRetry(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: func(n int, p string) (string, error) {
		if n == 0 {
			return "Sure! Here is a plan, but no JSON.", nil
		}
		return byChunk(n, p)
	}}
	res, err := RunPass1(context.Background(), r, threePartBrief, f, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.prompts) != 4 || res.Processed != 3 {
		t.Errorf("%d calls, Processed=%d; want 4 and 3", len(f.prompts), res.Processed)
	}
	if !strings.Contains(f.prompts[1], "rejected") || !strings.Contains(f.prompts[1], "no JSON object") {
		t.Errorf("the retry prompt must say why: %q", f.prompts[1][len(f.prompts[1])-200:])
	}
}

func TestReplyRejectedTwiceStopsWithoutAdvancing(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: func(int, string) (string, error) { return "still not json", nil }}
	_, err := RunPass1(context.Background(), r, threePartBrief, f, opts)
	if err == nil || !strings.Contains(err.Error(), "rejected twice") || !strings.Contains(err.Error(), "chunk 1 of 3") {
		t.Fatalf("err = %v", err)
	}
	if len(f.prompts) != 2 {
		t.Errorf("%d calls, want exactly 2 (ask, then one correction)", len(f.prompts))
	}
	if st, _, _ := loadState(r); st.Next != 0 {
		t.Errorf("state.Next = %d, want 0", st.Next)
	}
}

func TestResumingWithADifferentBriefIsRefused(t *testing.T) {
	dir := t.TempDir()
	r := plantree.Open(dir)
	if _, err := RunPass1(context.Background(), r, threePartBrief, &fake{reply: byChunk}, opts); err != nil {
		t.Fatal(err)
	}
	f := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, threePartBrief+"one more line\n", f, opts); !errors.Is(err, ErrBriefChanged) {
		t.Errorf("changed brief: err = %v, want ErrBriefChanged", err)
	}
	other := opts
	other.ChunkBytes = 60
	if _, err := RunPass1(context.Background(), r, threePartBrief, f, other); !errors.Is(err, ErrBriefChanged) {
		t.Errorf("changed chunk size: err = %v, want ErrBriefChanged", err)
	}
	if len(f.prompts) != 0 {
		t.Errorf("refused runs must not call the model, made %d calls", len(f.prompts))
	}
}

func TestOverviewOverTheCapIsCompressedThenCut(t *testing.T) {
	long := strings.Repeat("overview sentence. ", 20) // 380 bytes
	brief := "# Only\nsome text\n"
	mk := func(compressed string) *fake {
		return &fake{reply: func(_ int, p string) (string, error) {
			if strings.Contains(p, "Compress this project overview") {
				return compressed, nil
			}
			return fmt.Sprintf(`{"phases":[],"overview":%q}`, long), nil
		}}
	}
	o := Options{ProjectName: "demo", OverviewCap: 100}

	r := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r, brief, mk("short overview"), o); err != nil {
		t.Fatal(err)
	}
	if ov, _ := ReadOverview(r.Dir()); ov != "short overview\n" {
		t.Errorf("compressed overview = %q", ov)
	}

	r2 := plantree.Open(t.TempDir())
	if _, err := RunPass1(context.Background(), r2, brief, mk(long), o); err != nil {
		t.Fatal(err)
	}
	ov, _ := ReadOverview(r2.Dir())
	if len(ov) > 101 || !strings.Contains(ov, "[overview truncated]") {
		t.Errorf("a compression that is still too long must be cut: len=%d %q", len(ov), ov)
	}
}

func TestEmptyBriefAndCancelledContext(t *testing.T) {
	r := plantree.Open(t.TempDir())
	f := &fake{reply: byChunk}
	if _, err := RunPass1(context.Background(), r, "  \n", f, opts); err == nil {
		t.Error("an empty brief must be an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunPass1(ctx, r, threePartBrief, f, opts); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled run: err = %v", err)
	}
	if len(f.prompts) != 0 {
		t.Errorf("no model calls expected, made %d", len(f.prompts))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: RunPass1` (and the other names).

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/runner.go`:

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

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

// Completer runs one prompt and returns the model's reply. Every call must
// start from a fresh context: no history from any earlier call. AgentCompleter
// is the production implementation.
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// ErrBriefChanged is returned when a run is resumed against a brief, or a chunk
// size, different from the one it started with. Resuming would mix two plans.
var ErrBriefChanged = errors.New("plan: the brief or chunk size changed since this run started")

// Options tunes RunPass1. Zero values pick the defaults.
type Options struct {
	ProjectName string
	ChunkBytes  int // default DefaultChunkBytes
	OverviewCap int // default OverviewCapBytes
}

// Result summarizes one RunPass1 call.
type Result struct {
	Chunks    int     // chunks in the whole brief
	Processed int     // chunks processed by this call
	Created   Created // nodes added by this call
}

const (
	briefFile = "brief.md"
	stateFile = "pass1.json"
)

// pass1State is the resume cursor for a skeleton run. It is only a cursor:
// the tree and overview hold the real state, and a chunk that was merged but
// not yet recorded here is simply merged again, which changes nothing.
type pass1State struct {
	BriefSHA256 string `json:"brief_sha256"`
	Chunks      int    `json:"chunks"`
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
		return pass1State{}, false, fmt.Errorf("plan: reading %s: %w", statePath(repo), err)
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
	if found && (state.BriefSHA256 != digest || state.Chunks != len(chunks)) {
		return Result{}, ErrBriefChanged
	}
	if !found {
		state = pass1State{BriefSHA256: digest, Chunks: len(chunks)}
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
		created, err := runChunk(ctx, repo, c, opt, chunks[i], len(chunks))
		if err != nil {
			return res, fmt.Errorf("plan: chunk %d of %d: %w", i+1, len(chunks), err)
		}
		res.Created.add(created)
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

// runChunk performs one skeleton pass: build the prompt, ask, parse (once more
// with the rejection reason if the reply is malformed), merge, refresh the
// overview.
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

	reply, err := c.Complete(ctx, prompt)
	if err != nil {
		return Created{}, err
	}
	out, perr := ParsePass1(reply)
	if perr != nil {
		reply, err = c.Complete(ctx, RetryPrompt(prompt, perr.Error()))
		if err != nil {
			return Created{}, err
		}
		if out, err = ParsePass1(reply); err != nil {
			return Created{}, fmt.Errorf("the model's reply was rejected twice: %w", err)
		}
	}

	created, err := Merge(repo, out)
	if err != nil {
		return created, err
	}
	text := out.Overview
	if len(text) > opt.OverviewCap {
		if shorter, cerr := c.Complete(ctx, CompressPrompt(text, opt.OverviewCap)); cerr == nil && shorter != "" {
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

- [ ] **Step 4: Run the tests, including the race detector**

Run: `gofmt -w plantree && go test ./plantree/... -count=1 && go test -race ./plantree/plan/... -short -count=1 && gofmt -l plantree && go vet ./plantree/...`
Expected: all `ok`, no gofmt output, vet clean.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/runner.go gophermind-lib/plantree/plan/runner_test.go
git commit -m "feat(plan): resumable pass-1 runner with one fresh-context call per chunk

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---
### Task 6: The production Completer

**Files:**
- Create: `gophermind-lib/plantree/plan/clientcompleter.go`
- Test: `gophermind-lib/plantree/plan/clientcompleter_test.go`

**Interfaces:**
- Consumes: `Completer` (Task 5), `llm.Client.Stream(ctx, msgs []llm.Message, tools []llm.Tool, onToken func(string)) (llm.Message, llm.Usage, error)`.
- Produces: `type ClientCompleter struct{ Client *llm.Client }` with `Complete(ctx, prompt) (string, error)` (satisfies `Completer`).

- [ ] **Step 1: Write the failing tests**

Create `plantree/plan/clientcompleter_test.go`:

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
)

func TestClientCompleterStartsEveryCallFromScratchWithNoTools(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"REPLY"},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}
	first, err := c.Complete(context.Background(), "FIRST-PROMPT-TEXT")
	if err != nil || first != "REPLY" {
		t.Fatalf("Complete = %q, %v", first, err)
	}
	if _, err := c.Complete(context.Background(), "SECOND-PROMPT-TEXT"); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(bodies))
	}
	if !strings.Contains(bodies[1], "SECOND-PROMPT-TEXT") || strings.Contains(bodies[1], "FIRST-PROMPT-TEXT") || strings.Contains(bodies[1], "REPLY") {
		t.Error("the second request must not carry anything from the first call")
	}
	for i, b := range bodies {
		if strings.Contains(b, `"tools"`) {
			t.Errorf("request %d offers tool definitions: a planning pass must be offered none", i)
		}
		if strings.Contains(b, "precise coding agent") || !strings.Contains(b, "planning assistant") {
			t.Errorf("request %d must use the planner system prompt, not the coding agent's", i)
		}
	}
}

func TestClientCompleterReportsAnEmptyReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: " + `{"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := ClientCompleter{Client: llm.New(srv.URL, "", "m", 5*time.Second, false)}
	if _, err := c.Complete(context.Background(), "p"); err == nil {
		t.Error("an empty reply must be an error, not an empty string")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./plantree/plan/... -short -count=1`
Expected: FAIL to build with `undefined: ClientCompleter`.

- [ ] **Step 3: Write the implementation**

Create `plantree/plan/clientcompleter.go`:

```go
package plan

import (
	"context"
	"errors"

	"gophermind/gophermind-lib/llm"
)

// plannerSystemPrompt replaces the coding agent's system prompt. A planning
// pass only turns a prompt into JSON, so it must not be nudged to explore,
// edit files or run commands.
const plannerSystemPrompt = "You are a planning assistant. You break project briefs into plans and reply only with what the user asks for. You never call tools, write files or run commands."

// ClientCompleter runs every prompt as a brand-new two-message conversation
// (a planner system prompt and the prompt itself) with no tools, so no pass
// can inherit, or overflow on, an earlier pass's context.
type ClientCompleter struct {
	Client *llm.Client
}

// Complete implements Completer.
func (c ClientCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	msgs := []llm.Message{
		{Role: "system", Content: plannerSystemPrompt},
		{Role: "user", Content: prompt},
	}
	reply, _, err := c.Client.Stream(ctx, msgs, nil, nil)
	if err != nil {
		return "", err
	}
	if reply.Content == "" {
		return "", errors.New("the model returned an empty reply")
	}
	return reply.Content, nil
}
```

- [ ] **Step 4: Run every check**

Run: `gofmt -w plantree && go test ./plantree/... -count=1 && go test -race ./plantree/... -short -count=1 && gofmt -l plantree && go vet ./plantree/... && go build ./...`
Expected: all `ok`, no gofmt output, vet clean, the whole module builds.

- [ ] **Step 5: Commit**

```bash
git add gophermind-lib/plantree/plan/clientcompleter.go gophermind-lib/plantree/plan/clientcompleter_test.go
git commit -m "feat(plan): planner-prompt completer with no tools and no history

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HArwYJXPZfFwmuSRuxLcYr"
```

---

## Self-review (M2)

- **Design coverage:** fresh context per pass (Tasks 5 and 6), terse overview (Task 4, capped and compressed), tree as state with a derived resume point (Tasks 3 and 5), overflow protection (bounded chunks, bounded outline, capped overview, no truncation of the brief). Questions, pass 2, UI and wiring are M3 to M6 by design.
- **Placeholders:** none. Every step carries the full code, and the code was run green in order, task by task, before this plan was generated.
- **Types:** `Chunk`, `Pass1Output`, `Created`, `Completer`, `Options` and `Result` are defined once and used with the same names later. `newRepo`, `ids` and `sampleOut` live in `merge_test.go` and are reused by later test files in the same package.
- **Known limits:** `ensureChild` numbers siblings by max+1 (single writer); a second process reading mid-run can see a half-merged chunk; the resume cursor is written after the merge, so a crash replays the last chunk, which the title-matched merge makes harmless.

## Definition of done (M2)

`go test ./plantree/... -count=1`, `go test -race ./plantree/... -short -count=1`, `gofmt -l plantree` (empty), `go vet ./plantree/...` and `go build ./...` all clean; six commits; a test proves a run that fails at chunk 2 resumes at chunk 2 in a new process and ends with the same tree as an uninterrupted run.
