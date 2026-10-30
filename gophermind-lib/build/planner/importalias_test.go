package planner

import (
	"strings"
	"testing"
)

// Run gm-2026-10-30-051: the package was imported as example but called as
// roman.IsCanonical, on every try. An alias that is never used, where the
// package's own name is, is dropped; any other alias stays.
func TestDropUnusedImportAlias(t *testing.T) {
	src := "package roman_test\n\nimport (\n\t\"testing\"\n\texample \"example.com/roman/internal/roman\"\n)\n\nfunc TestX(t *testing.T) {\n\t_ = roman.IsCanonical(1, \"I\")\n}\n"
	got := dropUnusedImportAlias(src)
	if !strings.Contains(got, "\t\"example.com/roman/internal/roman\"\n") || strings.Contains(got, "example \"") {
		t.Errorf("alias not dropped:\n%s", got)
	}
	used := strings.Replace(src, "roman.IsCanonical", "example.IsCanonical", 1)
	if dropUnusedImportAlias(used) != used {
		t.Error("a used alias was dropped")
	}
	neither := strings.Replace(src, "roman.IsCanonical(1, \"I\")", "1", 1)
	if dropUnusedImportAlias(neither) != neither {
		t.Error("an alias was dropped though the package name is not used either")
	}
}
