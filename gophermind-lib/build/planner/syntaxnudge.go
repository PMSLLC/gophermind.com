package planner

import (
	"errors"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"strings"
)

// maxSyntaxNudges bounds the edits nudgeSyntax tries on one file.
const maxSyntaxNudges = 4

// nudgeSyntax repairs small bracket and quote slips a model makes in a test
// file, at the line the parser reports: a table literal left unclosed before
// its loop, one closing brace too many, quotes inside a string that are not
// escaped, a stray quote after the last brace. Each candidate edit is kept
// only when the first syntax error moves later or goes away, and the result is
// kept only if the whole file then parses. Source that parses, or that the
// edits cannot repair, is returned as it was.
func nudgeSyntax(src string) string {
	line, bad := firstSyntaxErrLine(src)
	if !bad {
		return src
	}
	if fixed, ok := nudgeSearch(src, line, maxSyntaxNudges); ok {
		return fixed
	}
	return src
}

// nudgeSearch tries the candidate edits depth first. A branch goes on only
// while the first error moves later, so the search is bounded by the depth and
// the few candidates per line.
func nudgeSearch(src string, line, depth int) (string, bool) {
	if depth == 0 {
		return "", false
	}
	for _, cand := range syntaxCandidates(src, line) {
		nl, bad := firstSyntaxErrLine(cand)
		if !bad {
			return cand, true
		}
		if nl > line {
			if fixed, ok := nudgeSearch(cand, nl, depth-1); ok {
				return fixed, true
			}
		}
	}
	return "", false
}

// firstSyntaxErrLine is the 1-based line of the first syntax error, and false
// when the source parses.
func firstSyntaxErrLine(src string) (int, bool) {
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err == nil {
		return 0, false
	}
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		return list[0].Pos.Line, true
	}
	return 1, true
}

var innerQuoteRE = regexp.MustCompile(`^(.*?:\s*|.*?\(\s*|\s*)"(.*)"(\s*[,)}\]]*\s*)$`)

func syntaxCandidates(src string, line int) []string {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return nil
	}
	i := line - 1
	indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
	join := func(ls []string) string { return strings.Join(ls, "\n") }
	// The targeted edits come first: a brace edit can move the error without
	// fixing anything.
	var out []string
	// A newline or tab written as the two characters \n or \t on a line with
	// no string literal.
	if !strings.Contains(lines[i], `"`) && (strings.Contains(lines[i], `\n`) || strings.Contains(lines[i], `\t`)) {
		esc := append([]string{}, lines...)
		esc[i] = strings.NewReplacer(`\n`, "\n", `\t`, "\t").Replace(lines[i])
		out = append(out, join(esc))
	}
	// Quotes inside a string literal that are not escaped.
	if m := innerQuoteRE.FindStringSubmatch(lines[i]); m != nil && strings.Contains(m[2], `"`) && !strings.Contains(m[2], `\"`) {
		esc := append([]string{}, lines...)
		esc[i] = m[1] + `"` + strings.ReplaceAll(m[2], `"`, `\"`) + `"` + m[3]
		out = append(out, join(esc))
	}
	// A stray quote after the last brace.
	if t := strings.TrimRight(src, "\n \t"); strings.HasSuffix(t, `}"`) {
		out = append(out, strings.TrimSuffix(t, `"`)+"\n")
	}
	// A closing brace missing just before the reported line.
	ins := append(append(append([]string{}, lines[:i]...), indent+"}"), lines[i:]...)
	out = append(out, join(ins))
	// A function literal passed as an argument (t.Run) closed with "}"
	// where ")" must follow.
	if strings.TrimSpace(lines[i]) == "}" {
		rep := append([]string{}, lines...)
		rep[i] = indent + "})"
		out = append(out, join(rep))
	}
	// A literal closed with "}," where its statement ends: "}" at the
	// reported line's indent.
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		if strings.TrimSpace(lines[j]) == "}," {
			rep := append([]string{}, lines...)
			rep[j] = indent + "}"
			out = append(out, join(rep))
			break
		}
		if strings.TrimSpace(lines[j]) != "" {
			break
		}
	}
	// One closing brace too many on a line just before it.
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		if t := strings.TrimSpace(lines[j]); t == "}," || t == "}" {
			del := append(append([]string{}, lines[:j]...), lines[j+1:]...)
			out = append(out, join(del))
			break
		}
	}
	return out
}
