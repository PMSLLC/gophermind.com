package planner

import (
	"strings"
	"testing"
)

const testRunID = "gm-2026-09-29-900"

const okOutline = `{"module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "components": [{"id": "types", "package": "x"}, {"id": "greeting", "package": "x"}],
 "types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "type NameError struct{}"}]}`

func TestParseOutline(t *testing.T) {
	doc, err := parseOutline(okOutline, testRunID)
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
			_, err := parseOutline(strings.Replace(okOutline, c.edit, c.with, 1), testRunID)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestMergePass(t *testing.T) {
	base, err := parseOutline(okOutline, testRunID)
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

	bad := []struct{ name, reply, want string }{
		{"more with nothing new", `{"types": [], "functions": [], "more": true}`, "holds no function"},
		{"uses an id nobody declared", `{"functions": [` + fn("fn-a", "func A()", "internal/x/a.go", `"fn-later"`) + `]}`, "unknown id"},
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
