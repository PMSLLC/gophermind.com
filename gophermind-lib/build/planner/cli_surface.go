package planner

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// CLISurface is the command-line surface a brief states: the flags it names,
// the accepted values of a flag, whether "-" reads standard input, and the exit
// codes. It is found by code, not by a model, so the contract and the clarify
// answers can be checked against what the brief says word for word.
type CLISurface struct {
	Flags     []string            // "--format"
	Values    map[string][]string // flag -> accepted values ("text", "json")
	Stdin     bool                // "-" as the file name reads standard input
	ExitCodes []string            // "0", "1", "2"
	Quotes    []string            // the brief lines the items came from
}

var (
	flagTokenRE = regexp.MustCompile(`(?:^|\s)(--?[A-Za-z][A-Za-z0-9-]*)`)
	bareWordRE  = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	stdinWordRE = regexp.MustCompile(`(?i)\bstdin\b|standard input`)
	exitPhrase  = regexp.MustCompile(`(?i)\bexit(?:s|ing)?\b[^.\n]*?\bcode\b([^.\n]*)`)
	digitsRE    = regexp.MustCompile(`\b\d+\b`)
)

// ExtractCLISurface reads the command-line surface out of a brief. Flags come
// from code spans only (a span led by a tool such as go or grep is a check, not
// the product); values are the bare words in code spans on the line of a span
// that is exactly a flag; "-" counts when a line has a `-` span and mentions
// standard input; exit codes are the numbers after "exit ... code".
func ExtractCLISurface(src string) CLISurface {
	s := CLISurface{Values: map[string][]string{}}
	seenFlag, seenCode, seenQuote := map[string]bool{}, map[string]bool{}, map[string]bool{}
	quote := func(line string) {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if !seenQuote[line] {
			seenQuote[line] = true
			s.Quotes = append(s.Quotes, line)
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		spans := codeSpanRE.FindAllStringSubmatch(line, -1)
		var exact []string
		var words []string
		dash := false
		for _, m := range spans {
			span := strings.TrimSpace(m[1])
			if span == "-" {
				dash = true
			}
			if bareWordRE.MatchString(span) {
				words = append(words, span)
			}
			if f := strings.Fields(span); len(f) > 0 && notBinaries[f[0]] {
				continue
			}
			for _, t := range flagTokenRE.FindAllStringSubmatch(span, -1) {
				if !seenFlag[t[1]] {
					seenFlag[t[1]] = true
					s.Flags = append(s.Flags, t[1])
				}
				quote(line)
				if span == t[1] {
					exact = append(exact, t[1])
				}
			}
		}
		for _, f := range exact {
			for _, w := range words {
				if !contains(s.Values[f], w) {
					s.Values[f] = append(s.Values[f], w)
				}
			}
		}
		if dash && stdinWordRE.MatchString(line) {
			s.Stdin = true
			quote(line)
		}
		for _, m := range exitPhrase.FindAllStringSubmatch(line, -1) {
			for _, d := range digitsRE.FindAllString(m[1], -1) {
				if !seenCode[d] {
					seenCode[d] = true
					s.ExitCodes = append(s.ExitCodes, d)
				}
			}
			quote(line)
		}
	}
	return s
}

// Empty is true when the brief states no command-line surface.
func (s CLISurface) Empty() bool {
	return len(s.Flags) == 0 && !s.Stdin && len(s.ExitCodes) == 0
}

// Items lists the surface as short phrases, for a prompt or an answer.
func (s CLISurface) Items() []string {
	var out []string
	for _, f := range s.Flags {
		item := "flag " + f
		if v := s.Values[f]; len(v) > 0 {
			item += " (accepted values: " + strings.Join(v, ", ") + ")"
		}
		out = append(out, item)
	}
	if s.Stdin {
		out = append(out, "the file name - reads standard input (stdin)")
	}
	for _, c := range s.ExitCodes {
		out = append(out, "exit code "+c)
	}
	return out
}

// Text is Items as a bulleted block followed by the brief's own lines.
func (s CLISurface) Text() string {
	var b strings.Builder
	for _, it := range s.Items() {
		b.WriteString("- " + it + "\n")
	}
	if len(s.Quotes) > 0 {
		b.WriteString("The brief says:\n")
		for _, q := range s.Quotes {
			b.WriteString("> " + q + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Missing lists the items text does not state. A flag and each of its values
// must appear verbatim; standard input by the word stdin or "standard input";
// an exit code as the number near the word exit, return or code.
func (s CLISurface) Missing(text string) []string {
	var out []string
	for _, f := range s.Flags {
		if !strings.Contains(text, f) {
			out = append(out, "flag "+f)
			continue
		}
		for _, v := range s.Values[f] {
			if !regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\b`).MatchString(text) {
				out = append(out, fmt.Sprintf("the value %s of %s", v, f))
			}
		}
	}
	if s.Stdin && !stdinWordRE.MatchString(text) {
		out = append(out, "the file name - reads standard input (stdin)")
	}
	for _, c := range s.ExitCodes {
		n := regexp.QuoteMeta(c)
		near := regexp.MustCompile(`(?i)(?:exit|return|code)[^\n]{0,80}\b` + n + `\b|\b` + n + `\b[^\n]{0,80}(?:exit|return|code)`)
		if !near.MatchString(text) {
			out = append(out, "exit code "+c)
		}
	}
	return out
}

// Touches is true when a question or its unattended default is about the
// command-line surface: it names a flag, stdin, an exit code or an argument.
// The brief already fixes those, so a default is not asked for them.
func (s CLISurface) Touches(question, answer string) bool {
	if s.Empty() {
		return false
	}
	text := question + "\n" + answer
	if stdinWordRE.MatchString(text) {
		return true
	}
	if regexp.MustCompile(`(?i)\b(flags?|arguments?|exit codes?|command[- ]line|cli)\b`).MatchString(text) {
		return true
	}
	return flagTokenRE.MatchString(text)
}

// isEntryPackage is true for a package under cmd/.
func isEntryPackage(pkg string) bool {
	rest, ok := strings.CutPrefix(path.Clean(pkg), "cmd/")
	return ok && rest != ""
}

// runFunction is the run function of a component: the one whose signature is
// func run(...).
func runFunction(doc map[string]any, component string) map[string]any {
	for _, f := range objects(doc["functions"]) {
		if sig, _ := f["signature"].(string); f["component"] == component && strings.HasPrefix(sig, "func run(") {
			return f
		}
	}
	return nil
}

// functionText is what a function states: its doc. The signature is left out
// because run's own parameter is named stdin.
func functionText(f map[string]any) string { return fmt.Sprint(f["doc"]) }

func sameFunction(a, b map[string]any) bool { return a["id"] == b["id"] }

func hasFunction(doc map[string]any, f map[string]any) bool {
	for _, g := range objects(doc["functions"]) {
		if sameFunction(g, f) {
			return true
		}
	}
	return false
}

// withoutFunction is a shallow copy of doc whose function list lacks f.
func withoutFunction(doc map[string]any, f map[string]any) map[string]any {
	next := map[string]any{}
	for k, v := range doc {
		next[k] = v
	}
	var kept []any
	for _, g := range objects(doc["functions"]) {
		if !sameFunction(g, f) {
			kept = append(kept, g)
		}
	}
	next["functions"] = kept
	return next
}

func objectsAsAny(v any) []any {
	var out []any
	for _, o := range objects(v) {
		out = append(out, o)
	}
	return out
}

// surfaceDefectText names the run function and the items its doc leaves out.
func surfaceDefectText(id string, missing []string) string {
	if !componentIDRE.MatchString(id) || len(id) > maxBoundedID {
		id = fmt.Sprintf("<%d bytes>", len(id))
	}
	return "the doc of " + id + " does not state these items of the command-line surface the brief fixes: " +
		strings.Join(missing, "; ") + ". Write " + id + " again with the same id, signature and uses, and a doc that states every item exactly as the brief does"
}
