package planner

import (
	"fmt"
	"strings"
	"testing"
)

const testRunID = "gm-2026-09-29-900"

const okOutline = `{"module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "components": [{"id": "types", "package": "x"}, {"id": "greeting", "package": "x"}],
 "types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "type NameError struct{}"}]}`

func TestParseOutline(t *testing.T) {
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if doc["brief_id"] != testRunID || doc["spec_version"] != "2.0" || len(objects(doc["components"])) != 2 {
		t.Errorf("doc = %v", doc)
	}
	for _, c := range objects(doc["components"]) {
		if _, ok := c["exports"].([]any); !ok {
			t.Errorf("component %v has no exports array", c["id"])
		}
	}

	bad := []struct{ name, edit, with, want string }{
		{"no components", `"components": [{"id": "types", "package": "x"}, {"id": "greeting", "package": "x"}]`, `"components": []`, "lists no component"},
		{"reserved id logs", `"id": "greeting"`, `"id": "logs"`, "reserved"},
		{"reserved id outline", `"id": "greeting"`, `"id": "outline"`, "reserved"},
		{"duplicate component", `"id": "greeting"`, `"id": "types"`, "duplicate component"},
		{"component named like the run", `"id": "greeting"`, `"id": "gm-2026-09-29-900"`, "is the run id"},
		{"type file leaves the repo", `internal/x/errors.go`, `../x/errors.go`, "inside the repository"},
		{"type file is absolute", `internal/x/errors.go`, `/etc/errors.go`, "relative path"},
		{"no conventions", `"errors": "return error"`, `"mistakes": "x"`, "conventions"},
		{"not json", okOutline, "an outline in prose", "not a JSON object"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := parseOutline(strings.Replace(okOutline, c.edit, c.with, 1), testRunID)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestMergePass(t *testing.T) {
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	fn := func(id, sig, file, uses string) string {
		return `{"id": "` + id + `", "package": "x", "file": "` + file + `", "signature": "` + sig + `", "doc": "d", "uses": [` + uses + `]}`
	}
	good := `{"types": [], "functions": [` + fn("fn-greet", "func Greet(name string) (string, error)", "internal/x/greet.go", `"name-error"`) + `], "more": true}`
	doc, more, err := mergePass(base, "greeting", good, testRunID)
	if err != nil || !more {
		t.Fatalf("mergePass = more %v, %v", more, err)
	}
	if fns := objects(doc["functions"]); len(fns) != 1 || fns[0]["component"] != "greeting" {
		t.Errorf("functions = %v (component must be set by the harness)", fns)
	}
	if len(objects(base["functions"])) != 0 {
		t.Error("mergePass changed the document it was given")
	}

	// A reference to an id nobody has declared yet is not refused here: a later
	// component may declare it. It is reported as unresolved instead.
	later, _, err := mergePass(doc, "greeting", `{"functions": [`+fn("fn-a", "func A()", "internal/x/a.go", `"fn-later"`)+`]}`, testRunID)
	if err != nil || strings.Join(unresolvedUses(later), ",") != "fn-later" {
		t.Errorf("deferred reference: err %v unresolved %v", err, unresolvedUses(later))
	}

	bad := []struct{ name, reply, want string }{
		{"more with nothing new", `{"types": [], "functions": [], "more": true}`, "holds no function"},
		{"signature is not Go", `{"functions": [` + fn("fn-a", "A(name) string", "internal/x/a.go", ``) + `]}`, "not valid Go"},
		{"signature is two declarations", `{"functions": [` + fn("fn-a", "func A() {}\\nfunc B()", "internal/x/a.go", ``) + `]}`, "exactly one function"},
		{"file leaves the repo", `{"functions": [` + fn("fn-a", "func A()", "../a.go", ``) + `]}`, "inside the repository"},
		{"file is not Go", `{"functions": [` + fn("fn-a", "func A()", "internal/x/a.txt", ``) + `]}`, "not a Go file"},
		{"id repeats", `{"functions": [` + fn("fn-greet", "func Greet()", "internal/x/g.go", ``) + `]}`, "duplicate id"},
		{"not json", "here are the functions", "not a JSON object"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := mergePass(doc, "greeting", c.reply, testRunID)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestParseSignature(t *testing.T) {
	for _, sig := range []string{
		"func Greet(name string) (string, error)",
		"func (s *Server) Routes() http.Handler",
		"func Map[T any](in []T, f func(T) T) []T",
	} {
		if _, err := parseSignature(sig); err != nil {
			t.Errorf("parseSignature(%q): %v", sig, err)
		}
	}
	for _, sig := range []string{"", "Greet(name string)", "type T int", "func"} {
		if _, err := parseSignature(sig); err == nil {
			t.Errorf("parseSignature(%q) accepted a non-function", sig)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Greeting": "greeting", "CRM sync": "crm-sync", "  KPI Tracking & Evaluation ": "kpi-tracking-evaluation"} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// Model-supplied ids in the new error paths are bounded and syntax-checked.
func TestOutlineMergeErrorsNeverQuoteArbitraryIDs(t *testing.T) {
	const canary = "CANARY ID with spaces"
	long := strings.Repeat("a", 200)
	first := `{"module": "example.com/x", "conventions": {"layout": ["a"], "naming": ["a"], "errors": "e", "testing": "t"},
	 "components": [{"id": "` + canary + `", "package": "x"}, {"id": "` + long + `", "package": "x"}], "more": true}`
	doc, _, _, err := mergeOutline(nil, nil, first, testRunID, nil)
	if err == nil {
		// A pass that is locally valid is kept; the ids are refused at the end.
		_ = doc
	}
	for _, c := range []struct{ name, text string }{
		{"duplicate in one reply", `{"components": [{"id": "` + canary + `", "package": "x"}, {"id": "` + canary + `", "package": "x"}]}`},
		{"long duplicate in one reply", `{"components": [{"id": "` + long + `", "package": "x"}, {"id": "` + long + `", "package": "x"}]}`},
	} {
		_, _, _, err := mergeOutline(nil, nil, c.text, testRunID, nil)
		if err == nil {
			t.Fatalf("%s: want an error", c.name)
		}
		if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), long) || strings.Contains(err.Error(), "CANARY") {
			t.Errorf("%s: error quotes the id: %v", c.name, err)
		}
	}
	if got := boundedID("fine-id"); got != `"fine-id"` {
		t.Errorf("boundedID = %s", got)
	}
	if got := boundedID(canary); got != fmt.Sprintf("<%d bytes>", len(canary)) {
		t.Errorf("boundedID(bad syntax) = %s", got)
	}
	if got := boundedID(long); len(got) > 70 || !strings.HasPrefix(got, `"aaa`) {
		t.Errorf("boundedID(long) = %s, want at most 64 bytes of the id", got)
	}
}

func TestMergeRepairErrorsNeverQuoteReplyText(t *testing.T) {
	base, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	const canary = "CANARY fn id"
	for name, reply := range map[string]string{
		"no component":  `{"functions": [{"id": "` + canary + `", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []}]}`,
		"not json":      "CANARY prose",
		"conflicting":   `{"types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "// CANARY\ntype NameError int"}]}`,
		"unknown owner": `{"functions": [{"id": "fn-a", "component": "CANARY", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []}]}`,
	} {
		_, err := mergeRepair(base, reply, testRunID, nil)
		if err == nil {
			t.Errorf("%s: want an error", name)
			continue
		}
		if strings.Contains(err.Error(), "CANARY") {
			t.Errorf("%s: error quotes reply text: %v", name, err)
		}
	}
}
