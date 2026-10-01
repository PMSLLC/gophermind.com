package planner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/contract"
)

// The fixtures are nodes the model really sent in rehearsal 10 (the AIVS brief):
// side_effects as objects instead of strings, and a node_class written into
// model_tier.
type realNode struct {
	Component string         `json:"component"`
	Raw       map[string]any `json:"raw"`
}

func loadRealNode(t *testing.T, name string) realNode {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "rehearsal10", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var n realNode
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

// contractFor builds a contract that declares the node's function (signature
// from the node itself) and every id its depends_on names as a type.
func contractFor(t *testing.T, n realNode) (*contract.Contracts, contract.Function) {
	t.Helper()
	id, _ := n.Raw["id"].(string)
	ct, _ := n.Raw["contract"].(map[string]any)
	sig, _ := ct["signature"].(string)
	var types, fns []string
	for _, d := range strList(n.Raw["depends_on"]) {
		if strings.HasPrefix(d, "fn-") {
			fns = append(fns, fmt.Sprintf(`{"id": %q, "package": "x", "file": "internal/x/d.go", "signature": "func Dep()", "doc": "d", "uses": [], "component": %q}`, d, n.Component))
		} else {
			types = append(types, fmt.Sprintf(`{"id": %q, "package": "x", "file": "internal/x/t.go", "decl": "type T int"}`, d))
		}
	}
	doc := `{"spec_version": "2.0", "brief_id": "` + testRunID + `", "revision": 0, "module": "example.com/x",
 "conventions": {"layout": ["a"], "naming": ["a"], "errors": "e", "testing": "t"},
 "types": [` + strings.Join(types, ",") + `],
 "functions": [` + strings.Join(append(fns, ""), ",") + `{"id": ` + fmt.Sprintf("%q", id) + `, "package": "x", "file": "internal/x/a.go", "signature": ` + fmt.Sprintf("%q", sig) + `, "doc": "d", "uses": [], "component": ` + fmt.Sprintf("%q", n.Component) + `}],
 "components": [{"id": ` + fmt.Sprintf("%q", n.Component) + `, "package": "x", "exports": []}]}`
	c, err := contract.Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range c.Functions {
		if fn.ID == id {
			return c, fn
		}
	}
	t.Fatal("function not found")
	return nil, contract.Function{}
}

func normalizeReal(t *testing.T, name string) (map[string]any, []string, []string, []string) {
	t.Helper()
	n := loadRealNode(t, name)
	c, f := contractFor(t, n)
	env := newNodeEnv(n.Component, c, &run{brief: &brief.Brief{}})
	d := cloneMap(n.Raw)
	_, kinds, _, err := env.normalizeNode(f, d)
	if err != nil {
		t.Fatal(err)
	}
	return d, kinds, env.notes, env.details
}

func TestRehearsal10NodesNormaliseWithoutARepair(t *testing.T) {
	for _, name := range []string{"target_type", "write_target", "with_description", "strong", "tier_wiring"} {
		t.Run(name, func(t *testing.T) {
			d, kinds, notes, _ := normalizeReal(t, name)
			if len(kinds) != 0 {
				t.Fatalf("defects %v, want the node accepted after its shape is normalised", kinds)
			}
			for _, se := range d["contract"].(map[string]any)["side_effects"].([]any) {
				if _, ok := se.(string); !ok {
					t.Errorf("side effect %v is not a string", se)
				}
			}
			if len(notes) == 0 {
				t.Error("no normalisation was recorded")
			}
		})
	}
	d, _, _, _ := normalizeReal(t, "with_description")
	if got := fmt.Sprint(d["contract"].(map[string]any)["side_effects"]); got != "[call fn-send-notification: Sends notifications to owners/admins as part of escalation.]" {
		t.Errorf("side effects = %s", got)
	}
	d, _, notes, _ := normalizeReal(t, "tier_wiring")
	if d["model_tier"] != "standard" || !strings.Contains(strings.Join(notes, " "), "model_tier") {
		t.Errorf("tier %v notes %v", d["model_tier"], notes)
	}
}

func TestNodeShapeNormalisation(t *testing.T) {
	base := func() map[string]any { return cloneMap(loadRealNode(t, "target_type").Raw) }
	cases := []struct {
		name  string
		edit  func(d map[string]any)
		check func(t *testing.T, d map[string]any)
	}{
		{"string for an array", func(d map[string]any) { d["contract"].(map[string]any)["side_effects"] = "calls fn-is-company-member" },
			func(t *testing.T, d map[string]any) {
				if got := fmt.Sprint(d["contract"].(map[string]any)["side_effects"]); got != "[calls fn-is-company-member]" {
					t.Errorf("side_effects = %s", got)
				}
			}},
		{"null for arrays", func(d map[string]any) {
			ct := d["contract"].(map[string]any)
			ct["side_effects"] = nil
			d["depends_on"] = nil
		}, func(t *testing.T, d map[string]any) {
			ct := d["contract"].(map[string]any)
			if _, ok := ct["side_effects"].([]any); !ok {
				t.Errorf("side_effects = %v", ct["side_effects"])
			}
			if d["depends_on"] == nil {
				t.Errorf("depends_on = %v", d["depends_on"])
			}
		}},
		{"enum case and spaces", func(d map[string]any) { d["model_tier"] = "  Strong " },
			func(t *testing.T, d map[string]any) {
				if d["model_tier"] != "strong" {
					t.Errorf("model_tier = %v", d["model_tier"])
				}
			}},
		{"extra properties", func(d map[string]any) {
			d["notes"] = "x"
			d["contract"].(map[string]any)["complexity"] = "low"
		}, func(t *testing.T, d map[string]any) {
			if _, ok := d["notes"]; ok {
				t.Error("an extra top-level property stayed")
			}
			if _, ok := d["contract"].(map[string]any)["complexity"]; ok {
				t.Error("an extra contract property stayed")
			}
		}},
		{"title from the signature", func(d map[string]any) { delete(d, "title") },
			func(t *testing.T, d map[string]any) {
				if d["title"] != "Validate task assignee" {
					t.Errorf("title = %v", d["title"])
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := loadRealNode(t, "target_type")
			n.Raw = base()
			c.edit(n.Raw)
			cc, f := contractFor(t, n)
			env := newNodeEnv(n.Component, cc, &run{brief: &brief.Brief{}})
			_, kinds, msgs, err := env.normalizeNode(f, n.Raw)
			if err != nil || len(kinds) != 0 {
				t.Fatalf("kinds %v msgs %v err %v", kinds, msgs, err)
			}
			c.check(t, n.Raw)
		})
	}
}

// What cannot be normalised is reported by field and keyword, in a fixed
// vocabulary, never by the reply's own words.
func TestSchemaDefectsNameTheFieldAndTheKeyword(t *testing.T) {
	n := loadRealNode(t, "target_type")
	n.Raw["contract"].(map[string]any)["side_effects"] = []any{5}
	n.Raw["contract"].(map[string]any)["inputs"].([]any)[0].(map[string]any)["constraints"] = "ctx CANARY-text"
	c, f := contractFor(t, n)
	env := newNodeEnv(n.Component, c, &run{brief: &brief.Brief{}})
	_, kinds, _, err := env.normalizeNode(f, cloneMap(n.Raw))
	if err != nil || len(kinds) == 0 {
		t.Fatalf("kinds %v err %v", kinds, err)
	}
	got := strings.Join(env.details, "|")
	for _, want := range []string{"field:contract.side_effects[] keyword:type", "field:contract.inputs[].constraints keyword:type"} {
		if !strings.Contains(got, want) {
			t.Errorf("details %q lack %q", got, want)
		}
	}
	if strings.Contains(got, "CANARY") {
		t.Errorf("details quote the reply: %q", got)
	}
}

// A pending node saved by an older run is tried again with the current checks
// before any model call: nothing is asked for what normalisation now fixes.
func TestPendingNodesAreSettledAgainOnResume(t *testing.T) {
	n := loadRealNode(t, "target_type")
	c, f := contractFor(t, n)
	r := &run{id: testRunID, dir: t.TempDir(), brief: &brief.Brief{}}
	if err := os.MkdirAll(filepath.Join(r.dir, "_state"), 0o755); err != nil {
		t.Fatal(err)
	}
	dec := &decomposed{Components: map[string][]map[string]any{}, Pending: []pendingDraft{
		{Component: n.Component, ID: f.ID, Raw: n.Raw, Defects: []string{defSchema}, Tries: maxDecomposeRepairs}}}
	if err := New(Deps{}).repairDrafts(t.Context(), r, c, dec, map[string]string{}); err != nil {
		t.Fatalf("a node that normalisation fixes needs no model: %v", err)
	}
	if len(dec.Pending) != 0 || len(dec.Components[n.Component]) != 1 {
		t.Errorf("pending %d, drafts %d", len(dec.Pending), len(dec.Components[n.Component]))
	}
}

// The failure names the field and the keyword of each node (at most 10).
func TestDecomposeFailureNamesFieldAndKeyword(t *testing.T) {
	n := loadRealNode(t, "target_type")
	n.Raw["contract"].(map[string]any)["side_effects"] = []any{5}
	c, f := contractFor(t, n)
	r := &run{id: testRunID, dir: t.TempDir(), brief: &brief.Brief{}}
	if err := os.MkdirAll(filepath.Join(r.dir, "_state"), 0o755); err != nil {
		t.Fatal(err)
	}
	dec := &decomposed{Components: map[string][]map[string]any{}, Pending: []pendingDraft{
		{Component: n.Component, ID: f.ID, Raw: n.Raw, Defects: []string{defSchema}, Tries: maxDecomposeRepairs}}}
	err := New(Deps{}).repairDrafts(t.Context(), r, c, dec, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), `"fn-validate-task-assignee"`) || !strings.Contains(err.Error(), "field:contract.side_effects[] keyword:type") ||
		strings.Contains(err.Error(), "(schema)") {
		t.Fatalf("err = %v", err)
	}
}
