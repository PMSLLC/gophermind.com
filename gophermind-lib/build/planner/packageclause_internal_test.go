package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStripPackageClause(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"clause after the doc comment", "// Slug is a slug.\npackage slug\n\ntype Slug string", "// Slug is a slug.\n\ntype Slug string"},
		{"clause first", "package slug\ntype Slug string", "type Slug string"},
		{"no clause", "// Slug.\ntype Slug string", "// Slug.\ntype Slug string"},
		{"the word package inside a comment is kept", "// package slug is small\ntype Slug string", "// package slug is small\ntype Slug string"},
		{"windows line ends", "package slug\r\ntype Slug string", "type Slug string"},
	}
	for _, c := range cases {
		if got := stripPackageClause(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A model wrote a whole file for a type: the package clause made the declaration
// unparseable once the harness wrapped it in its own package, three times in a
// row, with an error that did not say why.
func TestNormalizeReplyStripsAPackageClauseFromATypeDecl(t *testing.T) {
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	reply := `{"types": [{"id": "slug", "package": "x", "file": "internal/x/slug.go", "decl": "// Slug is a slug.\npackage x\n\ntype Slug string", "uses": []}], "functions": []}`
	out, notes, err := normalizeReply(doc, reply, replyComponent)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Types []struct {
			Decl string `json:"decl"`
		} `json:"types"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Types[0].Decl, "package x") {
		t.Errorf("decl still carries the package clause: %q", r.Types[0].Decl)
	}
	if err := TypeDeclShape(r.Types[0].Decl); err != nil {
		t.Errorf("the cleaned declaration is still refused: %v", err)
	}
	if notes.PackageClauses != 1 {
		t.Errorf("notes.PackageClauses = %d, want 1", notes.PackageClauses)
	}
}

func TestDoesNotParseTellsTheModelWhatToCheck(t *testing.T) {
	err := TypeDeclShape("type Slug struct {")
	if err == nil || !strings.Contains(err.Error(), "package clause") {
		t.Errorf("err = %v, want a hint that mentions the package clause and what the harness adds", err)
	}
}

// A model listed a "types" entry that is only a package clause and a comment
// (the brief says there are no shared types): it declares nothing, so it is
// dropped instead of refused three times.
func TestNormalizeReplyDropsATypeThatDeclaresNothing(t *testing.T) {
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	reply := `{"types": [{"id": "types", "package": "internal/types", "file": "internal/types/types.go", "decl": "// Package types holds shared declarations.\npackage types", "uses": []},` +
		`{"id": "slug", "package": "x", "file": "internal/x/slug.go", "decl": "type Slug string", "uses": []}],` +
		` "functions": [{"id": "fn-make", "uses": ["types", "slug"]}]}`
	out, notes, err := normalizeReply(doc, reply, replyComponent)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Types []struct {
			ID string `json:"id"`
		} `json:"types"`
		Functions []struct {
			Uses []string `json:"uses"`
		} `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Types) != 1 || r.Types[0].ID != "slug" {
		t.Errorf("types = %+v, want only slug", r.Types)
	}
	if len(r.Functions) != 1 || strings.Join(r.Functions[0].Uses, ",") != "slug" {
		t.Errorf("functions = %+v, want uses [slug]", r.Functions)
	}
	if notes.EmptyTypes != 1 {
		t.Errorf("notes.EmptyTypes = %d, want 1", notes.EmptyTypes)
	}
}

// Gemma wrapped each type entry in an object named for its kind,
// {"type": {...}}, six replies in a row (run gm-2026-10-30-031): the entry is
// unwrapped, and a wrapper with other keys beside it is left for the schema.
func TestNormalizeReplyUnwrapsAnEntryWrappedInItsKind(t *testing.T) {
	reply := `{"components": [{"component": {"id": "core", "package": "retryx"}}], "types": [` +
		`{"type": {"id": "sleeper", "package": "retryx", "file": "sleeper.go", "decl": "type Sleeper func()", "uses": []}},` +
		`{"type": {"id": "odd"}, "id": "odd2"}]}`
	out, notes, err := normalizeReply(nil, reply, replyOutline)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Components []map[string]any `json:"components"`
		Types      []map[string]any `json:"types"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Components) != 1 || r.Components[0]["id"] != "core" {
		t.Errorf("components = %v, want the unwrapped component", r.Components)
	}
	if len(r.Types) != 2 || r.Types[0]["id"] != "sleeper" || r.Types[0]["decl"] != "type Sleeper func()" {
		t.Errorf("types = %v, want the first unwrapped", r.Types)
	}
	if _, kept := r.Types[1]["type"]; !kept {
		t.Errorf("a wrapper with other keys was changed: %v", r.Types[1])
	}
	if notes.Unwrapped != 2 {
		t.Errorf("notes.Unwrapped = %d, want 2", notes.Unwrapped)
	}
}
