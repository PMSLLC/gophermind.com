package planner

import (
	"strings"
	"testing"
)

func TestTestwriterPromptStatesTheImportPath(t *testing.T) {
	got, err := renderTestwriter("{}", "github.com/example/csvstat", "internal/stats/stats.go", "TestX", "internal/stats/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "github.com/example/csvstat/internal/stats") {
		t.Fatalf("prompt lacks the package import path:\n%s", got)
	}
	if !strings.Contains(got, "`github.com/example/csvstat`") {
		t.Fatalf("prompt lacks the module path")
	}
}

func TestTestwriterPromptImportPathAtModuleRoot(t *testing.T) {
	got, err := renderTestwriter("{}", "github.com/example/csvstat", "main.go", "TestX", "x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "imported as `github.com/example/csvstat`") {
		t.Fatalf("root package should import as the module alone:\n%s", got)
	}
}

func TestTestwriterPromptWithoutAModuleHasNoBrokenSentence(t *testing.T) {
	got, err := renderTestwriter("{}", "", "internal/stats/stats.go", "TestX", "x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "``") || strings.Contains(got, "module is") {
		t.Fatalf("empty module leaked into the prompt:\n%s", got)
	}
}
