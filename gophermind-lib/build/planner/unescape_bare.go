package planner

import (
	"go/scanner"
	"go/token"
	"strings"
)

// unescapeBareEscapes turns the two characters \n or \t that sit outside any
// string, rune or comment into the newline or tab the model meant. A model
// makes this slip on some rows of a table and not others, and a backslash
// outside a literal is never valid Go, so the edit cannot break a file that
// parses and is applied without a parse check: the check-after-parse repairs
// (quoteBareImports, unescapeQuotedSource) threw their edit away whenever this
// slip was also in the file.
func unescapeBareEscapes(src string) string {
	if !strings.Contains(src, `\n`) && !strings.Contains(src, `\t`) {
		return src
	}
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, scanner.ScanComments)
	var b strings.Builder
	last := 0
	fix := func(text string) string {
		return strings.NewReplacer(`\n`, "\n", `\t`, "\t").Replace(text)
	}
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING && tok != token.CHAR && tok != token.COMMENT {
			continue
		}
		start := file.Offset(pos)
		end := start + len(lit)
		if start < last || end > len(src) {
			continue
		}
		b.WriteString(fix(src[last:start]))
		b.WriteString(src[start:end])
		last = end
	}
	b.WriteString(fix(src[last:]))
	return b.String()
}
