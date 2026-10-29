package planner

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestMainEntryTest(t *testing.T) {
	if _, _, ok := mainEntryTest("func Run() int", "internal/cli/run.go", "TestFnRun"); ok {
		t.Fatal("a function that is not main got the fixed test")
	}
	tests, src, ok := mainEntryTest("func  main()", "cmd/slugify/main.go", "TestFnMain")
	if !ok {
		t.Fatal("func main() did not get the fixed test")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", src, 0); err != nil {
		t.Fatalf("source does not parse: %v\n%s", err, src)
	}
	if !strings.Contains(src, "package main") || !strings.Contains(src, "func TestFnMain(t *testing.T)") || !strings.Contains(src, "_ = main\n") {
		t.Errorf("unexpected source:\n%s", src)
	}
	if len(tests) != 1 || tests[0]["command"] != "go test ./cmd/slugify -run ^TestFnMain$" {
		t.Errorf("tests = %v", tests)
	}
}
