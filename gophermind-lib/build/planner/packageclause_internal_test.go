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
