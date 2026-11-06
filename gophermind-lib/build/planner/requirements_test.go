package planner_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

const aivsBrief = "../testdata/ai-venture-studio-server-brief.md"

// sampleBrief is numbered by line in the comments of the test below.
const sampleBrief = `---
spec_version: "2.0"
id: gm-2026-09-29-009
---

## Overview

- An overview bullet is not a requirement.

## Features

### Greeting

Says hello.

- Returns "hello, NAME".

### Farewell

Says goodbye.

## Constraints

These hold everywhere.

- Standard library only.
- Errors are values:
  - never panic
  - wrap with %w

` + "```text" + `
- a bullet inside a fence
` + "```" + `
* Go 1.22 or later.

## Out of scope

- Persistence.

## Acceptance

- ` + "`go build ./...`" + ` succeeds.
- ` + "`go test ./...`" + ` passes
  with no network.
`

func TestParseRequirements(t *testing.T) {
	got := planner.ParseRequirements([]byte(sampleBrief))
	want := []planner.Requirement{
		{ID: "F1", Kind: planner.ReqFeature, Name: "Greeting", Text: "Says hello.\n\n- Returns \"hello, NAME\".", Line: 12},
		{ID: "F2", Kind: planner.ReqFeature, Name: "Farewell", Text: "Says goodbye.", Line: 18},
		{ID: "C1", Kind: planner.ReqConstraint, Text: "Standard library only.", Line: 26},
		{ID: "C2", Kind: planner.ReqConstraint, Text: "Errors are values:\n  - never panic\n  - wrap with %w", Line: 27},
		{ID: "C3", Kind: planner.ReqConstraint, Text: "Go 1.22 or later.", Line: 34},
		{ID: "A1", Kind: planner.ReqAcceptance, Text: "`go build ./...` succeeds.", Line: 42},
		{ID: "A2", Kind: planner.ReqAcceptance, Text: "`go test ./...` passes\n  with no network.", Line: 43},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requirements, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("requirement %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestParseRequirementsEdgeCases(t *testing.T) {
	cases := []struct {
		name, src string
		wantIDs   string
	}{
		{"empty file", "", ""},
		{"no frontmatter", "## Constraints\n\n- one\n", "C1"},
		{"crlf and bom", "\xef\xbb\xbf---\r\nid: x\r\n---\r\n\r\n## Acceptance\r\n\r\n- one\r\n- two\r\n", "A1 A2"},
		{"no acceptance section", "---\n---\n## Constraints\n- one\n## Features\n### A\nbody\n", "C1 F1"},
		{"heading inside a fence is not a section", "## Constraints\n- one\n```\n## Acceptance\n- hidden\n```\n- two\n", "C1 C2"},
		{"text before the first bullet is ignored", "## Acceptance\nIntro line.\n\n- one\n", "A1"},
		{"a paragraph ends a bullet and is ignored", "## Constraints\n- one\nloose text\n  indented after loose text\n- two\n", "C1 C2"},
		{"plus and numbered bullets count", "## Constraints\n+ one\n1. two\n2) three\n", "C1 C2 C3"},
		{"h3 outside features is nothing", "## Architecture\n### Packages\n- `cmd/x`\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ids []string
			for _, r := range planner.ParseRequirements([]byte(c.src)) {
				ids = append(ids, r.ID)
			}
			if got := strings.Join(ids, " "); got != c.wantIDs {
				t.Fatalf("ids = %q, want %q", got, c.wantIDs)
			}
		})
	}
	// The ignored paragraph must not leak into the bullet before it.
	got := planner.ParseRequirements([]byte("## Constraints\n- one\nloose text\n  indented after loose text\n- two\n"))
	if got[0].Text != "one" || got[1].Text != "two" {
		t.Fatalf("texts = %q, %q", got[0].Text, got[1].Text)
	}
}

// The AI Venture Studio brief is the one the v1 planner dropped requirements
// from. The counts are pinned so a parser change that loses a bullet fails.
func TestParseRequirementsAIVentureStudio(t *testing.T) {
	src, err := os.ReadFile(aivsBrief)
	if err != nil {
		t.Fatal(err)
	}
	reqs := planner.ParseRequirements(src)
	counts := map[planner.ReqKind]int{}
	for _, r := range reqs {
		counts[r.Kind]++
		if r.Text == "" || r.Line == 0 {
			t.Errorf("%s has empty text or no line: %+v", r.ID, r)
		}
	}
	if counts[planner.ReqConstraint] != 10 || counts[planner.ReqAcceptance] != 12 || counts[planner.ReqFeature] != 19 {
		t.Fatalf("counts = %v, want 10 constraints, 12 acceptance, 19 features", counts)
	}
	lines := strings.Split(string(src), "\n")
	for _, r := range reqs {
		at := lines[r.Line-1]
		switch r.Kind {
		case planner.ReqFeature:
			if at != "### "+r.Name {
				t.Errorf("%s: line %d is %q, want the heading %q", r.ID, r.Line, at, r.Name)
			}
		default:
			first := strings.SplitN(r.Text, "\n", 2)[0]
			if at != "- "+first {
				t.Errorf("%s: line %d is %q, want the bullet %q", r.ID, r.Line, at, first)
			}
		}
	}
}

func TestIsTestRule(t *testing.T) {
	cases := []struct {
		kind planner.ReqKind
		text string
		want bool
	}{
		{planner.ReqConstraint, "Tests are hermetic and table driven.", true},
		{planner.ReqConstraint, "Tests for `internal/cli` use separate `bytes.Buffer` values for `stdout` and `stderr`.", true},
		{planner.ReqConstraint, "Tests never invoke `main` directly, because `main` ends the process; the entry point is verified by running the built binary.", true},
		{planner.ReqConstraint, "  unit tests run offline.", true},
		{planner.ReqConstraint, "Benchmarks live beside the code.", true},
		{planner.ReqConstraint, "Tests", true},
		{planner.ReqConstraint, "Go 1.22 or later.", false},
		{planner.ReqConstraint, "`gofmt` clean", false},
		{planner.ReqConstraint, "Testing is done elsewhere.", false},
		{planner.ReqFeature, "Tests the greeting.", false},
	}
	for _, c := range cases {
		if got := (planner.Requirement{ID: "C1", Kind: c.kind, Text: c.text}).IsTestRule(); got != c.want {
			t.Errorf("IsTestRule(%v %q) = %v, want %v", c.kind, c.text, got, c.want)
		}
	}
}

// helloWithTests is the hello fixture brief with the Tests section of the
// library brief hello v1.1 appended (testdata/hello/tests-section.md, verbatim).
func helloWithTests(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/hello/brief.md")
	if err != nil {
		t.Fatal(err)
	}
	s, err := os.ReadFile("testdata/hello/tests-section.md")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, s...)
}

func TestTestsSectionBulletsGetTIds(t *testing.T) {
	src := helloWithTests(t)
	lines := strings.Split(string(src), "\n")
	start := 0
	for i, l := range lines {
		if l == "## Tests" {
			start = i
		}
	}
	type want struct {
		text string
		line int
	}
	var wants []want
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "- ") {
			wants = append(wants, want{strings.TrimPrefix(lines[i], "- "), i + 1})
		}
	}
	if len(wants) != 9 {
		t.Fatalf("the Tests section has %d bullets, want 9", len(wants))
	}
	var got []planner.Requirement
	for _, q := range planner.ParseRequirements(src) {
		if q.Kind == planner.ReqTest {
			got = append(got, q)
		}
	}
	if len(got) != len(wants) {
		t.Fatalf("got %d kind-test requirements, want %d: %+v", len(got), len(wants), got)
	}
	for i, q := range got {
		if id := "T" + strconv.Itoa(i+1); q.ID != id {
			t.Errorf("requirement %d has id %s, want %s", i, q.ID, id)
		}
		if q.Text != wants[i].text || q.Line != wants[i].line {
			t.Errorf("%s = %q line %d, want %q line %d", q.ID, q.Text, q.Line, wants[i].text, wants[i].line)
		}
	}
}

func TestCoverageIgnoresTestRequirements(t *testing.T) {
	reqs := []planner.Requirement{{ID: "T1", Kind: planner.ReqTest, Text: "`Run(nil, stdout, stderr)` returns 0.", Line: 40}}
	covered, gaps := planner.CheckCoverage(reqs, testNodes, planner.CoverageReply{})
	if len(gaps) != 0 {
		t.Errorf("a test requirement opened a gap: %+v", gaps)
	}
	if len(covered) != 0 {
		t.Errorf("a test requirement has a Covered entry: %+v", covered)
	}
}
