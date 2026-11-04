package planner

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func syntaxList(t *testing.T, src string) string {
	t.Helper()
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution|parser.AllErrors)
	if err == nil {
		t.Fatal("source parsed")
	}
	return syntaxErrs(err, src)
}

func TestSyntaxErrsQuotesTheOffendingLine(t *testing.T) {
	got := syntaxList(t, "package p\n\nfunc f() {\n\tx := (1\n}\n")
	want := "syntax error at line 4, column 9: expected ')', found newline: x := (1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSyntaxErrsListsEveryErrorOnItsOwnLine(t *testing.T) {
	got := syntaxList(t, "package p\n\nvar a = ]\n\nvar b = 1\n\nvar c = ]\n")
	for _, want := range []string{"2 syntax errors", "line 3, column", "var a = ]", "line 7, column", "var c = ]"} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "var b") {
		t.Errorf("got %q, quotes a line that has no error", got)
	}
}

func TestSyntaxErrsCapsTheList(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n")
	for i := 0; i < 14; i++ {
		fmt.Fprintf(&b, "\nvar v%d = ]\n", i)
	}
	got := syntaxList(t, b.String())
	if strings.Count(got, "line ") > maxSyntaxErrors || !strings.Contains(got, "more") {
		t.Errorf("got %q, want at most %d entries and a count of the rest", got, maxSyntaxErrors)
	}
}

func TestSyntaxErrsCapsTheQuotedLine(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := syntaxList(t, "package p\n\nvar x = ] // "+long+"\n")
	if strings.Contains(got, long) {
		t.Errorf("got %q, want the line cut to %d characters", got, maxQuotedLine)
	}
}

// The bad token need not be last on its line for the next declaration's
// "expected ';'" cascade to be dropped.
func TestSyntaxErrsDropsTheNextDeclarationCascade(t *testing.T) {
	got := syntaxList(t, "package p\nvar a = ] + 1\nvar b = 1\nvar c = ]\n")
	for _, want := range []string{"2 syntax errors", "line 2, column 9", "line 4, column 9"} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "var b") || strings.Contains(got, "EOF") {
		t.Errorf("got %q, reports a cascade", got)
	}
}

func TestSyntaxErrsReportsTheRealErrorOfALine(t *testing.T) {
	got := syntaxList(t, "package p\nvar a, b = ], ]\nvar c = ]\n")
	for _, want := range []string{"2 syntax errors", "line 2, column 12", "line 3, column 9: var c = ]"} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "line 3, column 1:") {
		t.Errorf("got %q, reports the cascade on line 3", got)
	}
}

func TestSyntaxErrsDropsTheEOFEcho(t *testing.T) {
	got := syntaxList(t, "package p\nfunc f() {\n\tx := (1\n}\n")
	if strings.Contains(got, "syntax errors") || strings.Contains(got, "EOF") {
		t.Errorf("got %q, want one entry and no EOF echo", got)
	}
}

func TestSyntaxErrsKeepsAFirstErrorThatExpectsASemicolon(t *testing.T) {
	got := syntaxList(t, "package p\nvar x = 5 6\n")
	want := "syntax error at line 2, column 11: var x = 5 6"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A missing } on a table row sends the parser through the rest of the
// function; every later error there quotes a line that is fine.
func TestSyntaxErrsReportsOneErrorPerDeclaration(t *testing.T) {
	src := "package p\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {\n" +
		"\tcases := []struct {\n\t\tin   string\n\t\twant int\n\t}{\n" +
		"\t\t{\"a\", 1},\n" +
		"\t\t{\"b\", 2,\n" +
		"\t\t{\"c\", 3},\n" +
		"\t}\n" +
		"\tfor _, c := range cases {\n" +
		"\t\tif got := len(c.in); got != c.want {\n" +
		"\t\t\tt.Errorf(\"len(%q) = %d, want %d\", c.in, got, c.want)\n" +
		"\t\t}\n\t}\n}\n"
	got := syntaxList(t, src)
	if !strings.HasPrefix(got, "syntax error at ") {
		t.Errorf("got %q, want exactly one entry", got)
	}
	if strings.Contains(got, "for _, c") || strings.Contains(got, "t.Errorf") {
		t.Errorf("got %q, quotes a correct line", got)
	}
}

// A raw string fixture whose line starts with "func " is not a declaration,
// so the cascade past it is still dropped.
func TestSyntaxErrsIgnoresADeclarationKeywordInARawString(t *testing.T) {
	src := "package p\n\nfunc f() {\n\tx := []int{1, 2,\n\tsrc := `\nfunc g() {}\n`\n\t_ = src\n\tfor range x {\n\t}\n}\n"
	got := syntaxList(t, src)
	if !strings.HasPrefix(got, "syntax error at ") || strings.Contains(got, "for range") {
		t.Errorf("got %q, want one entry that does not quote the for line", got)
	}
}
