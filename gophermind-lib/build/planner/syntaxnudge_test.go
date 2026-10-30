package planner

import (
	"strings"
	"testing"
)

func TestNudgeSyntax(t *testing.T) {
	unclosed := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\ttests := []struct {\n\t\tname string\n\t}{\n\t\t{name: \"a\"},\n\t\t{name: \"b\"},\n\n\tfor _, tt := range tests {\n\t\t_ = tt\n\t}\n}\n"
	innerQuotes := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tcases := []struct{ want string }{\n\t\t{want: \"slugify: invalid --max \"abc\"\"},\n\t}\n\t_ = cases\n}\n"
	strayQuote := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\t_ = t\n}\"\n"
	extraBrace := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tcases := []struct{ a int }{\n\t\t{\n\t\t\ta: 1,\n\t\t},\n\t\t},\n\t\t{\n\t\t\ta: 2,\n\t\t},\n\t}\n\t_ = cases\n}\n"
	commaClosed := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\ttests := []struct {\n\t\tname string\n\t}{\n\t\t{\n\t\t\tname: \"a\",\n\t\t},\n\t\t},\n\n\tfor _, tt := range tests {\n\t\t_ = tt\n\t}\n}\n"
	literalNewline := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tcases := []struct{ a int }{\n\t\t{\n\t\t\ta: 1,\n\t\t},\\n\t\t{\n\t\t\ta: 2,\n\t\t},\n\t}\n\t_ = cases\n}\n"
	unclosedRun := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tfor _, n := range []string{\"a\"} {\n\t\tt.Run(n, func(t *testing.T) {\n\t\t\t_ = t\n\t\t}\n\t}\n}\n"
	// Run gm-2026-10-30-039: the comma of a table row on a line of its own.
	loneComma := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tcases := []struct{ a int }{\n\t\t{\n\t\t\ta: 1,\n\t\t\t}\n\t\t,\n\t\t{\n\t\t\ta: 2,\n\t\t},\n\t}\n\t_ = cases\n}\n"
	argQuotes := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tgot := \"x\"\n\tt.Errorf(\"got %q, want \"data\"\", got)\n}\n"
	lastRow := "package cli\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tcases := []struct{ a int }{\n\t\t{\n\t\t\ta: 1,\n\t\t\t}\n\t\t,\n\t\t{\n\t\t\ta: 2,\n\t\t\t}\n\t\t},\n\n\t_ = cases\n}\n"
	// Run gm-2026-10-30-045: a row's setup function returning a mock literal.
	openSetup := "package cli\n\nimport \"testing\"\n\ntype M struct{ F func() int }\n\nfunc TestX(t *testing.T) {\n\tcases := []struct {\n\t\tsetup func() *M\n\t\twant  int\n\t}{\n\t\t{\n\t\t\tsetup: func() *M {\n\t\t\t\treturn &M{\n\t\t\t\t\tF: func() int {\n\t\t\t\t\t\treturn 1\n\t\t\t\t\t},\n\t\t\t\t},\n\t\t\twant: 1,\n\t\t},\n\t}\n\t_ = cases\n}\n"
	openSetupBrace := strings.Replace(openSetup, "\t\t\t\t},\n\t\t\twant: 1,", "\t\t\t\t},\n\t\t\t}\n\t\t\twant: 1,", 1)
	for name, src := range map[string]string{"open setup": openSetup, "open setup with brace": openSetupBrace, "quotes before arguments": argQuotes, "last row": lastRow, "lone comma": loneComma, "unclosed run": unclosedRun, "literal newline": literalNewline, "comma closed": commaClosed, "unclosed": unclosed, "inner quotes": innerQuotes, "stray quote": strayQuote, "extra brace": extraBrace} {
		got := nudgeSyntax(src)
		if !parsesAsGo(got) {
			t.Errorf("%s: not repaired:\n%s", name, got)
		}
	}
	if got := nudgeSyntax(innerQuotes); !strings.Contains(got, `"slugify: invalid --max \"abc\""`) {
		t.Errorf("inner quotes not escaped:\n%s", got)
	}
	if got := nudgeSyntax(loneComma); !strings.Contains(got, "\t\t\t},\n\t\t{") {
		t.Errorf("lone comma not joined to its brace:\n%s", got)
	}
	hopeless := "package cli\n\nfunc {{{ ((( \n"
	if nudgeSyntax(hopeless) != hopeless {
		t.Error("an unrepairable file was changed")
	}
}
