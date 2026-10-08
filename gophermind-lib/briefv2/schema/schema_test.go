package schema_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/schema"
)

const exampleTree = "../testdata/example/tree/gm-2026-09-29-001"

func TestExampleNodesValidate(t *testing.T) {
	n := 0
	err := filepath.WalkDir(exampleTree, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "contracts.json" {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if verr := schema.Validate(schema.KindNode, raw); verr != nil {
			t.Errorf("%s: %v", p, verr)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("expected 7 example node files, found %d", n)
	}
}

func TestExampleContractsValidate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(exampleTree, "contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(schema.KindContract, raw); err != nil {
		t.Fatal(err)
	}
}

// mutate loads a fixture, applies fn to its decoded form, and re-encodes it.
func mutate(t *testing.T, rel string, fn func(map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(exampleTree, rel))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRejects(t *testing.T) {
	cases := []struct {
		name string
		doc  []byte
		kind schema.Kind
	}{
		{"function test without command", mutate(t, "registration/fn-validate-email.json", func(m map[string]any) {
			for _, x := range m["tests"].([]any) {
				delete(x.(map[string]any), "command")
			}
		}), schema.KindNode},
		{"component without parent", mutate(t, "registration/component.json", func(m map[string]any) { delete(m, "parent") }), schema.KindNode},
		{"bad status", mutate(t, "root.json", func(m map[string]any) { m["status"] = "done" }), schema.KindNode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := schema.Validate(c.kind, c.doc); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestBriefErrorsNameTheField(t *testing.T) {
	cases := map[string]string{
		"spec_version": `{"id":"gm-2026-09-29-001","title":"t","language":"go","repo":"r","base_branch":"main","landing":"commit","on_ambiguity":"halt"}`,
		"language":     `{"spec_version":"2.0","id":"gm-2026-09-29-001","title":"t","language":"python","repo":"r","base_branch":"main","landing":"commit","on_ambiguity":"halt"}`,
	}
	for field, doc := range cases {
		err := schema.Validate(schema.KindBrief, []byte(doc))
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: want an error naming the field, got %v", field, err)
		}
	}
}

func TestUnknownKindAndBadJSON(t *testing.T) {
	if err := schema.Validate("nope", []byte(`{}`)); err == nil {
		t.Error("unknown kind must error")
	}
	if err := schema.Validate(schema.KindNode, []byte(`{`)); err == nil {
		t.Error("malformed JSON must error")
	}
}

func TestRawReturnsTheEmbeddedSchema(t *testing.T) {
	raw, err := schema.Raw(schema.KindContract)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Raw(KindContract) is not JSON: %v", err)
	}
	if doc["$id"] != "https://gophermind.local/schema/contract/2.0" {
		t.Errorf("$id = %v", doc["$id"])
	}
	if _, err := schema.Raw("nope"); err == nil {
		t.Error("unknown kind must error")
	}
}
func TestNodeGroupsValidateAndRejectBadShapes(t *testing.T) {
	base := func(extra string) []byte {
		return []byte(`{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"c","title":"t","description":"d","brief_ref":"#x","status":"pending","wave":0,
"contract":{"package":"p","file":"p/a.go","signature":"func A()","inputs":[],"outputs":[]},
"tests":[{"name":"n","level":"unit","given":"g","expect":"e","command":"go test ./p"}]` + extra + `}`)
	}
	good := map[string]string{
		"rationale":       `,"rationale":"Serves the registration form so a visitor can sign up."`,
		"construction":    `,"construction":{"approach_chosen":"a","steps":["one","two"]}`,
		"alternatives":    `,"alternatives":[{"approach":"x","rejected_because":"y"}]`,
		"alternatives na": `,"alternatives":{"not_applicable":"there is only one sensible way to write this"}`,
		"security":        `,"security":{"trust_boundary":"none"}`,
		"performance":     `,"performance":{"complexity":"O(1)","max_latency_ms":null,"concurrency":"none","hot_path":false}`,
		"observability":   `,"observability":{"log_events":[{"level":"warn","msg":"m","fields":["f"]}],"trace_span":null}`,
		"portability":     `,"portability":{"os":["linux"],"arch":["amd64"],"go_min":"1.22","cgo":false}`,
		"profile_hooks":   `,"profile_hooks":["bench"]`,
		"refactor_notes":  `,"refactor_notes":[{"what":"w","why":"y","when":"z"}]`,
		"ids":             `,"requirement_ids":["C1","A2","F3"],"decision_ids":["q1","decompose-greeting-q1"],"open_questions":[]`,
		"decisions":       `,"decisions":[{"id":"q1","question":"Which store?","kind":"decision","options":["memory","postgres"],"recommended":"memory","answer":"memory","answered_by":"accepted","round":1,"raised_by":"clarify","settled_at":"2026-10-08T00:00:00Z","history":[{"at":"2026-10-08T00:01:00Z","answer":"postgres"}]}]`,
	}
	for name, extra := range good {
		if err := schema.Validate(schema.KindNode, base(extra)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := map[string]string{
		"short rationale":    `,"rationale":"too short"`,
		"one step":           `,"construction":{"approach_chosen":"a","steps":["one"]}`,
		"short na reason":    `,"alternatives":{"not_applicable":"none"}`,
		"na plus content":    `,"alternatives":{"not_applicable":"there is only one sensible way to write this","approach":"x"}`,
		"bad boundary":       `,"security":{"trust_boundary":"sideways"}`,
		"bad concurrency":    `,"performance":{"complexity":"O(1)","concurrency":"maybe","hot_path":false}`,
		"bad log level":      `,"observability":{"log_events":[{"level":"loud","msg":"m"}]}`,
		"bad go_min":         `,"portability":{"os":["linux"],"arch":["amd64"],"go_min":"one","cgo":false}`,
		"bad hook":           `,"profile_hooks":["gdb"]`,
		"empty refactor":     `,"refactor_notes":[]`,
		"bad requirement id": `,"requirement_ids":["X1"]`,
		"unknown group":      `,"zzz":{}`,
		"decision no answer": `,"decisions":[{"id":"q1","question":"Which store?","kind":"decision","answered_by":"human","raised_by":"clarify"}]`,
		"decision bad actor": `,"decisions":[{"id":"q1","question":"Q?","kind":"decision","answer":"a","answered_by":"robot","raised_by":"clarify"}]`,
	}
	for name, extra := range bad {
		if err := schema.Validate(schema.KindNode, base(extra)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	tests := func(item string) []byte {
		return []byte(`{"spec_version":"2.0","id":"fn-a","kind":"function","parent":"c","title":"t","description":"d","brief_ref":"#x","status":"pending","wave":0,
"contract":{"package":"p","file":"p/a.go","signature":"func A()","inputs":[],"outputs":[],"errors":[{"when":"w","returns":"r","kind":"sentinel","test":"n"}]},
"tests":[` + item + `]}`)
	}
	if err := schema.Validate(schema.KindNode, tests(`{"name":"n","level":"unit","given":"g","expect":"e","command":"c","polarity":"negative","covers":"error:1"}`)); err != nil {
		t.Errorf("test polarity and covers: %v", err)
	}
	for _, item := range []string{
		`{"name":"n","level":"unit","given":"g","expect":"e","command":"c","polarity":"sideways"}`,
		`{"name":"n","level":"unit","given":"g","expect":"e","command":"c","covers":"everything"}`,
	} {
		if err := schema.Validate(schema.KindNode, tests(item)); err == nil {
			t.Errorf("accepted %s", item)
		}
	}
}
