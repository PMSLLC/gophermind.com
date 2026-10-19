package planner

import (
	"fmt"
	"strings"
	"testing"
)

const impMod = "csvstat"

func impSet() importSet {
	return importSet{
		all:        []string{"csvstat/cmd/csvstat", "csvstat/internal/render", "csvstat/internal/stats"},
		importable: []string{"csvstat/internal/render", "csvstat/internal/stats"},
	}
}

func impSrc(imp string) string {
	return "package render\n\nimport (\n\t\"testing\"\n\t\"" + imp + "\"\n)\n\nvar _ = 1\n\nfunc TestRenderText(t *testing.T) {}\n"
}

func TestPromptListsImportPathOfEachQualifier(t *testing.T) {
	node := `{"contract":{"signature":"func renderText(summaries []stats.ColumnSummary, maxRows int, w io.Writer) error"},` +
		`"context":{"dependency_signatures":["type ColumnSummary struct{ Hist []stats.Bucket }","func Fmt(b *strings.Builder) string"]}}`
	sib := map[string]string{"stats": "csvstat/internal/stats", "render": "csvstat/internal/render"}
	got, err := renderTestwriterWith(node, impMod, "internal/render/render.go", "TestRenderText", "internal/render/x_test.go", sib)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"`stats` = `csvstat/internal/stats`", "`io` = `io`", "`strings` = `strings`", "the module path `csvstat` plus the package directory"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "`render` =") {
		t.Errorf("a qualifier that is not used is listed")
	}
}

func TestPromptWithoutQualifiersListsNone(t *testing.T) {
	got, err := renderTestwriterWith(`{"contract":{"signature":"func F(a int) int"}}`, impMod, "internal/render/render.go", "TestF", "x_test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "package qualifier used") {
		t.Errorf("empty list rendered:\n%s", got)
	}
}

func TestImportRefusalNamesModuleAndPackagesNotTheReply(t *testing.T) {
	for _, bad := range []string{"gophermind/internal/stats", "csvstat/stats", "csvstat/internal/nothere", "github.com/x/y"} {
		err := checkTestSourceIn(impSrc(bad), "internal/render", "TestRenderText", impMod, false, impSet())
		if err == nil {
			t.Fatalf("%s accepted", bad)
		}
		msg := err.Error()
		if strings.Contains(msg, bad) {
			t.Errorf("refusal quotes the reply import %q: %s", bad, msg)
		}
		want := "the module path is csvstat and the packages of this module are: csvstat/internal/render, csvstat/internal/stats; import only those, spelled exactly"
		if !strings.Contains(msg, want) {
			t.Errorf("%s: refusal %q lacks %q", bad, msg, want)
		}
	}
}

func TestImportOfContractPackageAccepted(t *testing.T) {
	if err := checkTestSourceIn(impSrc("csvstat/internal/stats"), "internal/render", "TestRenderText", impMod, false, impSet()); err != nil {
		t.Fatal(err)
	}
}

func TestImportRefusalCapsTheList(t *testing.T) {
	var all []string
	for i := 0; i < 20; i++ {
		all = append(all, fmt.Sprintf("csvstat/p%02d", i))
	}
	msg := importRefusal(impMod, importSet{all: all, importable: all})
	if strings.Contains(msg, "csvstat/p12") || !strings.Contains(msg, "csvstat/p11") {
		t.Errorf("list not capped at 12: %s", msg)
	}
}
