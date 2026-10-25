package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func TestTypeDeclShape(t *testing.T) {
	ok := []string{
		"type T int",
		"type T struct{ R string }\n\nfunc (t *T) Error() string { return t.R }",
		"type A int\ntype B string",
	}
	for _, d := range ok {
		if err := TypeDeclShape(d); err != nil {
			t.Errorf("TypeDeclShape(%q) = %v, want nil", d, err)
		}
	}
	bad := map[string]string{
		"const block":    "type renderFormat int\n\nconst (\n\trenderFormatText renderFormat = iota\n)",
		"var":            "type T int\nvar x = 1",
		"plain function": "type T int\nfunc F() {}",
		"import":         "import \"os\"\ntype T int",
		"no type":        "// nothing",
		"bodyless":       "type T int\nfunc (T) M()",
	}
	for name, d := range bad {
		err := TypeDeclShape(d)
		if err == nil {
			t.Errorf("%s: accepted %q", name, d)
			continue
		}
		if strings.Contains(err.Error(), "renderFormat") {
			t.Errorf("%s: error quotes the declaration: %v", name, err)
		}
	}
}

func TestLocalChecksRefusesConstInTypeDecl(t *testing.T) {
	c := &contract.Contracts{BriefID: "b", Types: []contract.Type{{ID: "render-format", Package: "internal/render", File: "internal/render/render.go",
		Decl: "type renderFormat int\n\nconst (\n\trenderFormatText renderFormat = iota\n)"}}}
	err := localChecks(c, c.BriefID)
	if err == nil || !strings.Contains(err.Error(), "type declarations and methods only") {
		t.Fatalf("localChecks = %v, want the type declaration rule", err)
	}
}
