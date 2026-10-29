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
	for name, src := range map[string]string{"literal newline": literalNewline, "comma closed": commaClosed, "unclosed": unclosed, "inner quotes": innerQuotes, "stray quote": strayQuote, "extra brace": extraBrace} {
		got := nudgeSyntax(src)
		if !parsesAsGo(got) {
			t.Errorf("%s: not repaired:\n%s", name, got)
		}
	}
	if got := nudgeSyntax(innerQuotes); !strings.Contains(got, `"slugify: invalid --max \"abc\""`) {
		t.Errorf("inner quotes not escaped:\n%s", got)
	}
	hopeless := "package cli\n\nfunc {{{ ((( \n"
	if nudgeSyntax(hopeless) != hopeless {
		t.Error("an unrepairable file was changed")
	}
}
