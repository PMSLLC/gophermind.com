// Package packer builds the prompt for one leaf (need-to-know, budgeted, never
// stored) and checks the reply (reply.go). Prompt text is carried only in
// Packed.Text, which the executor's request builder reads; every other view of
// a Packed shows sizes and a SHA-256 only.
package packer

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"

	"gophermind/gophermind-lib/briefv2/contract"
)

//go:embed prompts/*.md
var promptFS embed.FS

// SystemPrefix starts the system message of every executor call; the stage follows, e.g. "implement:fn-greet".
const SystemPrefix = "GopherMind executor. Stage: "

// NodeView is everything the packer may know about one leaf. The executor fills
// it; the packer never reads a file or a repo.
type NodeView struct {
	ID                   string
	FuncID               string   // contract function id
	Package              string   // package name from the contract
	File                 string   // contract.file, repo-relative
	Signature            string   // contract.signature, exactly as declared
	DependencySignatures []string // harness-derived
	Constraints          []string
	TestFile             string // repo-relative path of the leaf's own test file
	TestSource           string
	MaxContextTokens     int // node budget; 0 when unset
}

const (
	maxFailureNames = 8
	maxFailureKeep  = 15   // lines kept at each end of the failure text
	maxFailureBytes = 2048 // total, marker included
	maxLineBytes    = 512  // one line is clipped (head and tail kept) to this
	condenseKeep    = 5    // lines kept at each end when the budget cut applies
)

// Failure is the only thing carried from one attempt to the next, in memory only.
type Failure struct {
	Names []string
	Lines []string
}

// NewFailure keeps at most 8 names and the failure text: the first and last
// lines of the output (up to 15 at each end), a line `[N lines omitted]`
// between them when lines were cut, and at most 2048 bytes in total. A line
// longer than 512 bytes keeps its head and tail with `...` between. Names and
// lines that contain a secret value (see minSecretLen) are dropped first, and so
// are lines that together spell one with whitespace removed, so a cut cannot
// leave part of a secret behind.
func NewFailure(names []string, output string, secrets []string) Failure {
	sc := newScan(secrets)
	var f Failure
	for _, n := range names {
		if n == "" || sc.has(n) {
			continue
		}
		if len(f.Names) == maxFailureNames {
			break
		}
		f.Names = append(f.Names, strings.ToValidUTF8(n, "?"))
	}
	var lines []string
	for _, raw := range strings.Split(output, "\n") {
		if sc.has(raw) {
			continue
		}
		l := strings.TrimRight(raw, "\r")
		if i := strings.LastIndex(l, "\r"); i >= 0 {
			l = l[i+1:]
		}
		l = strings.TrimRight(strings.ToValidUTF8(l, "?"), " \t")
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
	}
	f.Lines = fitLines(sc.scrub(lines))
	return f
}

// Empty reports whether there is nothing to feed back.
func (f Failure) Empty() bool { return len(f.Names) == 0 && len(f.Lines) == 0 }

var omittedRE = regexp.MustCompile(`^\[(\d+) lines omitted\]$`)

func clipLine(l string) string {
	if len(l) <= maxLineBytes {
		return l
	}
	n := (maxLineBytes - 3) / 2
	h := n
	for h > 0 && !utf8.RuneStart(l[h]) {
		h--
	}
	t := len(l) - n
	for t < len(l) && !utf8.RuneStart(l[t]) {
		t++
	}
	return l[:h] + "..." + l[t:]
}

// condense keeps the first k and last k lines and puts `[N lines omitted]`
// between them; N counts real lines (an earlier marker adds its own count).
func condense(lines []string, k int) []string {
	if len(lines) <= 2*k {
		return lines
	}
	omitted := 0
	for _, l := range lines[k : len(lines)-k] {
		if m := omittedRE.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[1])
			omitted += n
		} else {
			omitted++
		}
	}
	out := append([]string(nil), lines[:k]...)
	out = append(out, fmt.Sprintf("[%d lines omitted]", omitted))
	return append(out, lines[len(lines)-k:]...)
}

func fitLines(lines []string) []string {
	clipped := make([]string, len(lines))
	for i, l := range lines {
		clipped[i] = clipLine(l)
	}
	for k := maxFailureKeep; ; k-- {
		out := condense(clipped, k)
		size := 0
		for _, l := range out {
			size += len(l) + 1
		}
		if size <= maxFailureBytes || k == 1 {
			return out
		}
	}
}

// minSecretLen is the shortest secret value that is scanned for. A shorter
// value is not a credible secret (it would match ordinary text and drop
// legitimate output); it still never reaches a prompt by construction, because
// values are never put into any prompt input.
const minSecretLen = 6

// scan finds secret values. For each secret of minSecretLen bytes or more it
// matches the raw value and its %q-escaped, JSON-escaped, URL-escaped and
// standard and URL base64 forms.
type scan struct {
	forms    []string
	stripped []string
}

func stripWS(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func newScan(secrets []string) *scan {
	sc := &scan{}
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			sc.forms = append(sc.forms, s)
			if st := stripWS(s); len(st) >= minSecretLen && !seen["\x00"+st] {
				seen["\x00"+st] = true
				sc.stripped = append(sc.stripped, st)
			}
		}
	}
	for _, s := range secrets {
		if len(s) < minSecretLen {
			continue
		}
		add(s)
		q := strconv.Quote(s)
		add(q[1 : len(q)-1])
		j, _ := json.Marshal(s)
		add(string(j[1 : len(j)-1]))
		add(url.QueryEscape(s))
		add(url.PathEscape(s))
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
			add(enc.EncodeToString([]byte(s)))
		}
	}
	return sc
}

// HasSecret reports whether s holds any of the secret values in one of the
// forms the packer scrubs (raw, %q, JSON, URL and base64, and the raw secret
// with whitespace removed, matched against s with its whitespace removed, each
// for secrets of minSecretLen bytes or more). Callers outside the packer use it so that there
// is one definition of "contains a secret".
func HasSecret(s string, secrets []string) bool {
	sc := newScan(secrets)
	if sc.has(s) {
		return true
	}
	if len(sc.stripped) == 0 {
		return false
	}
	w := stripWS(s)
	for _, st := range sc.stripped {
		if strings.Contains(w, st) {
			return true
		}
	}
	return false
}

func (sc *scan) has(s string) bool {
	for _, f := range sc.forms {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

// scrub drops every line that contains a secret, and every line that takes
// part in a secret spelled across lines (whitespace removed), until none is left.
func (sc *scan) scrub(lines []string) []string {
	cur := make([]string, 0, len(lines))
	for _, l := range lines {
		if !sc.has(l) {
			cur = append(cur, l)
		}
	}
	for len(sc.stripped) > 0 {
		var flat []byte
		var owner []int
		for i, l := range cur {
			for _, b := range []byte(stripWS(l)) {
				flat = append(flat, b)
				owner = append(owner, i)
			}
		}
		bad := map[int]bool{}
		for _, st := range sc.stripped {
			for from := 0; ; {
				i := bytes.Index(flat[from:], []byte(st))
				if i < 0 {
					break
				}
				i += from
				for j := i; j < i+len(st); j++ {
					bad[owner[j]] = true
				}
				from = i + 1
			}
		}
		if len(bad) == 0 {
			break
		}
		next := make([]string, 0, len(cur))
		for i, l := range cur {
			if !bad[i] {
				next = append(next, l)
			}
		}
		cur = next
	}
	return cur
}

// Inputs is what the caller adds to a NodeView for one attempt.
type Inputs struct {
	Failure Failure
	Notes   []string // revision notes for this node
	Budget  int      // tokens; the caller resolves node, brief, then settings default
	Secrets []string // vault values, for stripping (never logged)
}

// Packed carries the prompt text for the request and, for everything else, only
// sizes and a hash. String, GoString, Format and MarshalJSON never print Text.
type Packed struct {
	Text    string
	Bytes   int
	Tokens  int
	SHA256  string
	Dropped []string
}

func (p Packed) String() string {
	return fmt.Sprintf("packer.Packed{bytes:%d tokens:%d sha256:%s}", p.Bytes, p.Tokens, p.SHA256)
}

func (p Packed) GoString() string { return p.String() }

// Format prints the same size-and-hash form for every verb.
func (p Packed) Format(f fmt.State, _ rune) { fmt.Fprint(f, p.String()) }

func (p Packed) MarshalJSON() ([]byte, error) { return []byte(`"` + p.String() + `"`), nil }

func newPacked(text string, dropped []string) Packed {
	sum := sha256.Sum256([]byte(text))
	return Packed{Text: text, Bytes: len(text), Tokens: EstimateTokens(len(text)), SHA256: hex.EncodeToString(sum[:]), Dropped: dropped}
}

// EstimateTokens is ceil(ceil(bytes/4) * 1.1) in integer arithmetic. The plan's
// table lists 1101 for 4001 bytes; that is an arithmetic slip, the formula gives
// 1102 (ceil(1001 * 1.1)) and the formula is what is implemented.
func EstimateTokens(bytes int) int {
	t := (bytes + 3) / 4
	return (t*11 + 9) / 10
}

// ErrFloorOverBudget means the required sections alone exceed the budget.
type ErrFloorOverBudget struct{ Tokens, Budget int }

func (e *ErrFloorOverBudget) Error() string {
	return fmt.Sprintf("packer: required sections need %d tokens, budget is %d", e.Tokens, e.Budget)
}

// MaxTokens is min(4096, contextTokens - promptTokens); ok is false below 512.
// A contextTokens of 0 or less means unknown.
func MaxTokens(contextTokens, promptTokens int) (max int, ok bool) {
	if contextTokens <= 0 {
		return 4096, true
	}
	max = contextTokens - promptTokens
	if max > 4096 {
		max = 4096
	}
	return max, max >= 512
}

func renderTemplate(name string, data any) (string, error) {
	raw, err := promptFS.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// state is the mutable part of a prompt while sections are being cut.
type state struct {
	n        NodeView
	contract string
	deps     []string
	notes    []string
	fail     Failure
	testSrc  string
}

var sectionTagRE = regexp.MustCompile(`(?i)<\s*(file|signature|contract|dependency_signatures|constraints|revision_notes|tests|previous_failure|attempts)\s*>`)

// esc makes untrusted text unable to close or open a section: every "</"
// becomes `<\/` and a bare section tag gets a backslash after the "<".
func esc(s string) string {
	s = strings.ReplaceAll(s, "</", `<\/`)
	return sectionTagRE.ReplaceAllString(s, `<\$1>`)
}

func escAll(m map[string]string) map[string]string {
	for k, v := range m {
		m[k] = esc(v)
	}
	return m
}

func (s *state) render() (string, error) {
	deps := "(none)"
	if len(s.deps) > 0 {
		deps = strings.Join(s.deps, "\n\n")
	}
	cons := "(none)"
	if len(s.n.Constraints) > 0 {
		cons = "- " + strings.Join(s.n.Constraints, "\n- ")
	}
	notes := ""
	if len(s.notes) > 0 {
		notes = "- " + strings.Join(s.notes, "\n- ")
	}
	failure := ""
	if !s.fail.Empty() {
		var b strings.Builder
		if len(s.fail.Names) > 0 {
			b.WriteString("Failing tests: " + strings.Join(s.fail.Names, ", "))
		}
		if len(s.fail.Lines) > 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(strings.Join(s.fail.Lines, "\n"))
		}
		failure = b.String()
	}
	return renderTemplate("implement", escAll(map[string]string{
		"File": s.n.File, "Package": s.n.Package, "Signature": s.n.Signature,
		"Contract": s.contract, "Deps": deps, "Constraints": cons, "Notes": notes,
		"TestFile": s.n.TestFile, "TestSource": strings.TrimRight(s.testSrc, "\n"), "Failure": failure,
	}))
}

// Pack builds the prompt for one leaf and cuts it to the budget in a fixed
// order: failure lines to 10, failure lines to none, revision notes, test
// comments, then dependency signatures the contract slice does not name (last
// to first). The signature, contract slice, constraints and the test's
// assertions are never cut. If the rest does not fit, ErrFloorOverBudget.
func Pack(n NodeView, c *contract.Contracts, in Inputs) (Packed, error) {
	if in.Budget <= 0 {
		return Packed{}, errors.New("packer: budget must be positive")
	}
	if c == nil {
		return Packed{}, errors.New("packer: contracts are required")
	}
	slice, err := c.Slice([]string{n.FuncID}, "")
	if err != nil {
		return Packed{}, errors.New("packer: the function is not in the contracts")
	}
	sc := newScan(in.Secrets)
	s := &state{
		n: n, contract: strings.Join(slice, "\n\n"), deps: append([]string(nil), n.DependencySignatures...),
		notes:   sc.scrub(in.Notes),
		fail:    Failure{Names: scrubNames(in.Failure.Names, sc), Lines: sc.scrub(in.Failure.Lines)},
		testSrc: n.TestSource,
	}
	text, err := s.render()
	if err != nil {
		return Packed{}, errors.New("packer: the prompt template failed")
	}
	if sc.has(text) {
		return Packed{}, errors.New("packer: a secret value is present in the prompt inputs")
	}
	var dropped []string
	fits := func() (bool, error) {
		t, err := s.render()
		if err != nil {
			return false, errors.New("packer: the prompt template failed")
		}
		text = t
		return EstimateTokens(len(text)) <= in.Budget, nil
	}
	ok, err := fits()
	if err != nil {
		return Packed{}, err
	}
	steps := []func() (string, bool){
		func() (string, bool) {
			if len(s.fail.Lines) <= 2*condenseKeep+1 {
				return "", false
			}
			s.fail.Lines = condense(s.fail.Lines, condenseKeep)
			return "failure:lines10", true
		},
		func() (string, bool) {
			if len(s.fail.Lines) == 0 {
				return "", false
			}
			s.fail.Lines = nil
			return "failure:names", true
		},
		func() (string, bool) {
			if len(s.notes) == 0 {
				return "", false
			}
			s.notes = nil
			return "notes", true
		},
		func() (string, bool) {
			out, _ := stripTestComments(s.testSrc)
			if len(out) >= len(s.testSrc) {
				return "", false
			}
			s.testSrc = out
			return "test_comments", true
		},
	}
	for _, step := range steps {
		if ok {
			break
		}
		name, changed := step()
		if !changed {
			continue
		}
		dropped = append(dropped, name)
		if ok, err = fits(); err != nil {
			return Packed{}, err
		}
	}
	if !ok {
		// Dependency signatures the contract slice does not name, last to first.
		unnamed := unnamedDeps(s.deps, slice)
		count := 0
		for i := len(unnamed) - 1; i >= 0 && !ok; i-- {
			s.deps = removeAt(s.deps, unnamed[i])
			count++
			if ok, err = fits(); err != nil {
				return Packed{}, err
			}
		}
		if count > 0 {
			dropped = append(dropped, fmt.Sprintf("dep_sig:%d", count))
		}
	}
	if !ok {
		return Packed{}, &ErrFloorOverBudget{Tokens: EstimateTokens(len(text)), Budget: in.Budget}
	}
	return newPacked(text, dropped), nil
}

func scrubNames(names []string, sc *scan) []string {
	var out []string
	for _, n := range names {
		if !sc.has(n) {
			out = append(out, n)
		}
	}
	return out
}

func removeAt(in []string, i int) []string {
	out := make([]string, 0, len(in)-1)
	out = append(out, in[:i]...)
	return append(out, in[i+1:]...)
}

var (
	declFuncRE  = regexp.MustCompile(`(?m)^func\s+(?:\(\s*\w*\s*\*?\s*(\w+)[^)]*\)\s*)?(\w+)`)
	declTypeRE  = regexp.MustCompile(`(?m)^(?:type|var|const)\s+(\w+)`)
	groupOpenRE = regexp.MustCompile(`^(?:var|const|type)\s*\($`)
	memberRE    = regexp.MustCompile(`^[\t ]+(\w+)\b`)
	wordRE      = regexp.MustCompile(`\w+`)
)

// declaredNames lists the identifiers one dependency block declares: function
// and method names, receiver types, types, vars, consts and the members of a
// grouped var, const or type block (only inside the parentheses).
func declaredNames(block string) []string {
	var names []string
	for _, m := range declFuncRE.FindAllStringSubmatch(block, -1) {
		names = append(names, m[1], m[2])
	}
	for _, m := range declTypeRE.FindAllStringSubmatch(block, -1) {
		names = append(names, m[1])
	}
	inGroup := false
	for _, l := range strings.Split(block, "\n") {
		switch {
		case groupOpenRE.MatchString(strings.TrimRight(l, " \t")):
			inGroup = true
		case inGroup && strings.HasPrefix(l, ")"):
			inGroup = false
		case inGroup:
			if m := memberRE.FindStringSubmatch(l); m != nil {
				names = append(names, m[1])
			}
		}
	}
	return names
}

// unnamedDeps returns the indexes (ascending) of dependency blocks that declare
// no identifier the contract slice mentions.
func unnamedDeps(deps []string, slice []string) []int {
	words := map[string]bool{}
	for _, w := range wordRE.FindAllString(strings.Join(slice, "\n"), -1) {
		words[w] = true
	}
	var out []int
	for i, d := range deps {
		named := false
		for _, name := range declaredNames(d) {
			if name != "" && name != "_" && words[name] {
				named = true
				break
			}
		}
		if !named {
			out = append(out, i)
		}
	}
	return out
}

// stripTestComments returns the source without comments and blank lines. It
// parses with go/parser and prints without comments; ok is false when the
// source does not parse and the line-based fallback was used. Blank lines are
// kept when the source has a raw string (a blank line there is data).
func stripTestComments(src string) (out string, ok bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err == nil {
		var b bytes.Buffer
		if format.Node(&b, fset, f) == nil {
			out = b.String()
			if !strings.Contains(src, "`") {
				out = dropBlank(out, func(l string) bool { return false })
			}
			return out, true
		}
	}
	return dropBlank(src, func(l string) bool { return strings.HasPrefix(l, "//") }), false
}

// dropBlank removes blank lines and the lines extra accepts, except inside a
// raw string (an odd number of backticks opens or closes one).
func dropBlank(src string, extra func(trimmed string) bool) string {
	var keep []string
	inRaw := false
	for _, l := range strings.Split(src, "\n") {
		t := strings.TrimSpace(l)
		if !inRaw && (t == "" || extra(t)) {
			continue
		}
		keep = append(keep, l)
		if strings.Count(l, "`")%2 == 1 {
			inRaw = !inRaw
		}
	}
	return strings.Join(keep, "\n") + "\n"
}
