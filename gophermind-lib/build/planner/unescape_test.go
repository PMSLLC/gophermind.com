package planner

import (
	"strings"
	"testing"
)

func TestUnescapeQuotedSource(t *testing.T) {
	broken := "package cli\n\nvar cases = []struct{ a, b string }{\n\t{\\\"happy\\\", \\\"42\\\"},\n}\n"
	got := unescapeQuotedSource(broken)
	if strings.Contains(got, `\"`) || !strings.Contains(got, `{"happy", "42"}`) {
		t.Fatalf("not repaired:\n%s", got)
	}
	// Valid Go with a real escaped quote is left alone.
	ok := "package cli\n\nvar s = \"a \\\"b\\\"\"\n"
	if unescapeQuotedSource(ok) != ok {
		t.Fatal("valid source was changed")
	}
	// A repair that does not parse either is not kept.
	bad := "package cli\n\nfunc {\\\"x\\\"\n"
	if unescapeQuotedSource(bad) != bad {
		t.Fatal("an unparseable repair was kept")
	}
}

func TestPlaceholderTestFileRefused(t *testing.T) {
	err := checkNotPlaceholder("<full Go source of the test file>")
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("err = %v", err)
	}
	if checkNotPlaceholder("package cli\n") != nil {
		t.Fatal("real source refused")
	}
}

func TestQuoteBareImports(t *testing.T) {
	broken := "package cli\n\nimport (\n\t\"testing\"\n\n\texample.com/slugify/internal/slug\n\tstr \"strings\"\n)\n\nvar _ = slug.Make\nvar _ = str.ToUpper\nvar _ testing.T\n"
	got := quoteBareImports(broken)
	if !strings.Contains(got, "\t\"example.com/slugify/internal/slug\"\n") || !parsesAsGo(got) {
		t.Fatalf("not repaired:\n%s", got)
	}
	single := "package cli\n\nimport example.com/x/y\n\nvar _ = y.Z\n"
	if got := quoteBareImports(single); !strings.Contains(got, "import \"example.com/x/y\"") {
		t.Errorf("single import not repaired:\n%s", got)
	}
	ok := "package cli\n\nimport \"testing\"\n\nvar _ testing.T\n"
	if quoteBareImports(ok) != ok {
		t.Error("valid source was changed")
	}
}

// A model writes the two characters \n (and \t) where a newline belongs, on
// some rows and not others, including rows that hold a string literal (run
// linkcheck-v1-20261104-r002, nodes fn-calculate-fixed-count and
// fn-classify-targets). Inside a string literal \n is the model's own escape
// and stays. The repair is unconditional: a backslash outside a literal is
// never valid Go, and keeping it only if the file parses let a second defect
// (a bare import path) throw the repair away.
func TestUnescapeBareEscapes(t *testing.T) {
	broken := "package cli\n\nimport (\n\t\"testing\"\n\n\texample.com/linkcheck/internal/types\n)\n\n" +
		"func TestX(t *testing.T) {\n\tcases := []struct {\n\t\tname string\n\t\twant int\n\t}{\n" +
		"\t\t{\n\t\t\tname: \"a\\nb\",\n\t\t},\\n\t\t{\n\t\t\tname: \"c\",\\n\t\t\twant: 1,\n\t\t},\n\t}\n\t_ = cases\n}\n"
	got := unescapeBareEscapes(broken)
	if !strings.Contains(got, "\"a\\nb\"") {
		t.Fatalf("the escape inside a string literal was changed:\n%s", got)
	}
	if strings.Contains(got, "},\\n") || strings.Contains(got, "\"c\",\\n") {
		t.Fatalf("bare escapes kept:\n%s", got)
	}
	if !parsesAsGo(quoteBareImports(got)) {
		t.Fatalf("the chain does not parse:\n%s", quoteBareImports(got))
	}
	// Valid source, with a raw string that holds a backslash, is left alone.
	ok := "package cli\n\nvar s = `a\\nb`\nvar r = '\\n'\n// a comment with \\n in it\n"
	if unescapeBareEscapes(ok) != ok {
		t.Fatal("valid source was changed")
	}
}
