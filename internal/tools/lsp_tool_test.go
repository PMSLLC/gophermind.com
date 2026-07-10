package tools

import "testing"

func TestLSPDefinitionRejectsTraversal(t *testing.T) {
	// A configured LSP command so we reach the path check (echo won't be invoked).
	tool := LSPDefinition(t.TempDir(), []string{"true"})
	if _, err := run(t, tool, `{"file":"../../etc/passwd","line":1,"column":1}`); err == nil {
		t.Error("file path escaping the root must be rejected")
	}
}

func TestLSPDefinitionNoServer(t *testing.T) {
	if _, err := run(t, LSPDefinition(t.TempDir(), nil), `{"file":"x.go","line":1,"column":1}`); err == nil {
		t.Error("no configured LSP server should error")
	}
}
