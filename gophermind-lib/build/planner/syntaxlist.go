package planner

import (
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

// maxSyntaxErrors bounds the entries listed in a test file syntax refusal.
const maxSyntaxErrors = 10

// syntaxErrs describes every parse error in the test writer's file, one entry
// per line, so a model that fixes one error is told about the others too. Each
// entry carries the line and column, the safe part of the parser message (see
// syntaxDetail) and the offending source line cut to maxQuotedLine characters.
// The quoted text is the model's own test file; the brief is never quoted.
// Cascades are dropped: a later error on a line already reported, and an error
// at the first token after the previous error (the parser recovering from a bad
// operand reports "expected ';'" at the next line's first token).
func syntaxErrs(err error, src string) string {
	var list scanner.ErrorList
	if !errors.As(err, &list) || len(list) == 0 {
		return syntaxErr(err)
	}
	lines := strings.Split(src, "\n")
	starts := tokenStarts(src)
	seen := map[int]bool{}
	prev := -1
	var entries []string
	for _, e := range list {
		n := e.Pos.Line
		if e.Msg == "too many errors" {
			continue
		}
		adjacent := prev >= 0 && sort.SearchInts(starts, e.Pos.Offset) == sort.SearchInts(starts, prev+1)
		prev = e.Pos.Offset
		if seen[n] || adjacent {
			continue
		}
		seen[n] = true
		entry := fmt.Sprintf("line %d, column %d", n, e.Pos.Column)
		if detail := syntaxDetail(e.Msg); detail != "" {
			entry += ": " + detail
		}
		if n >= 1 && n <= len(lines) {
			if l := capLine(lines[n-1], maxQuotedLine); l != "" {
				entry += ": " + l
			}
		}
		entries = append(entries, entry)
	}
	switch {
	case len(entries) == 0:
		return syntaxErr(err)
	case len(entries) == 1:
		return "syntax error at " + entries[0]
	}
	msg := fmt.Sprintf("%d syntax errors: ", len(entries))
	if len(entries) > maxSyntaxErrors {
		return msg + strings.Join(entries[:maxSyntaxErrors], "; ") + fmt.Sprintf(" and %d more", len(entries)-maxSyntaxErrors)
	}
	return msg + strings.Join(entries, "; ")
}

// tokenStarts returns the offsets of the tokens in src, in order, leaving out
// the semicolons the scanner inserts at line ends, so two errors with no
// written token between them can be recognised as one.
func tokenStarts(src string) []int {
	var s scanner.Scanner
	s.Init(token.NewFileSet().AddFile("", -1, len(src)), []byte(src), nil, 0)
	var starts []int
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return starts
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		starts = append(starts, int(pos)-1)
	}
}
