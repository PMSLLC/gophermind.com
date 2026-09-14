package prompt

import (
	"strings"
	"testing"
)

func TestParseFrontmatterAndSections(t *testing.T) {
	src := `---
name: reviewer
description: reviews code
tools: read_file, search, run_shell
color: blue
---
<role>
You are a careful reviewer.
</role>
<rules>
Prefer small diffs.
</rules>`
	tmpl, err := ParseTemplate(src)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "reviewer" || tmpl.Description != "reviews code" || tmpl.Color != "blue" {
		t.Errorf("frontmatter fields wrong: %+v", tmpl)
	}
	if len(tmpl.Tools) != 3 || tmpl.Tools[0] != "read_file" || tmpl.Tools[2] != "run_shell" {
		t.Errorf("tools = %v", tmpl.Tools)
	}
	if tmpl.Sections["role"] != "You are a careful reviewer." {
		t.Errorf("role section = %q", tmpl.Sections["role"])
	}
	if tmpl.Sections["rules"] != "Prefer small diffs." {
		t.Errorf("rules section = %q", tmpl.Sections["rules"])
	}
}

func TestParseOnlyFrontmatter(t *testing.T) {
	tmpl, err := ParseTemplate("---\nname: bare\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "bare" || len(tmpl.Sections) != 0 {
		t.Errorf("expected only frontmatter: %+v", tmpl)
	}
}

func TestParseOnlySections(t *testing.T) {
	tmpl, err := ParseTemplate("<role>\nhi\n</role>")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "" || tmpl.Sections["role"] != "hi" {
		t.Errorf("expected only sections: %+v", tmpl)
	}
}

func TestParseMalformedFrontmatter(t *testing.T) {
	// Opening --- with no closing delimiter.
	if _, err := ParseTemplate("---\nname: x\n<role>hi</role>"); err == nil {
		t.Error("unclosed frontmatter should error")
	}
}

func TestParseUnclosedSection(t *testing.T) {
	if _, err := ParseTemplate("<role>\nhi\n"); err == nil {
		t.Error("unclosed section tag should error")
	}
}

func TestBuildOrdersAndInjectsContext(t *testing.T) {
	tmpl := &Template{Sections: map[string]string{
		"rules":   "Follow {{.ProjectInstructions}}.",
		"role":    "Agent.",
		"unknown": "extra.",
	}}
	out := Build(tmpl, map[string]string{"ProjectInstructions": "the CLAUDE.md rules"})
	// role must come before rules (canonical order).
	if strings.Index(out, "Agent.") > strings.Index(out, "Follow") {
		t.Errorf("sections not in canonical order:\n%s", out)
	}
	if !strings.Contains(out, "Follow the CLAUDE.md rules.") {
		t.Errorf("context not injected:\n%s", out)
	}
	// unknown sections still included.
	if !strings.Contains(out, "extra.") {
		t.Errorf("extra section dropped:\n%s", out)
	}
	// wrapped in tags.
	if !strings.Contains(out, "<role>") || !strings.Contains(out, "</role>") {
		t.Errorf("sections not tag-wrapped:\n%s", out)
	}
}
