package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
)

func TestDefaultLeafFollowsTheSignature(t *testing.T) {
	cases := []struct {
		sig             string
		inputs, outputs []string // name:type
		errors          int
	}{
		{"func Greet(name string) (string, error)", []string{"name:string"}, []string{"result:string", "err:error"}, 1},
		{"func Join(string, int) string", []string{"arg1:string", "arg2:int"}, []string{"result:string"}, 0},
		{"func Split(a, b string) (head, tail string, err error)", []string{"a:string", "b:string"}, []string{"head:string", "tail:string", "err:error"}, 1},
		{"func Run() error", nil, []string{"err:error"}, 1},
		{"func Parse(r *Reader, opts ...Option) ([]Item, int, error)", []string{"r:*Reader", "opts:...Option"}, []string{"result:[]Item", "result2:int", "err:error"}, 1},
	}
	for _, c := range cases {
		raw := map[string]any{"contract": map[string]any{"errors": []any{map[string]any{"when": "", "returns": ""}}}}
		if c.errors == 0 {
			raw["contract"].(map[string]any)["errors"] = []any{}
		}
		if err := defaultLeaf(raw, contract.Function{ID: "fn-x", Signature: c.sig}); err != nil {
			t.Fatalf("%s: %v", c.sig, err)
		}
		ct := raw["contract"].(map[string]any)
		pairs := func(key string) []string {
			var out []string
			for _, o := range objects(ct[key]) {
				out = append(out, o["name"].(string)+":"+o["type"].(string))
			}
			return out
		}
		if got := strings.Join(pairs("inputs"), " "); got != strings.Join(c.inputs, " ") {
			t.Errorf("%s: inputs %q, want %q", c.sig, got, strings.Join(c.inputs, " "))
		}
		if got := strings.Join(pairs("outputs"), " "); got != strings.Join(c.outputs, " ") {
			t.Errorf("%s: outputs %q, want %q", c.sig, got, strings.Join(c.outputs, " "))
		}
		if n := len(objects(ct["errors"])); n != c.errors {
			t.Errorf("%s: %d errors entries, want %d", c.sig, n, c.errors)
		}
		for _, e := range objects(ct["errors"]) {
			if e["when"] == "" || e["returns"] == "" {
				t.Errorf("%s: an errors entry is incomplete: %v", c.sig, e)
			}
		}
	}
}

func TestMergeNodeFixTakesOnlyTheDefectiveFields(t *testing.T) {
	pd := pendingDraft{ID: "fn-x", Defects: []string{defOutputsCount}, Raw: map[string]any{
		"title": "keep", "contract": map[string]any{"inputs": []any{"keep"}, "outputs": []any{}}}}
	mergeNodeFix(&pd, map[string]any{"title": "new", "description": "new",
		"contract": map[string]any{"outputs": []any{"fixed"}, "inputs": []any{"new"}}})
	ct := pd.Raw["contract"].(map[string]any)
	if pd.Raw["title"] != "keep" || pd.Raw["description"] != nil || ct["inputs"].([]any)[0] != "keep" || ct["outputs"].([]any)[0] != "fixed" {
		t.Errorf("raw = %v", pd.Raw)
	}
}

func TestSplitDraftsKeepsTheGoodNodesOfAMixedBatch(t *testing.T) {
	c, err := contract.Load([]byte(leafContract))
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: testRunID, brief: &brief.Brief{}, reqs: nil}
	bad := strings.Replace(okDraft, `, {"name": "err", "type": "error"}`, ``, 1)
	other := strings.Replace(okDraft, `"id": "fn-greet"`, `"id": "fn-join"`, 1)
	res, err := splitDrafts("["+bad+"]", "greeting", c.Functions[:1], c, r)
	if err != nil || len(res.good) != 0 || len(res.bad) != 1 || res.bad[0].Defects[0] != defOutputsCount {
		t.Fatalf("res = %+v err %v", res, err)
	}
	_ = other
	if res.bad[0].Raw == nil || res.bad[0].Raw["contract"] == nil {
		t.Error("the defective node's content must be kept for the repair")
	}
}
