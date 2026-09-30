package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
)

const leafContract = `{"spec_version": "2.0", "brief_id": "gm-2026-09-29-900", "revision": 0, "module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "types": [{"id": "name-error", "package": "x", "file": "internal/x/errors.go", "decl": "type NameError struct{}"}],
 "functions": [
  {"id": "fn-greet", "package": "x", "file": "internal/x/greet.go", "signature": "func Greet(name string, times int) (string, error)", "doc": "d", "uses": ["name-error"], "component": "greeting"},
  {"id": "fn-join", "package": "x", "file": "internal/x/join.go", "signature": "func Join(string, string) string", "doc": "d", "uses": [], "component": "greeting"}],
 "components": [{"id": "greeting", "package": "x", "exports": []}]}`

const okDraft = `{"id": "fn-greet", "title": "Greet", "description": "Greets.", "node_class": "validation", "depends_on": [],
 "contract": {"inputs": [{"name": "name", "type": "string"}, {"name": "times", "type": "int"}],
  "outputs": [{"name": "message", "type": "string"}, {"name": "err", "type": "error"}],
  "errors": [{"when": "name is empty", "returns": "*NameError"}], "side_effects": []}}`

// What every leaf must carry, checked by code. Each bad draft is the good one
// with one thing removed or broken.
func TestLeafChecks(t *testing.T) {
	c, err := contract.Load([]byte(leafContract))
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "gm-2026-09-29-900", brief: &brief.Brief{}, reqs: []Requirement{{ID: "C1", Kind: ReqConstraint, Text: "Standard library only."}}}
	greet := c.Functions[:1]

	drafts, classes, err := normalizeDrafts("["+okDraft+"]", "greeting", greet, c, r)
	if err != nil {
		t.Fatalf("the good draft was refused: %v", err)
	}
	if classes["fn-greet"] != "validation" || drafts[0]["brief_ref"] != "#architecture" {
		t.Errorf("classes = %v, brief_ref = %v", classes, drafts[0]["brief_ref"])
	}

	bad := []struct{ name, edit, with, want string }{
		{"no input for a parameter", `{"name": "times", "type": "int"}`, `{"name": "count", "type": "int"}`, `no entry for parameter "times"`},
		{"an input without a type", `{"name": "name", "type": "string"}`, `{"name": "name", "type": " "}`, `input "name" has no type`},
		{"an output missing", `, {"name": "err", "type": "error"}`, ``, "outputs has 1 entries for 2 results"},
		{"an output without a type", `{"name": "err", "type": "error"}`, `{"name": "err", "type": ""}`, `output 2 has no type`},
		{"returns error with no errors entry", `{"when": "name is empty", "returns": "*NameError"}`, ``, "returns error but errors lists no condition"},
		{"an errors entry without returns", `"returns": "*NameError"`, `"returns": ""`, "needs both when and returns"},
		{"unknown node_class", `"node_class": "validation"`, `"node_class": "magic"`, `node_class (5 bytes) is not one of pure, validation`},
		{"no node_class", `"node_class": "validation", `, ``, `node_class (0 bytes) is not one of`},
		{"depends on an id nobody declared", `"depends_on": []`, `"depends_on": ["fn-ghost"]`, "unknown id"},
		{"a field the schema does not have", `"title": "Greet"`, `"title": "Greet", "notes": "x"`, "additional properties"},
		{"no title", `"title": "Greet", `, ``, "title"},
		{"no contract", `"contract": {`, `"agreement": {`, "contract is missing"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			draft := strings.Replace(okDraft, b.edit, b.with, 1)
			if draft == okDraft {
				t.Fatalf("the edit %q did not change the draft", b.edit)
			}
			_, _, err := normalizeDrafts("["+draft+"]", "greeting", greet, c, r)
			if err == nil || !strings.Contains(err.Error(), b.want) {
				t.Errorf("err = %v, want it to contain %q", err, b.want)
			}
		})
	}

	t.Run("a node nobody asked for", func(t *testing.T) {
		extra := strings.Replace(okDraft, `"id": "fn-greet"`, `"id": "fn-extra"`, 1)
		if _, _, err := normalizeDrafts("["+okDraft+","+extra+"]", "greeting", greet, c, r); err == nil || !strings.Contains(err.Error(), "not one of the functions asked for") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a function left out", func(t *testing.T) {
		if _, _, err := normalizeDrafts("[]", "greeting", greet, c, r); err == nil || !strings.Contains(err.Error(), "no node for fn-greet") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("the same node twice", func(t *testing.T) {
		if _, _, err := normalizeDrafts("["+okDraft+","+okDraft+"]", "greeting", greet, c, r); err == nil || !strings.Contains(err.Error(), "two nodes for fn-greet") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("unnamed parameters need one input each", func(t *testing.T) {
		join := c.Functions[1:]
		one := `{"id": "fn-join", "title": "Join", "description": "Joins.", "node_class": "pure",
 "contract": {"inputs": [{"name": "a", "type": "string"}], "outputs": [{"name": "s", "type": "string"}]}}`
		if _, _, err := normalizeDrafts("["+one+"]", "greeting", join, c, r); err == nil || !strings.Contains(err.Error(), "inputs has 1 entries for 2 parameters") {
			t.Errorf("err = %v", err)
		}
		two := strings.Replace(one, `[{"name": "a", "type": "string"}]`, `[{"name": "a", "type": "string"}, {"name": "b", "type": "string"}]`, 1)
		if _, _, err := normalizeDrafts("["+two+"]", "greeting", join, c, r); err != nil {
			t.Errorf("two inputs for two unnamed parameters were refused: %v", err)
		}
	})
	t.Run("an input may name a part of a parameter", func(t *testing.T) {
		part := strings.Replace(okDraft, `{"name": "name", "type": "string"}`, `{"name": "name.first", "type": "string"}`, 1)
		if _, _, err := normalizeDrafts("["+part+"]", "greeting", greet, c, r); err != nil {
			t.Errorf("refused: %v", err)
		}
	})
}

func TestWaves(t *testing.T) {
	got, err := waves(map[string][]string{"root": nil, "a": nil, "b": {"a"}, "c": {"a", "b"}, "d": {"c"}})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"root": 0, "a": 0, "b": 1, "c": 2, "d": 3} {
		if got[id] != want {
			t.Errorf("wave(%s) = %d, want %d", id, got[id], want)
		}
	}
	if _, err := waves(map[string][]string{"a": {"b"}, "b": {"a"}}); err == nil || !strings.Contains(err.Error(), "dependency cycle: a -> b -> a") {
		t.Errorf("cycle err = %v", err)
	}
	if _, err := waves(map[string][]string{"a": {"ghost"}}); err == nil || !strings.Contains(err.Error(), "unknown node") {
		t.Errorf("unknown dependency err = %v", err)
	}
}

// No error a Decompose parse path returns may quote the reply. Every case
// must fail, and the canary planted in it must not appear in the error.
func TestDecomposeErrorsNeverQuoteTheReply(t *testing.T) {
	c, err := contract.Load([]byte(leafContract))
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "gm-2026-09-29-900", brief: &brief.Brief{}}
	greet := c.Functions[:1]
	const canary = "CANARY-9b2f71"
	edit := func(from, to string) string {
		d := strings.Replace(okDraft, from, to, 1)
		if d == okDraft {
			t.Fatalf("edit %q changed nothing", from)
		}
		return "[" + d + "]"
	}
	replies := map[string]string{
		"prose":                  "prose " + canary,
		"an object not an array": `{"` + canary + `": 1}`,
		"wrong json type":        `[{"id": ` + `"` + canary + `"}] ` + canary,
		"truncated":              `[{"id": "` + canary,
		"unknown node id":        edit(`"id": "fn-greet"`, `"id": "`+canary+`"`),
		"node_class":             edit(`"node_class": "validation"`, `"node_class": "`+canary+`"`),
		"depends_on":             edit(`"depends_on": []`, `"depends_on": ["`+canary+`"]`),
		"extra property":         edit(`"title": "Greet"`, `"title": "Greet", "`+canary+`": "`+canary+`"`),
		"title of wrong type":    edit(`"title": "Greet"`, `"title": {"`+canary+`": 1}`),
		"input name, no type":    edit(`{"name": "times", "type": "int"}`, `{"name": "`+canary+`", "type": ""}`),
		"undeclared input":       edit(`{"name": "times", "type": "int"}`, `{"name": "`+canary+`", "type": "int"}`),
		"output without type":    edit(`{"name": "err", "type": "error"}`, `{"name": "`+canary+`", "type": ""}`),
		"errors entry":           edit(`"returns": "*NameError"`, `"returns": "", "when": "`+canary+`"`),
		"no contract":            edit(`"contract": {`, `"`+canary+`": {`),
		"contract of wrong type": edit(`"contract": {`, `"contract": "`+canary+`", "x": {`),
		"node twice":             "[" + okDraft + "," + okDraft + "]",
		"empty":                  "[]",
	}
	for name, reply := range replies {
		t.Run(name, func(t *testing.T) {
			_, _, err := normalizeDrafts(reply, "greeting", greet, c, r)
			if err == nil {
				t.Fatal("the reply was accepted")
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("the error quotes the reply: %v", err)
			}
		})
	}
}
