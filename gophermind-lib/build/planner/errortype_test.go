package planner

import (
	"slices"
	"strings"
	"testing"
)

func TestErrorTypesGetErrorMethod(t *testing.T) {
	doc := map[string]any{"types": []any{
		map[string]any{"id": "cli-error", "decl": "// cliError is an error.\ntype cliError struct {\n\texitCode int\n\tmessage  string\n}\n"},
		map[string]any{"id": "has-method", "decl": "type ParseError struct{ msg string }\n\nfunc (e *ParseError) Error() string { return e.msg }\n"},
		map[string]any{"id": "no-string", "decl": "type codeError struct{ code int }\n"},
		map[string]any{"id": "not-error", "decl": "type slugConfig struct{ maxLen int }\n"},
	}}
	got := addErrorMethods(doc)
	if !slices.Equal(got, []string{"cli-error"}) {
		t.Fatalf("changed %v, want [cli-error]", got)
	}
	decl := objects(doc["types"])[0]["decl"].(string)
	if !strings.Contains(decl, "func (e *cliError) Error() string { return e.message }") {
		t.Errorf("decl = %q", decl)
	}
	if err := TypeDeclShape(decl); err != nil {
		t.Errorf("the repaired declaration is refused: %v", err)
	}
	if again := addErrorMethods(doc); len(again) != 0 {
		t.Errorf("not idempotent: %v", again)
	}
}

func TestErrorMethodAsAContractFunctionCounts(t *testing.T) {
	doc := map[string]any{
		"types":     []any{map[string]any{"id": "name-error", "decl": "type NameError struct{ msg string }"}},
		"functions": []any{map[string]any{"id": "fn-name-error-error", "signature": "func (e *NameError) Error() string"}},
	}
	if got := addErrorMethods(doc); len(got) != 0 {
		t.Fatalf("changed %v although the contract declares the method", got)
	}
}
