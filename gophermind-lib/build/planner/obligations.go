package planner

import (
	"fmt"
	"go/format"
	"regexp"
	"strconv"
	"strings"

	"gophermind/gophermind-lib/build/contract"
	"gophermind/gophermind-lib/build/events"
)

// stateObligations is the Tests bullets of the brief, attached to the
// production functions they call, as the testwriter stage computed them.
const stateObligations = "_state/obligations.json"

// TestObligation is one bullet of the brief's Tests section, attached to the
// production function it exercises.
type TestObligation struct {
	ID     string `json:"id"`     // T3
	Target string `json:"target"` // contract function id, fn-run
	Text   string `json:"text"`   // the bullet, verbatim
	Line   int    `json:"line"`
}

// obligationsFile is _state/obligations.json.
type obligationsFile struct {
	Obligations []TestObligation `json:"obligations"`
	Unattached  []string         `json:"unattached"`
}

var (
	identRE  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	numberRE = regexp.MustCompile(`^[-+]?\.?[0-9]`)
	// bareNumberRE is a span that is only a Go number literal (0, -1, 2.5, 0x1f).
	bareNumberRE = regexp.MustCompile(`^[-+]?(0[xXoObB][0-9a-fA-F_]+|[0-9][0-9_]*(\.[0-9_]*)?([eE][-+]?[0-9]+)?|\.[0-9]+([eE][-+]?[0-9]+)?)$`)
	compositRE   = regexp.MustCompile(`^(\[|\{|map\[|[A-Za-z_][A-Za-z0-9_.]*\{)`)
)

// backtickSpans is the text of every backticked span of a bullet, in order.
func backtickSpans(text string) []string {
	var out []string
	for _, m := range backtickRE.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// obligationCall is the bullet's call span: its first backticked span that
// contains "(", or "" when it has none.
func obligationCall(text string) string {
	for _, s := range backtickSpans(text) {
		if strings.Contains(s, "(") {
			return s
		}
	}
	return ""
}

// calleeOf is the identifier before the first "(" of a call span; a method or
// qualified call x.Run( gives Run. "" when that is not an identifier.
func calleeOf(call string) string {
	i := strings.Index(call, "(")
	if i < 0 {
		return ""
	}
	name := strings.TrimSpace(call[:i])
	if j := strings.LastIndex(name, "."); j >= 0 {
		name = name[j+1:]
	}
	if !identRE.MatchString(name) {
		return ""
	}
	return name
}

// obligationTarget is the function name a bullet names: the callee of its
// first backticked call (`Run(nil, stdout, stderr)` names Run), or, when it has
// no call span, its only backticked span when that is a single identifier
// (No `Greeting` result ends with a newline. names Greeting). Such a bullet is
// a text-only obligation: no call means no argument, and an identifier is
// never expected text. "" for anything else.
func obligationTarget(text string) string {
	if call := obligationCall(text); call != "" {
		return calleeOf(call)
	}
	if spans := backtickSpans(text); len(spans) == 1 && identRE.MatchString(spans[0]) && !strings.Contains(spans[0], ".") {
		return spans[0]
	}
	return ""
}

// testObligations attaches every kind-test requirement to the contract function
// whose declared name obligationTarget reads from the bullet. A bullet that
// names no function, or whose name matches no function or more than one, is
// returned in unattached.
func testObligations(reqs []Requirement, c *contract.Contracts) (attached []TestObligation, unattached []Requirement) {
	byName := map[string][]string{}
	if c != nil {
		for _, f := range c.Functions {
			name, _, _ := internalTarget(f.Signature)
			if name != "" && !contains(byName[name], f.ID) {
				byName[name] = append(byName[name], f.ID)
			}
		}
	}
	for _, q := range reqs {
		if q.Kind != ReqTest {
			continue
		}
		ids := byName[obligationTarget(q.Text)]
		if len(ids) != 1 {
			unattached = append(unattached, q)
			continue
		}
		attached = append(attached, TestObligation{ID: q.ID, Target: ids[0], Text: q.Text, Line: q.Line})
	}
	return attached, unattached
}

// nodeObligations is the obligations attached to node id, in id order.
func nodeObligations(all []TestObligation, id string) []TestObligation {
	var out []TestObligation
	for _, o := range all {
		if o.Target == id {
			out = append(out, o)
		}
	}
	return out
}

// splitArgs splits the text between a call's parentheses at its top-level
// commas, respecting nested brackets and quoted strings.
func splitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == '\\' && quote != '`' {
				i++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			quote = ch
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" || len(out) > 0 {
		out = append(out, last)
	}
	return out
}

// callArgs is the text between the first "(" of a call span and its matching
// ")", or the rest of the span when it is not closed.
func callArgs(call string) string {
	i := strings.Index(call, "(")
	if i < 0 {
		return ""
	}
	depth := 0
	var quote byte
	for j := i; j < len(call); j++ {
		ch := call[j]
		if quote != 0 {
			if ch == '\\' && quote != '`' {
				j++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			quote = ch
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return call[i+1 : j]
			}
		}
	}
	return call[i+1:]
}

// isGoLiteral reports whether an argument is a Go literal: nil, a quoted
// string, a number, or a composite literal. An identifier, an address or a
// call is not.
func isGoLiteral(arg string) bool {
	switch {
	case arg == "nil":
		return true
	case arg == "":
		return false
	case arg[0] == '"' || arg[0] == '`' || arg[0] == '\'':
		return true
	case numberRE.MatchString(arg):
		return true
	case compositRE.MatchString(arg) && strings.HasSuffix(arg, "}"):
		return true
	}
	return false
}

// literalArgs is the literal arguments of a call span, in order.
func literalArgs(call string) []string {
	var out []string
	for _, a := range splitArgs(callArgs(call)) {
		if isGoLiteral(a) {
			out = append(out, a)
		}
	}
	return out
}

// obligationArgs is the literal arguments of every backticked call in the
// bullet whose callee is the callee of its first call: a bullet that lists
// `Run(nil, ...)` and `Run([]string{}, ...)` requires both. A call of another
// function contributes nothing. Each literal appears once, in order.
func obligationArgs(text string) []string {
	callee := calleeOf(obligationCall(text))
	if callee == "" {
		return nil
	}
	var out []string
	for _, s := range backtickSpans(text) {
		if !strings.Contains(s, "(") || calleeOf(s) != callee {
			continue
		}
		for _, lit := range literalArgs(s) {
			if !contains(out, lit) {
				out = append(out, lit)
			}
		}
	}
	return out
}

// expectedTexts is the backticked spans of a bullet that state a value a test
// must hold: not a call span (of any callee), not a bare identifier or nil,
// and not a bare number, which any file contains somewhere.
func expectedTexts(text string) []string {
	var out []string
	for _, s := range backtickSpans(text) {
		if (strings.Contains(s, "(") && calleeOf(s) != "") || identRE.MatchString(s) || bareNumberRE.MatchString(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// containsText reports whether src holds want as written or in its Go-quoted
// form without the outer quotes, the way it appears inside a string literal
// when it holds a quote or a backslash.
func containsText(src, want string) bool {
	if strings.Contains(src, want) {
		return true
	}
	q := strconv.Quote(want)
	return strings.Contains(src, q[1:len(q)-1])
}

// noSpace is s without white space, so a literal written `[]string{"a","b"}`
// in one place and `[]string{"a", "b"}` in the other still match.
func noSpace(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// checkObligations refuses a test file that leaves one of the node's
// obligations uncited by every test entry (cited is the ids the reply's
// entries list), or whose source lacks a literal argument of one of the
// obligation's calls or one of its expected texts. Only the obligation's own words are
// quoted back.
func checkObligations(obs []TestObligation, cited map[string]bool, src string) error {
	if len(obs) == 0 {
		return nil
	}
	if f, err := format.Source([]byte(src)); err == nil {
		src = string(f)
	}
	flat := noSpace(src)
	for _, o := range obs {
		if !cited[o.ID] {
			return fmt.Errorf("no test case covers obligation %s (%s)", o.ID, oneLine(o.Text, 300))
		}
		for _, lit := range obligationArgs(o.Text) {
			if !strings.Contains(flat, noSpace(lit)) {
				return fmt.Errorf("test file does not cover obligation %s: the argument %s is absent", o.ID, lit)
			}
		}
		for _, want := range expectedTexts(o.Text) {
			if !containsText(src, want) {
				return fmt.Errorf("test file does not cover obligation %s: expected text %s is absent", o.ID, want)
			}
		}
	}
	return nil
}

// coveredObligations is the ids of obs that cited lists, in obs order.
func coveredObligations(obs []TestObligation, cited map[string]bool) []string {
	var out []string
	for _, o := range obs {
		if cited[o.ID] {
			out = append(out, o.ID)
		}
	}
	return out
}

// obligationsBlock is the <obligations> lines of the test writer prompt.
func obligationsBlock(obs []TestObligation) string {
	lines := make([]string, 0, len(obs))
	for _, o := range obs {
		lines = append(lines, o.ID+": "+oneLine(o.Text, 2000))
	}
	return strings.Join(lines, "\n")
}

// writeObligations computes the obligations of the run once, writes
// _state/obligations.json and warns about each bullet that names no single
// contract function.
func (p *Planner) writeObligations(r *run, c *contract.Contracts) ([]TestObligation, error) {
	attached, unattached := testObligations(r.reqs, c)
	f := obligationsFile{Obligations: attached, Unattached: []string{}}
	if f.Obligations == nil {
		f.Obligations = []TestObligation{}
	}
	for _, q := range unattached {
		f.Unattached = append(f.Unattached, q.ID)
		p.emit(events.KindWarning, "testwrite", "", "test_obligation_unattached: "+q.ID+": "+oneLine(q.Text, 300))
	}
	if err := writeJSON(r.path(stateObligations), f); err != nil {
		return nil, err
	}
	return attached, nil
}

// loadObligations is the run's attached obligations as the testwriter stage
// wrote them in _state/obligations.json; a run planned before that file
// existed gets them computed from its requirements and contract.
func loadObligations(r *run, c *contract.Contracts) ([]TestObligation, error) {
	var f obligationsFile
	found, err := readJSON(r.path(stateObligations), &f)
	if err != nil {
		return nil, err
	}
	if found {
		return f.Obligations, nil
	}
	attached, _ := testObligations(r.reqs, c)
	return attached, nil
}

// obligationIDs is the ids of obs, in order; nil for none.
func obligationIDs(obs []TestObligation) []string {
	var out []string
	for _, o := range obs {
		out = append(out, o.ID)
	}
	return out
}
