package planner

import (
	"encoding/json"
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
	doc, more, _, err := mergePass(base, "greeting", good, testRunID)
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
	later, _, _, err := mergePass(doc, "greeting", `{"functions": [`+fn("fn-a", "func A()", "internal/x/a.go", `"fn-later"`)+`]}`, testRunID)
	if err != nil || strings.Join(unresolvedUses(later), ",") != "fn-later" {
		t.Errorf("deferred reference: err %v unresolved %v", err, unresolvedUses(later))
	}

	bad := []struct{ name, reply, want string }{
		{"signature is not Go", `{"functions": [` + fn("fn-a", "A(name) string", "internal/x/a.go", ``) + `]}`, "not valid Go"},
		{"signature is two declarations", `{"functions": [` + fn("fn-a", "func A() {}\\nfunc B()", "internal/x/a.go", ``) + `]}`, "exactly one function"},
		{"file leaves the repo", `{"functions": [` + fn("fn-a", "func A()", "../a.go", ``) + `]}`, "inside the repository"},
		{"file is not Go", `{"functions": [` + fn("fn-a", "func A()", "internal/x/a.txt", ``) + `]}`, "not a Go file"},
		{"not json", "here are the functions", "not a JSON object"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, _, _, err := mergePass(doc, "greeting", c.reply, testRunID)
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

// A repeated id keeps the first emission; identical repeats are silent, a
// different one is reported by bounded id only.
func TestMergeKeepsTheFirstEmission(t *testing.T) {
	const canary = "CANARY-decl-text"
	list := []any{map[string]any{"id": "a", "v": "first"}}
	in := []map[string]any{
		{"id": "a", "v": "first"}, // identical: silent
		{"id": "a", "v": canary},  // different: ignored, reported
		{"id": "b", "v": "x"},
		{"id": "b", "v": canary},
		{"id": "has spaces " + canary, "v": "1"},
		{"id": "has spaces " + canary, "v": "2"},
	}
	out, added, ignored := mergeByID(list, in, 0, "type", nil, nil)
	if len(out) != 3 || added != 2 {
		t.Fatalf("out %d added %d, want 3 and 2", len(out), added)
	}
	if out[0].(map[string]any)["v"] != "first" {
		t.Error("the first emission was replaced")
	}
	want := []string{`type "a"`, `type "b"`, fmt.Sprintf("type <%d bytes>", len("has spaces "+canary))}
	if strings.Join(ignored, "|") != strings.Join(want, "|") {
		t.Errorf("ignored = %v, want %v", ignored, want)
	}
	for _, n := range ignored {
		if strings.Contains(n, "CANARY") {
			t.Errorf("ignored entry quotes content: %s", n)
		}
	}
}

func TestBoundedID(t *testing.T) {
	const canary = "CANARY ID with spaces"
	long := strings.Repeat("a", 10000)
	if got := boundedID("fine-id"); got != `"fine-id"` {
		t.Errorf("boundedID = %s", got)
	}
	if got := boundedID(canary); got != fmt.Sprintf("<%d bytes>", len(canary)) {
		t.Errorf("boundedID(bad syntax) = %s", got)
	}
	// An id over 64 bytes is reported by length only: a cut prefix would read
	// like a real id.
	if got := boundedID(long); got != "<10000 bytes>" {
		t.Errorf("boundedID(10KB) = %s, want its length only", got)
	}
	if got := boundedID(strings.Repeat("a", 64)); len(got) != 66 {
		t.Errorf("boundedID(64 bytes) = %s, want the id in quotes", got)
	}
	if got := boundedID(strings.Repeat("a", 65)); got != "<65 bytes>" {
		t.Errorf("boundedID(65 bytes) = %s", got)
	}
	if got := boundedModule("example.com/" + strings.Repeat("a", 70)); !strings.HasPrefix(got, "<") {
		t.Errorf("boundedModule(long) = %s, want its length only", got)
	}
}

// Every model-supplied id in a validation error is bounded, even a 10KB one.
func TestValidationErrorsBoundEveryID(t *testing.T) {
	huge := strings.Repeat("a", 10000)
	doc := func(comps, types, fns string) map[string]any {
		var d map[string]any
		raw := `{"spec_version": "2.0", "brief_id": "` + testRunID + `", "revision": 0, "module": "example.com/x",
		 "conventions": {"layout": ["a"], "naming": ["a"], "errors": "e", "testing": "t"},
		 "components": [` + comps + `], "types": [` + types + `], "functions": [` + fns + `]}`
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	comp := func(id string) string { return `{"id": "` + id + `", "package": "x", "exports": []}` }
	typ := func(id, file string) string {
		return `{"id": "` + id + `", "package": "x", "file": "` + file + `", "decl": "// T.\ntype T int"}`
	}
	fn := func(id, comp, file, sig string) string {
		return `{"id": "` + id + `", "component": "` + comp + `", "package": "x", "file": "` + file + `", "signature": "` + sig + `", "doc": "d", "uses": []}`
	}
	cases := map[string]map[string]any{
		"component id":        doc(comp("Bad"+huge), "", ""),
		"reserved component":  doc(comp("logs"), "", ""),
		"duplicate component": doc(comp(huge)+", "+comp(huge), "", ""),
		"type file":           doc(comp("a"), typ(huge, "../x.go"), ""),
		"function component":  doc(comp("a"), "", fn("fn-"+huge, "b", "internal/x/a.go", "func A()")),
		"function is comp":    doc(comp("fn-"+huge), "", fn("fn-"+huge, "fn-"+huge, "internal/x/a.go", "func A()")),
		"function file":       doc(comp("a"), "", fn("fn-"+huge, "a", "../a.go", "func A()")),
		"function signature":  doc(comp("a"), "", fn("fn-"+huge, "a", "internal/x/a.go", "A(")),
	}
	for name, d := range cases {
		for _, check := range []func(map[string]any) error{
			func(d map[string]any) error { return validateOutlineShape(d, testRunID) },
			func(d map[string]any) error { _, err := validateContractDoc(d, testRunID); return err },
		} {
			err := check(d)
			if err == nil {
				t.Errorf("%s: want an error", name)
				continue
			}
			if len(err.Error()) > 600 || strings.Contains(err.Error(), strings.Repeat("a", 100)) {
				t.Errorf("%s: error carries a long id (%d bytes): %.120s", name, len(err.Error()), err)
			}
		}
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
		"unknown owner": `{"functions": [{"id": "fn-a", "component": "CANARY", "package": "x", "file": "internal/x/a.go", "signature": "func A()", "doc": "d", "uses": []}]}`,
	} {
		_, _, err := mergeRepair(base, reply, testRunID)
		if err == nil {
			t.Errorf("%s: want an error", name)
			continue
		}
		if strings.Contains(err.Error(), "CANARY") {
			t.Errorf("%s: error quotes reply text: %v", name, err)
		}
	}
}

// A model's exports in the outline are replaced by the harness's.
func TestOutlineIgnoresModelExports(t *testing.T) {
	text := strings.Replace(okOutline, `{"id": "greeting", "package": "x"}`, `{"id": "greeting", "package": "x", "exports": ["fn-nope"]}`, 1)
	doc, _, err := parseOutline(text, testRunID)
	if err != nil {
		t.Fatalf("an outline naming unwritten exports must not be an unusable reply: %v", err)
	}
	for _, c := range objects(doc["components"]) {
		if len(strList(c["exports"])) != 0 {
			t.Errorf("component %v keeps the model's exports %v", c["id"], c["exports"])
		}
	}
}

// A schema failure names at most the first 5 pointers and counts the rest.
func TestSchemaErrorBoundsThePointers(t *testing.T) {
	var types []string
	for i := 0; i < 20; i++ {
		types = append(types, `{"id": "x`+fmt.Sprint(i)+`", "package": "x", "file": "internal/x/a.go"}`)
	}
	d := map[string]any{}
	raw := `{"spec_version": "2.0", "brief_id": "` + testRunID + `", "revision": 0, "module": "example.com/x",
	 "conventions": {"layout": ["a"], "naming": ["a"], "errors": "e", "testing": "t"},
	 "components": [], "types": [` + strings.Join(types, ",") + `], "functions": []}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	err := validateOutlineShape(d, testRunID)
	if err == nil {
		t.Fatal("want a schema error")
	}
	if n := strings.Count(err.Error(), "is missing decl"); n != 5 || !strings.Contains(err.Error(), "and 15 more") {
		t.Errorf("err = %v, want 5 nodes and 15 more", err)
	}
}

// "more" with nothing new ends the component instead of failing the reply.
func TestMergePassMoreWithNothingNewEndsTheComponent(t *testing.T) {
	doc, _, err := parseOutline(okOutline, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	_, more, _, err := mergePass(doc, "greeting", `{"types": [], "functions": [], "more": true}`, testRunID)
	if err != nil || more {
		t.Errorf("more %v err %v, want done and no error", more, err)
	}
}
