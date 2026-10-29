package brief_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
)

func TestCheckIsEmptyForEveryExample(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(examplesDir, "[0-9][0-9]-*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := brief.Check(raw); len(got) != 0 {
			t.Errorf("%s: %+v", filepath.Base(f), got)
		}
	}
}

func TestCheckReportsEveryProblemAtOnce(t *testing.T) {
	src := "---\n" +
		"spec_version: \"2.0\"\n" +
		"id: gm-2026-10-28-050\n" +
		"title: x\n" +
		"language: go\n" +
		"base_branch: main\n" +
		"landing: commit\n" +
		"on_ambiguity: halt\n" +
		"bogus_key: 1\n" +
		"---\n" +
		"\n" +
		"## Overview\n" +
		"\n" +
		"hi\n" +
		"\n" +
		"## Overview\n" +
		"\n" +
		"again\n" +
		"\n" +
		"## Constraints\n" +
		"\n" +
		"a plain paragraph\n"
	got := brief.Check([]byte(src))
	want := []struct {
		line int
		text string
	}{
		{9, "bogus_key"},
		{16, "duplicate section"},
		{22, "not a bullet"},
		{0, `"## Features"`},
		{0, `"## Acceptance"`},
	}
	for _, w := range want {
		found := false
		for _, p := range got {
			if p.Line == w.line && strings.Contains(p.Message, w.text) {
				found = true
			}
		}
		if !found {
			t.Errorf("no problem at line %d containing %q; got %+v", w.line, w.text, got)
		}
	}
	for _, p := range got {
		if strings.Contains(p.Message, "a plain paragraph") {
			t.Errorf("message carries brief text: %q", p.Message)
		}
	}
}

func TestCheckStopsAtAMissingFrontmatter(t *testing.T) {
	got := brief.Check([]byte("# Brief\n\nA tool.\n"))
	if len(got) != 1 || got[0].Line != 1 || !strings.Contains(got[0].Message, "frontmatter") {
		t.Fatalf("got %+v, want one problem on line 1 about the frontmatter", got)
	}
	got = brief.Check([]byte("---\nid: x\n"))
	if len(got) != 1 || !strings.Contains(got[0].Message, "not closed") {
		t.Fatalf("got %+v, want one problem about the unclosed block", got)
	}
}

func TestCheckFindsSemanticFrontmatterProblems(t *testing.T) {
	ex := string(loadExample(t))
	edited := strings.Replace(ex, "env:\n", "env:\n  - name: PATH\n    purpose: p\n  - name: DATA_DIR\n    purpose: p\n  - name: DATA_DIR\n    purpose: q\n", 1)
	if edited == ex {
		t.Fatal("edit did not apply")
	}
	got := brief.Check([]byte(edited))
	for _, text := range []string{"PATH is reserved", "env DATA_DIR is declared twice"} {
		found := false
		for _, p := range got {
			found = found || strings.Contains(p.Message, text)
		}
		if !found {
			t.Errorf("missing a problem containing %q; got %+v", text, got)
		}
	}
}

// Check and Parse must never disagree about whether a brief is acceptable.
func TestCheckAgreesWithParse(t *testing.T) {
	ex := string(loadExample(t))
	cases := map[string]string{
		"unchanged":         ex,
		"no frontmatter":    "## Overview\n\nhi\n",
		"unterminated":      strings.Replace(ex, "## Data\n", "## Data\n\n```\nx\n", 1),
		"prose constraint":  strings.Replace(ex, "## Constraints\n", "## Constraints\n\nplain words\n", 1),
		"duplicate section": ex + "\n## Overview\n\nagain\n",
		"missing section":   strings.Replace(ex, "## Out of scope", "## Something else", 1),
		"bad yaml":          strings.Replace(ex, "title:", "title: [", 1),
		"unknown key":       strings.Replace(ex, "language: go\n", "language: go\nzzz: 1\n", 1),
		"crlf":              strings.ReplaceAll(ex, "\n", "\r\n"),
	}
	for name, src := range cases {
		_, perr := brief.Parse([]byte(src))
		probs := brief.Check([]byte(src))
		if (perr == nil) != (len(probs) == 0) {
			t.Errorf("%s: Parse err = %v but Check = %+v", name, perr, probs)
		}
	}
}
