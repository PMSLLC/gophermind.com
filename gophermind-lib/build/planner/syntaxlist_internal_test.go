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
