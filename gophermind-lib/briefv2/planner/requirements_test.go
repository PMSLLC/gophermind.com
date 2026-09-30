package planner_test

import (
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
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
