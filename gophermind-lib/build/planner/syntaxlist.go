package planner

import (
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"strings"
)

// maxSyntaxErrors bounds the entries listed in a test file syntax refusal.
const maxSyntaxErrors = 10

// syntaxErrs describes every parse error in the test writer's file, one entry
// per line, so a model that fixes one error is told about the others too. Each
// entry carries the line and column, the safe part of the parser message (see
// syntaxDetail) and the offending source line cut to maxQuotedLine characters.
// The quoted text is the model's own test file; the brief is never quoted.
// Cascades after the first error are dropped, so the model is not sent to fix
// lines that are fine: an "expected ';'" at the first token of a line (the
// parser recovering from a bad operand trips over the next declaration), a
// "found 'EOF'" (an unclosed brace echoed at the end of the file), and a later
// error on a line already reported. The third rule is per declaration: after
// a missing brace or comma the parser runs through the rest of the enclosing
// function and reports nearly every later line, all of them correct, so a
// later error in the same top-level declaration as the previous kept error is
// dropped. That rule cannot see the cascade onto the next declaration's first
// line, which is why the first two rules stay.
func syntaxErrs(err error, src string) string {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		return syntaxErr(err)
	}
	lines := strings.Split(src, "\n")
	decl := declIndex(src, len(lines))
	declOf := func(n int) int {
		if n >= 1 && n <= len(decl) {
			return decl[n-1]
		}
		return -1
	}
	seen := map[int]bool{}
	lastDecl := -1
	var entries []string
	for _, e := range list {
		n := e.Pos.Line
		text := ""
		if n >= 1 && n <= len(lines) {
			text = lines[n-1]
		}
		if len(entries) > 0 && (isCascade(e, text) || declOf(n) == lastDecl) || seen[n] {
			continue
		}
		seen[n] = true
		lastDecl = declOf(n)
		entry := fmt.Sprintf("line %d, column %d", n, e.Pos.Column)
		if detail := syntaxDetail(e.Msg); detail != "" {
			entry += ": " + detail
		}
		if l := capLine(text, maxQuotedLine); l != "" {
			entry += ": " + l
		}
		entries = append(entries, entry)
	}
	if len(entries) == 1 {
		return "syntax error at " + entries[0]
	}
	msg := fmt.Sprintf("%d syntax errors: ", len(entries))
	if len(entries) > maxSyntaxErrors {
		return msg + strings.Join(entries[:maxSyntaxErrors], "; ") + fmt.Sprintf(" and %d more", len(entries)-maxSyntaxErrors)
	}
	return msg + strings.Join(entries, "; ")
}

// isCascade reports whether e, on source line text, is an echo of an earlier
// error: an "expected ';'" at the line's first non-blank column, or any error
// that found the end of the file.
func isCascade(e *scanner.Error, text string) bool {
	if strings.HasSuffix(e.Msg, "found 'EOF'") {
		return true
	}
	first := len(text) - len(strings.TrimLeft(text, " \t")) + 1
	return strings.HasPrefix(e.Msg, "expected ';'") && e.Pos.Column == first
}

// declIndex numbers the top-level declaration each of the n source lines
// belongs to. A declaration starts on a line whose first column holds the
// keyword func, var, const, type or import. The source is tokenized with
// go/scanner rather than read line by line, so a line inside a raw string
// that happens to begin with "func " is not taken for a declaration; the
// scanner does not parse, so it works on a file the parser refused. Lines
// before the first declaration (the package clause) are declaration 0.
func declIndex(src string, n int) []int {
	starts := make([]bool, n+2)
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, 0)
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.FUNC, token.VAR, token.CONST, token.TYPE, token.IMPORT:
			if p := fset.Position(pos); p.Column == 1 && p.Line <= n {
				starts[p.Line] = true
			}
		}
	}
	idx := make([]int, n)
	d := 0
	for i := 0; i < n; i++ {
		if starts[i+1] {
			d++
		}
		idx[i] = d
	}
	return idx
}
