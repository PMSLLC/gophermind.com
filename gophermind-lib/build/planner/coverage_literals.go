package planner

import (
	"regexp"
	"strings"
)

// literalVerbs mark a sentence of a feature that states an observable result:
// the backticked spans in it are the values a plan must produce.
var literalVerbs = []string{"prints", "writes", "exits with code", "returns", "line is"}

// subjectVerbRE matches a trigger verb right after a span: the span is the
// subject of the sentence, an invocation (`hello Ada` prints ...), not a result.
var subjectVerbRE = regexp.MustCompile(`^\s*(prints|writes|exits|returns)\b`)

// inputWordRE matches a word that ends the text before a span when the span is
// not a result of the sentence: an input (with `Ada` it writes ..., for `x`,
// given `y`) or a destination (writes ... to `stdout`, prints ... on `stderr`).
var inputWordRE = regexp.MustCompile(`(?i)\b(with|for|given|argument|arguments|input|from|to|on)$`)

// goTypeRE is a Go type or error value named in a sentence (`*NameError`,
// `io.EOF`, `ErrEmpty`): what kind of value is returned, which the contract's
// errors and their tests settle, not output text to carry or assert.
var goTypeRE = regexp.MustCompile(`^(\*[A-Za-z_][A-Za-z0-9_.]*|[a-z][a-z0-9]*\.[A-Z][A-Za-z0-9_]*|Err[A-Z][A-Za-z0-9_]*|[A-Z][A-Za-z0-9]*Error)$`)

// exitCodeRE is the integer after "code" in a sentence: the exit code it states.
var exitCodeRE = regexp.MustCompile("\\bcode\\s+`?([0-9]+)`?")

// requirementLiterals is the output literals a feature (F) requirement states:
// the backticked spans after the trigger verb of a sentence that says what is
// printed, written, returned or exited with, plus a non-zero exit code it
// names. A span that is an invocation is not a literal: one that starts with a
// binary name followed by a space or the end, or one that is the subject of a
// trigger verb. Nor is an input or a destination: a span right after with,
// for, given, argument(s), input, from, to or on. Exit
// code 0 is success, which is not a behaviour of its own. Other kinds have none.
func requirementLiterals(req Requirement, binaries ...string) []string {
	if req.Kind != ReqFeature {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, sent := range sentences(req.Text) {
		verb := firstVerb(sent)
		if verb < 0 {
			continue
		}
		for _, loc := range backtickRE.FindAllStringSubmatchIndex(sent, -1) {
			span := sent[loc[2]:loc[3]]
			if loc[0] < verb || isInvocation(span, binaries) || subjectVerbRE.MatchString(sent[loc[1]:]) || goTypeRE.MatchString(span) ||
				inputWordRE.MatchString(strings.TrimSpace(sent[:loc[0]])) {
				continue
			}
			if exitCodeRE.MatchString(sent[max(0, loc[0]-6):loc[1]]) {
				continue // the exit code, added below without its backticks
			}
			add(span)
		}
		for _, m := range exitCodeRE.FindAllStringSubmatch(sent, -1) {
			if m[1] != "0" {
				add(m[1])
			}
		}
	}
	return out
}

// firstVerb is the byte offset of the first trigger verb outside backticks in
// sent, or -1. A span before it is the condition or the command (`-` as FILE,
// a `--max` value that `strconv.Atoi` rejects), not the result.
func firstVerb(sent string) int {
	low := []byte(strings.ToLower(sent))
	quoted := false
	for i := range low {
		if low[i] == '`' {
			quoted = !quoted
			low[i] = ' '
		} else if quoted {
			low[i] = ' '
		}
	}
	first := -1
	for _, v := range literalVerbs {
		if i := strings.Index(string(low), v); i >= 0 && (first < 0 || i < first) {
			first = i
		}
	}
	return first
}

// isInvocation reports whether span starts with one of the binary names
// followed by a space or the end.
func isInvocation(span string, binaries []string) bool {
	for _, b := range binaries {
		if b != "" && (span == b || strings.HasPrefix(span, b+" ")) {
			return true
		}
	}
	return false
}

// sentences splits text into its sentences: each line (a bullet) apart, and a
// line at a '.', '!' or '?' followed by a space, outside backticks.
func sentences(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		start, quoted := 0, false
		for i := 0; i < len(line); i++ {
			switch c := line[i]; {
			case c == '`':
				quoted = !quoted
			case !quoted && (c == '.' || c == '!' || c == '?') && i+1 < len(line) && line[i+1] == ' ':
				out = appendSentence(out, line[start:i+1])
				start = i + 1
			}
		}
		out = appendSentence(out, line[start:])
	}
	return out
}

func appendSentence(out []string, s string) []string {
	s = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "-*"))
	if s == "" {
		return out
	}
	return append(out, s)
}

// placeholderRE is a placeholder in a literal, like <NAME>.
var placeholderRE = regexp.MustCompile(`<[A-Za-z_][A-Za-z0-9_ -]*>`)

// literalIn reports whether text carries lit. A placeholder in lit stands for
// any text on the line (Hello, <NAME>! is carried by Hello, %s! and by
// Hello, Ada!), and a number must stand as a word of its own. lit is matched
// as written: a test file writes a newline as the escape \n, as the brief does.
// A node's doc text is read with docCarries instead.
func literalIn(text, lit string) bool {
	if isDigits(lit) {
		return regexp.MustCompile(`(^|[^0-9])` + lit + `($|[^0-9])`).MatchString(text)
	}
	if !placeholderRE.MatchString(lit) {
		return strings.Contains(text, lit)
	}
	var b strings.Builder
	last := 0
	for _, loc := range placeholderRE.FindAllStringIndex(lit, -1) {
		b.WriteString(regexp.QuoteMeta(lit[last:loc[0]]))
		b.WriteString(`[^\n]+?`)
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(lit[last:]))
	return regexp.MustCompile(b.String()).MatchString(text)
}

// normalLiteral replaces each escaped newline of lit with a real one and trims
// trailing newlines. Other escapes are left alone. A literal that is nothing
// but newlines is kept as written.
func normalLiteral(lit string) string {
	if n := strings.TrimRight(strings.ReplaceAll(lit, `\n`, "\n"), "\n"); n != "" {
		return n
	}
	return lit
}

// coveredByPattern reports whether lit is an example of one of the pattern
// literals (those with a placeholder) that all carries: Hello, Ada! of a
// carried Hello, <name>!. patterns are the ones lit is an example of
// (examplePatterns).
func coveredByPattern(all string, lit string, patterns []string) bool {
	for _, p := range patterns {
		if literalIn(lit, p) && docCarries(all, p) {
			return true
		}
	}
	return false
}

// examplePatterns lists the patterns lit, a literal of requirement text, is an
// example of: lit is the pattern with each placeholder filled by text that the
// requirement names in a backticked span that is not one of its literals, lits
// (the input the example is made from: `Greeting("Ada")` returns
// `Hello, Ada!`). Hello, World! for an empty name fills the placeholder with
// text named nowhere else, so it is a behaviour of its own, not an example.
func examplePatterns(lit, text string, lits, patterns []string) []string {
	lit = normalLiteral(lit)
	var inputs []string
	for _, m := range backtickRE.FindAllStringSubmatch(text, -1) {
		if !contains(lits, m[1]) {
			inputs = append(inputs, m[1])
		}
	}
	var out []string
	for _, p := range patterns {
		if p == lit || placeholderRE.MatchString(lit) {
			continue
		}
		var b strings.Builder
		last := 0
		for _, loc := range placeholderRE.FindAllStringIndex(p, -1) {
			b.WriteString(regexp.QuoteMeta(p[last:loc[0]]))
			b.WriteString(`([^\n]+?)`)
			last = loc[1]
		}
		b.WriteString(regexp.QuoteMeta(p[last:]))
		m := regexp.MustCompile(`^` + b.String() + `$`).FindStringSubmatch(lit)
		if m == nil {
			continue
		}
		named := true
		for _, fill := range m[1:] {
			found := false
			for _, in := range inputs {
				if strings.Contains(in, fill) {
					found = true
					break
				}
			}
			named = named && found
		}
		if named {
			out = append(out, p)
		}
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// nodeBehaviourText is what a function node says it does: its description,
// its contract doc, outputs and errors, one per line.
func nodeBehaviourText(d map[string]any, doc string) string {
	desc, _ := d["description"].(string)
	ct, _ := d["contract"].(map[string]any)
	return strings.Join([]string{desc, doc, jsonText(ct["outputs"]), jsonText(ct["errors"])}, "\n")
}

// literalGaps checks the behaviour behind each covered feature requirement:
// every literal it states must appear in the text (title, description, doc,
// outputs, errors) of at least one node mapped to it, of a function of a
// mapped component, or of a node those use (transitively). A requirement with
// literals missing is one gap, its reason naming each missing literal, so the
// fill round sees what the plan lacks. An example of a pattern literal of any
// feature (Hello, Ada! of Hello, <name>!, see examplePatterns) is carried when
// the pattern is, and is not named on its own when the requirement's own
// missing pattern is.
func literalGaps(reqs []Requirement, covered []Covered, nodes []PlanNode, binaries ...string) []Gap {
	byReq := map[string]Covered{}
	for _, c := range covered {
		byReq[c.Requirement] = c
	}
	byID := map[string]PlanNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var patterns []string
	for _, q := range reqs {
		for _, lit := range requirementLiterals(q, binaries...) {
			if placeholderRE.MatchString(lit) && !contains(patterns, lit) {
				patterns = append(patterns, lit)
			}
		}
	}
	var gaps []Gap
	for _, q := range reqs {
		if q.Kind != ReqFeature {
			continue
		}
		c, ok := byReq[q.ID]
		if !ok || len(c.Nodes) == 0 {
			continue
		}
		var texts []string
		seen := map[string]bool{}
		var queue []string
		for _, n := range nodes {
			if contains(c.Nodes, n.ID) || contains(c.Nodes, n.Parent) {
				queue = append(queue, n.ID)
			}
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			n, ok := byID[id]
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			texts = append(texts, n.Title+"\n"+n.Text)
			queue = append(queue, n.Uses...)
		}
		all := strings.Join(texts, "\n")
		lits := requirementLiterals(q, binaries...)
		var missing []string
		for _, lit := range lits {
			if !docCarries(all, lit) && !coveredByPattern(all, normalLiteral(lit), examplePatterns(lit, q.Text, lits, patterns)) {
				missing = append(missing, lit)
			}
		}
		var own []string // this requirement's missing patterns, named in place of their examples
		for _, lit := range missing {
			if placeholderRE.MatchString(lit) {
				own = append(own, lit)
			}
		}
		var why []string
		for _, lit := range missing {
			if len(examplePatterns(lit, q.Text, lits, own)) == 0 {
				why = append(why, "behaviour missing: "+lit)
			}
		}
		if len(why) > 0 {
			gaps = append(gaps, Gap{Requirement: q.ID, Line: q.Line, Text: q.Text, Reason: strings.Join(why, "; ")})
		}
	}
	return gaps
}

// docCarries reports whether a node's doc text carries lit, read with
// normalLiteral first: an escaped newline in lit is a real one, and a trailing
// one is dropped, because the doc writes the text with a real newline or none
// and the test proves it.
func docCarries(text, lit string) bool {
	return literalIn(text, normalLiteral(lit))
}
