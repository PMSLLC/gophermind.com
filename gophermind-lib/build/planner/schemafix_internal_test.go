package planner

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func docWith(fns ...string) map[string]any {
	var d map[string]any
	raw := `{"spec_version": "2.0", "brief_id": "` + testRunID + `", "revision": 0, "module": "example.com/x",
	 "conventions": {"layout": ["a"], "naming": ["a"], "errors": "e", "testing": "t"},
	 "components": [{"id": "greeting", "package": "x", "exports": []}], "types": [], "functions": [` + strings.Join(fns, ",") + `]}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		panic(err)
	}
	return d
}

func fnJSON(id string, drop ...string) string {
	m := map[string]any{"id": id, "component": "greeting", "package": "x", "file": "internal/x/a.go", "signature": "func (s *S) Run(a int) error", "doc": "d", "uses": []any{}}
	for _, d := range drop {
		delete(m, d)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestSchemaFailuresNameTheNodeAndTheField(t *testing.T) {
	d := docWith(fnJSON("fn-a"), fnJSON("fn-b", "doc"), fnJSON("fn-c", "signature", "file"))
	_, err := validateContractDoc(d, testRunID)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{`function "fn-b" is missing doc`, `function "fn-c" is missing signature`, `function "fn-c" is missing file`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "/functions/") {
		t.Errorf("a bare pointer is in %v", err)
	}
	issues := schemaIssues(d)
	if len(issues) != 3 || issues[0].id != "fn-b" || issues[0].field != "doc" || !issues[0].repairable() {
		t.Errorf("issues = %+v", issues)
	}
}

func TestSchemaFailuresAreBoundedAndLongIDsNamedByLength(t *testing.T) {
	var fns []string
	for i := 0; i < 15; i++ {
		fns = append(fns, fnJSON(fmt.Sprintf("fn-n%02d", i), "doc"))
	}
	fns = append(fns, fnJSON("fn-"+strings.Repeat("a", 80), "doc"))
	_, err := validateContractDoc(docWith(fns...), testRunID)
	if err == nil || strings.Count(err.Error(), "is missing doc") != maxSchemaPointers || !strings.Contains(err.Error(), "more") {
		t.Fatalf("err = %v", err)
	}
	if issues := schemaIssues(docWith(fnJSON("fn-"+strings.Repeat("a", 80), "doc"))); len(issues) != 1 || issues[0].repairable() {
		t.Errorf("an id too long to name must not be repairable: %+v", issues)
	}
}

func TestDefaultDocUsesTheSignatureName(t *testing.T) {
	d := docWith(fnJSON("fn-a", "doc"), fnJSON("fn-b", "signature", "doc"))
	n, ids := defaultDocs(d)
	if n != 1 || len(ids) != 1 || ids[0] != `"fn-a"` {
		t.Fatalf("defaulted %d %v, want only the function with a signature", n, ids)
	}
	if got := usesDoc(d, "fn-a"); got != "Run implements greeting behaviour described in the brief." {
		t.Errorf("doc = %q", got)
	}
}

func usesDoc(d map[string]any, id string) string {
	for _, f := range objects(d["functions"]) {
		if f["id"] == id {
			s, _ := f["doc"].(string)
			return s
		}
	}
	return ""
}
