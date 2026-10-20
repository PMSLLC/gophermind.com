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

func TestTestwriterPromptModuleRootIsAMainPackage(t *testing.T) {
	got, err := renderTestwriter("{}", "github.com/example/csvstat", "main.go", "TestX", "x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "no import of the package") || strings.Contains(got, "imported as") {
		t.Fatalf("root package is main and must not be imported:\n%s", got)
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

func TestRenderTestwriterMainPackageSaysPackageMain(t *testing.T) {
	got, err := renderTestwriter("{}", "github.com/example/csvstat", "cmd/csvstat/main.go", "TestX", "cmd/csvstat/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"package main", "no import of the package"} {
		if !strings.Contains(got, want) {
			t.Errorf("main package prompt lacks %q", want)
		}
	}
	if strings.Contains(got, "package `X_test`") {
		t.Error("main package prompt still offers package X_test")
	}
	other, err := renderTestwriter("{}", "github.com/example/csvstat", "internal/stats/stats.go", "TestX", "internal/stats/x_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(other, "package `X_test`") {
		t.Error("non-main prompt lost the X_test guidance")
	}
}
