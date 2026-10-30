package planner

import (
	"strings"
	"testing"
)

// No error a Coverage parse path returns may quote the reply.
func TestCoverageParseErrorsNeverQuoteTheReply(t *testing.T) {
	c := replyCanary
	reqs := []Requirement{
		{ID: "F1", Kind: ReqFeature, Name: "Greeting", Text: "t", Line: 1},
		{ID: "A1", Kind: ReqAcceptance, Text: "`go build ./...` succeeds.", Line: 2},
	}
	fn := func(id, comp, sig, file string) string {
		return `{"id": "` + id + `", "package": "x", "file": "` + file + `", "signature": "` + sig + `", "doc": "d", "uses": [], "component": "` + comp + `"}`
	}
	tests := func(name, cmd string) string {
		return `"root_tests": [{"requirement": "A1", "name": "` + name + `", "command": "` + cmd + `"}]`
	}
	replies := []string{
		"prose " + c,
		`{"map": "` + c + `"}`,
		`{"map": [{"requirement": "` + c + `", "nodes": []}]}`,
		`{"map": [{"requirement": "F1", "nodes": "` + c + `"}]}`,
		`{"root_tests": [{"requirement": "` + c + `", "name": "n", "command": "true"}]}`,
		`{` + tests(" ", c) + `}`,
		`{` + tests(c, " ") + `}`,
		`{"` + c + `": 1} ` + c,
		`{"map": [], "types": "` + c + `"}`,
		`{"map": [], "functions": [{"id": 5, "` + c + `": "` + c + `"}]}`,
		`{"map": [], "functions": [` + fn("fn-a", "greeting", "func "+c+"(", "internal/x/a.go") + `]}`,
		`{"map": [], "functions": [` + fn("fn-a", c, "func A()", "internal/x/a.go") + `]}`,
		`{"map": [], "functions": [` + fn("fn-a", "greeting", "func A()", "../"+c+".go") + `]}`,
		`{"map": [], "functions": [` + fn("fn-a", "greeting", "func A()", c+".txt") + `]}`,
		`{"map": [], "functions": [` + fn(c, "greeting", "func A()", "internal/x/a.go") + `]}`,
		`{"map": [], "functions": [` + fn("greeting", "greeting", "func A()", "internal/x/a.go") + `]}`,
		`{"map": [], "functions": [` + fn("fn-a", "greeting", "type "+c+" int", "internal/x/a.go") + `]}`,
		`{"map": [], "types": [{"id": "` + c + `", "package": "x", "file": "internal/x/e.go", "decl": "type T int"}]}`,
		`{"map": [], "types": [{"id": "t", "package": "x", "file": "/` + c + `.go", "decl": "type T int"}]}`,
	}
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range replies {
		_, _, err := parseCoverageFill(r, reqs, base, testRunID)
		if err == nil {
			t.Errorf("reply %d: want an error", i)
		} else if strings.Contains(err.Error(), c) {
			t.Errorf("reply %d: error quotes the reply: %v", i, err)
		}
	}
	for i, r := range replies[:8] {
		if _, err := ParseCoverageReply(r, reqs); err == nil || strings.Contains(err.Error(), c) {
			t.Errorf("ParseCoverageReply %d: err = %v", i, err)
		}
	}
}

// Run gm-2026-10-30-055: Gemma wrote "F2 (line 28)" for F2, on every try. A
// reference that is a known id followed by other text is that id; one whose
// leading word is not an id stays unknown.
func TestCoverageReplyReadsAnIDWithTextAfterIt(t *testing.T) {
	reqs := []Requirement{{ID: "F2", Kind: ReqFeature, Name: "Parse", Text: "t", Line: 28}, {ID: "A1", Kind: ReqAcceptance, Text: "t", Line: 30}}
	r, err := ParseCoverageReply(`{"map": [{"requirement": "F2 (line 28)", "nodes": ["fn-a"]}], "root_tests": [{"requirement": "A1: builds", "name": "n", "command": "true"}]}`, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Map[0].Requirement != "F2" || r.RootTests[0].Requirement != "A1" {
		t.Errorf("map %q root %q", r.Map[0].Requirement, r.RootTests[0].Requirement)
	}
	for _, bad := range []string{"F22", "F", "X2 (line 28)"} {
		if _, err := ParseCoverageReply(`{"map": [{"requirement": "`+bad+`", "nodes": []}]}`, reqs); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// Run gm-2026-10-30-073: "cmd && exit 1 || exit 0" asserts that cmd fails, but
// the gate reads "|| exit 0" as a masked failure. A single pipeline in that
// form becomes "! { cmd; }"; a chain stays as written, since negating it
// would also pass when an earlier step failed.
func TestCoverageReplyNegatesAnExpectedFailure(t *testing.T) {
	reqs := []Requirement{{ID: "A1", Kind: ReqAcceptance, Text: "t", Line: 1}, {ID: "A2", Kind: ReqAcceptance, Text: "t", Line: 2}}
	r, err := ParseCoverageReply(`{"root_tests": [`+
		`{"requirement": "A1", "name": "n", "command": "printf 'x\\n' | linkcheck --workers 0 - 2>/dev/null && exit 1 || exit 0"},`+
		`{"requirement": "A2", "name": "m", "command": "serve && linkcheck - && exit 1 || exit 0"}]}`, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.RootTests[0].Command; got != "! { printf 'x\\n' | linkcheck --workers 0 - 2>/dev/null; }" {
		t.Errorf("single pipeline = %q", got)
	}
	if got := r.RootTests[1].Command; got != "serve && linkcheck - && exit 1 || exit 0" {
		t.Errorf("a chain was rewritten: %q", got)
	}
}
