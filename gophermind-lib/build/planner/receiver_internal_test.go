package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/contract"
)

const receiverPlannerContract = `{"spec_version": "2.0", "brief_id": "gm-2026-09-29-900", "revision": 0, "module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "types": [
  {"id": "col-state", "package": "x", "file": "internal/x/x.go", "decl": "type colState struct {\n\tname string\n\tcount int\n}"},
  {"id": "summary", "package": "x", "file": "internal/x/x.go", "decl": "type Summary struct {\n\tName string\n}"}],
 "functions": [
  {"id": "fn-finalize", "package": "x", "file": "internal/x/fin.go", "signature": "func (s *colState) finalize(name string) Summary", "doc": "d", "uses": ["summary"], "component": "c"}],
 "components": [{"id": "c", "package": "x", "exports": []}]}`

const receiverDraft = `{"id": "fn-finalize", "title": "Finalize", "description": "Finalizes.", "node_class": "pure", "depends_on": [],
 "contract": {"inputs": [{"name": "name", "type": "string"}],
  "outputs": [{"name": "summary", "type": "Summary"}], "errors": [], "side_effects": []}}`

func TestMethodNodeSliceCarriesReceiverDecl(t *testing.T) {
	c, err := contract.Load([]byte(receiverPlannerContract))
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "gm-2026-09-29-900", brief: &brief.Brief{}}
	drafts, _, err := normalizeDrafts("["+receiverDraft+"]", "c", c.Functions, c, r)
	if err != nil {
		t.Fatal(err)
	}
	ctx := drafts[0]["context"].(map[string]any)
	sigs := strings.Join(ctx["dependency_signatures"].([]string), "\n")
	if !strings.Contains(sigs, "type colState struct") || !strings.Contains(sigs, "type Summary struct") {
		t.Fatalf("dependency_signatures lacks the receiver or listed type:\n%s", sigs)
	}
	if deps, _ := drafts[0]["depends_on"].([]string); len(deps) != 0 {
		t.Fatalf("depends_on changed: %v", deps)
	}
	if got := usesWithReceiver(c, contract.Function{Signature: "func Plain() string", Uses: []string{"summary"}}); len(got) != 1 {
		t.Fatalf("a plain function gained uses: %v", got)
	}
	both := usesWithReceiver(c, contract.Function{Signature: c.Functions[0].Signature, File: c.Functions[0].File, Uses: []string{"col-state"}})
	if len(both) != 1 {
		t.Fatalf("receiver already listed was duplicated: %v", both)
	}
}

func TestWithReceiverDeclFillsAResumedNodeView(t *testing.T) {
	c, err := contract.Load([]byte(receiverPlannerContract))
	if err != nil {
		t.Fatal(err)
	}
	node := map[string]any{"id": "fn-finalize",
		"contract": map[string]any{"signature": c.Functions[0].Signature, "file": c.Functions[0].File},
		"context":  map[string]any{"dependency_signatures": []any{"type Summary struct {\n\tName string\n}"}}}
	view := withReceiverDecl(c, testwriterView(node))
	sigs, _ := view["context"].(map[string]any)["dependency_signatures"].([]any)
	if len(sigs) != 2 {
		t.Fatalf("signatures = %v", sigs)
	}
	view = withReceiverDecl(c, view)
	if sigs, _ = view["context"].(map[string]any)["dependency_signatures"].([]any); len(sigs) != 2 {
		t.Fatalf("receiver declaration added twice: %v", sigs)
	}
	if node["context"].(map[string]any)["dependency_signatures"].([]any)[0] == nil || len(node["context"].(map[string]any)["dependency_signatures"].([]any)) != 1 {
		t.Fatal("the node itself was mutated")
	}
}

func TestTestwriterPromptSaysThePackageDeclaresTheReceiver(t *testing.T) {
	node := `{"contract": {"signature": "func (s *columnState) finalize(name string) ColumnSummary"}}`
	got, err := renderTestwriter(node, "csvstat", "internal/stats/column_state_finalize.go", "TestX", "internal/stats/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "`columnState` is already declared by the package") || !strings.Contains(got, "declaration is in dependency_signatures") {
		t.Fatalf("prompt does not say the receiver is declared:\n%s", got)
	}
	plain, _ := renderTestwriter(`{"contract": {"signature": "func Plain() string"}}`, "csvstat", "internal/stats/p.go", "TestX", "internal/stats/x_test.go")
	if strings.Contains(plain, "already declared by the package") {
		t.Fatal("a plain function prompt mentions a receiver")
	}
}
