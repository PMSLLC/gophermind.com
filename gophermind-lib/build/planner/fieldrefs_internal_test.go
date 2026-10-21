package planner

import (
	"encoding/json"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/contract"
)

// The shape of the csvstat run 12 failure: the brief declares ColumnSummary
// without a Sum field and fn-finish-stats is described as using one.
const fieldRefContract = `{"spec_version": "2.0", "brief_id": "gm-2026-09-29-900", "revision": 0, "module": "csvstat",
 "conventions": {"layout": ["internal/stats"], "naming": ["verbs"], "errors": "return error"},
 "types": [
  {"id": "t-summary", "package": "stats", "file": "internal/stats/summary.go", "decl": "type ColumnSummary struct {\n\tName string\n\tCount int\n\tMean float64\n}"},
  {"id": "t-request", "package": "stats", "file": "internal/stats/request.go", "decl": "type Request struct {\n\tID int\n}"},
  {"id": "t-wrapped", "package": "stats", "file": "internal/stats/wrapped.go", "decl": "type Wrapped struct {\n\tColumnSummary\n\tNote string\n}"}],
 "functions": [
  {"id": "fn-finish-stats", "package": "stats", "file": "internal/stats/finish.go", "signature": "func finishStats(s *ColumnSummary)", "doc": "d", "uses": ["t-summary"], "component": "stats"},
  {"id": "fn-label", "package": "stats", "file": "internal/stats/label.go", "signature": "func (s *ColumnSummary) Label()", "doc": "d", "uses": ["t-summary"], "component": "stats"},
  {"id": "fn-unwrap", "package": "stats", "file": "internal/stats/unwrap.go", "signature": "func unwrap(w Wrapped)", "doc": "d", "uses": ["t-wrapped"], "component": "stats"},
  {"id": "fn-rows", "package": "stats", "file": "internal/stats/rows.go", "signature": "func sumRows(rows []ColumnSummary)", "doc": "d", "uses": ["t-summary"], "component": "stats"},
  {"id": "fn-http", "package": "stats", "file": "internal/stats/http.go", "signature": "func serve(r *http.Request)", "doc": "d", "uses": ["t-request"], "component": "stats"},
  {"id": "fn-with-e", "package": "stats", "file": "internal/stats/e.go", "signature": "func withE(e *ColumnSummary)", "doc": "d", "uses": ["t-summary"], "component": "stats"}],
 "components": [{"id": "stats", "package": "stats", "exports": []}]}`

func fieldRefNode(id, desc, sideEffect, inputName, inputType string) string {
	return `{"id": "` + id + `", "title": "T", "description": "` + desc + `", "node_class": "pure", "depends_on": [],
 "contract": {"inputs": [{"name": "` + inputName + `", "type": "` + inputType + `"}], "outputs": [],
  "errors": [], "side_effects": ["` + sideEffect + `"]}}`
}

func fieldRefSplit(t *testing.T, fn int, node string) draftResult {
	t.Helper()
	c, err := contract.Load([]byte(fieldRefContract))
	if err != nil {
		t.Fatal(err)
	}
	r := &run{id: "gm-2026-09-29-900", brief: &brief.Brief{}}
	res, err := splitDrafts("["+node+"]", "stats", c.Functions[fn:fn+1], c, r)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestUndeclaredFieldInSideEffectsIsADefect(t *testing.T) {
	node := fieldRefNode("fn-finish-stats", "Finishes the summary.", "Computes and sets s.Mean from accumulated s.Sum and s.Count.", "s", "*ColumnSummary")
	res := fieldRefSplit(t, 0, node)
	if len(res.good) != 0 || len(res.bad) != 1 || strings.Join(res.bad[0].Defects, ",") != defUndeclaredField {
		t.Fatalf("res = %+v", res)
	}
	d := strings.Join(res.bad[0].Details, "\n")
	for _, want := range []string{"parameter s", "Sum", "ColumnSummary", "Name", "Count", "Mean"} {
		if !strings.Contains(d, want) {
			t.Errorf("details lack %q: %q", want, d)
		}
	}
	if strings.Contains(d, "s.Count") && strings.Contains(d, "no field Count") {
		t.Errorf("a declared field was reported: %q", d)
	}
}

func TestUndeclaredFieldInDescriptionAndErrorsIsADefect(t *testing.T) {
	node := fieldRefNode("fn-finish-stats", "Divides s.Total by the count.", "Nothing.", "s", "*ColumnSummary")
	if res := fieldRefSplit(t, 0, node); len(res.bad) != 1 || res.bad[0].Defects[0] != defUndeclaredField {
		t.Fatalf("description: %+v", res)
	}
	node = strings.Replace(fieldRefNode("fn-finish-stats", "Finishes.", "Nothing.", "s", "*ColumnSummary"),
		`"errors": []`, `"errors": [{"when": "s.Sum is negative", "returns": "an error"}]`, 1)
	if res := fieldRefSplit(t, 0, node); len(res.bad) != 1 || res.bad[0].Defects[0] != defUndeclaredField {
		t.Fatalf("errors: %+v", res)
	}
}

func TestDeclaredFieldsMethodsAndOtherNamesAreFine(t *testing.T) {
	for name, node := range map[string]string{
		"declared fields":   fieldRefNode("fn-finish-stats", "Sets s.Mean.", "Reads s.Count and s.Name.", "s", "*ColumnSummary"),
		"a method":          fieldRefNode("fn-finish-stats", "Uses s.Label for output.", "None.", "s", "*ColumnSummary"),
		"not a struct name": fieldRefNode("fn-finish-stats", "Reads x.Foo.", "None.", "s", "*ColumnSummary"),
	} {
		if res := fieldRefSplit(t, 0, node); len(res.good) != 1 {
			t.Errorf("%s: res = %+v", name, res)
		}
	}
}

func TestEgAndIeAreNotSelectors(t *testing.T) {
	node := fieldRefNode("fn-with-e", "Pure (e.g. no io, i.e. nothing else).", "None.", "e", "*ColumnSummary")
	if res := fieldRefSplit(t, 5, node); len(res.good) != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestEmbeddedStructsAndSlicesAreHandled(t *testing.T) {
	// An embedded struct promotes fields this check cannot list: skipped.
	node := fieldRefNode("fn-unwrap", "Reads w.Anything.", "None.", "w", "Wrapped")
	if res := fieldRefSplit(t, 2, node); len(res.good) != 1 {
		t.Errorf("embedded: %+v", res)
	}
	// A slice of a struct: the name resolves after stripping [].
	node = fieldRefNode("fn-rows", "Sums rows.Total.", "None.", "rows", "[]ColumnSummary")
	if res := fieldRefSplit(t, 3, node); len(res.bad) != 1 {
		t.Errorf("slice: %+v", res)
	}
}

func TestReceiverNameIsChecked(t *testing.T) {
	node := fieldRefNode("fn-label", "Labels s.Title.", "None.", "s", "*ColumnSummary")
	if res := fieldRefSplit(t, 1, node); len(res.bad) != 1 || res.bad[0].Defects[0] != defUndeclaredField {
		t.Errorf("receiver: %+v", res)
	}
}

func TestRepairPromptNamesTheDefectAndNeverQuotesTheReply(t *testing.T) {
	node := fieldRefNode("fn-finish-stats", "CANARYWORDS Finishes.", "Sets s.Mean from s.Sum.", "s", "*ColumnSummary")
	res := fieldRefSplit(t, 0, node)
	c, _ := contract.Load([]byte(fieldRefContract))
	pd := res.bad[0]
	pd.Raw = map[string]any{"id": "fn-finish-stats"}
	text := fixNodeText(pd, c.Functions[0], 1)
	for _, want := range []string{"fn-finish-stats", defUndeclaredField, "Sum", "ColumnSummary", "Name, Count, Mean", "description", "contract.side_effects"} {
		if !strings.Contains(text, want) {
			t.Errorf("repair text lacks %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(strings.Join(pd.Details, "\n"), "\n") {
		if strings.Contains(line, "CANARYWORDS") || strings.Contains(line, "from s.Sum") {
			t.Errorf("a detail quotes the reply: %q", line)
		}
	}
}

// A parameter whose type has a package qualifier is another package's type,
// even when the contract declares a struct of the same bare name.
func TestQualifiedParameterTypeIsNotResolvedToASameNamedStruct(t *testing.T) {
	node := fieldRefNode("fn-http", "Reads r.URL and r.Method.", "None.", "r", "*http.Request")
	if res := fieldRefSplit(t, 4, node); len(res.good) != 1 {
		t.Errorf("res = %+v", res)
	}
}

// Words that look like file names are not field references, but a wrong-case
// spelling of a declared field and an exported unknown field are.
func TestFileLikeLowercaseWordsAreNotFieldReferences(t *testing.T) {
	for _, desc := range []string{"Reads s.csv and writes s.json.", "Opens s.txt, then s.yaml."} {
		node := fieldRefNode("fn-finish-stats", desc, "None.", "s", "*ColumnSummary")
		if res := fieldRefSplit(t, 0, node); len(res.good) != 1 {
			t.Errorf("%q: res = %+v", desc, res)
		}
	}
	node := fieldRefNode("fn-finish-stats", "Sets s.mean.", "None.", "s", "*ColumnSummary")
	if res := fieldRefSplit(t, 0, node); len(res.bad) != 1 {
		t.Errorf("wrong-case declared field: %+v", res)
	}
}

// Details of the undeclared field survive a schema failure of the same node.
func TestUndeclaredFieldDetailsAreKeptBesideSchemaDetails(t *testing.T) {
	c, _ := contract.Load([]byte(fieldRefContract))
	r := &run{id: "gm-2026-09-29-900", brief: &brief.Brief{}}
	env := newNodeEnv("stats", c, r)
	d := map[string]any{}
	if err := json.Unmarshal([]byte(strings.Replace(fieldRefNode("fn-finish-stats", "Uses s.Sum.", "None.", "s", "*ColumnSummary"), `"model_tier"`, `"x"`, 1)), &d); err != nil {
		t.Fatal(err)
	}
	_, kinds, _, err := env.normalizeNode(c.Functions[0], d)
	if err != nil || !contains(kinds, defUndeclaredField) || !strings.Contains(strings.Join(env.details, "\n"), "Sum") {
		t.Fatalf("kinds %v details %v err %v", kinds, env.details, err)
	}
}
