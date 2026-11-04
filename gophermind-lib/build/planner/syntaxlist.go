package planner

import (
	"errors"
	"fmt"
	"go/scanner"
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
// error on a line already reported.
func syntaxErrs(err error, src string) string {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		return syntaxErr(err)
	}
	lines := strings.Split(src, "\n")
	seen := map[int]bool{}
	var entries []string
	for _, e := range list {
		n := e.Pos.Line
		text := ""
		if n >= 1 && n <= len(lines) {
			text = lines[n-1]
		}
		if len(entries) > 0 && isCascade(e, text) || seen[n] {
			continue
		}
		seen[n] = true
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
