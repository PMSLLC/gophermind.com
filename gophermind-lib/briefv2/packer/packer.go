// Package packer builds the prompt for one leaf (need-to-know, budgeted, never
// stored) and checks the reply (reply.go). Prompt text is carried only in
// Packed.Text, which the executor's request builder reads; every other view of
// a Packed shows sizes and a SHA-256 only.
package packer

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"text/template"

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
	maxFailureLines = 30
	maxFailureBytes = 2048
)

// Failure is the only thing carried from one attempt to the next, in memory only.
type Failure struct {
	Names []string
	Lines []string
}

// NewFailure keeps at most 8 names, the first 30 non-blank lines of output cut
// to 2048 bytes in total on a line boundary, and drops every name and line that
// contains a non-empty secret value. Secrets are dropped before any cut, so a
// cut cannot split a secret and leave part of it behind.
func NewFailure(names []string, output string, secrets []string) Failure {
	var f Failure
	for _, n := range names {
		if n == "" || hasSecret(n, secrets) {
			continue
		}
		if len(f.Names) == maxFailureNames {
			break
		}
		f.Names = append(f.Names, strings.ToValidUTF8(n, "?"))
	}
	total := 0
	for _, raw := range strings.Split(output, "\n") {
		if len(f.Lines) == maxFailureLines {
			break
		}
		if hasSecret(raw, secrets) {
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
		if total+len(l)+1 > maxFailureBytes {
			break
		}
		total += len(l) + 1
		f.Lines = append(f.Lines, l)
	}
	return f
}

// Empty reports whether there is nothing to feed back.
func (f Failure) Empty() bool { return len(f.Names) == 0 && len(f.Lines) == 0 }

func hasSecret(s string, secrets []string) bool {
	for _, sec := range secrets {
		if sec != "" && strings.Contains(s, sec) {
			return true
		}
	}
	return false
}

func dropSecretLines(in []string, secrets []string) []string {
	var out []string
	for _, s := range in {
		if !hasSecret(s, secrets) {
			out = append(out, s)
		}
	}
	return out
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

// EstimateTokens is ceil(ceil(bytes/4) * 1.1) in integer arithmetic.
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
	return renderTemplate("implement", map[string]string{
		"File": s.n.File, "Package": s.n.Package, "Signature": s.n.Signature,
		"Contract": s.contract, "Deps": deps, "Constraints": cons, "Notes": notes,
		"TestFile": s.n.TestFile, "TestSource": strings.TrimRight(s.testSrc, "\n"), "Failure": failure,
	})
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
	s := &state{
		n: n, contract: strings.Join(slice, "\n\n"), deps: append([]string(nil), n.DependencySignatures...),
		notes:   dropSecretLines(in.Notes, in.Secrets),
		fail:    Failure{Names: dropSecretLines(in.Failure.Names, in.Secrets), Lines: dropSecretLines(in.Failure.Lines, in.Secrets)},
		testSrc: n.TestSource,
	}
	text, err := s.render()
	if err != nil {
		return Packed{}, errors.New("packer: the prompt template failed")
	}
	if hasSecret(text, in.Secrets) {
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
			if len(s.fail.Lines) <= 10 {
				return "", false
			}
			s.fail.Lines = s.fail.Lines[:10]
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

func removeAt(in []string, i int) []string {
	out := make([]string, 0, len(in)-1)
	out = append(out, in[:i]...)
	return append(out, in[i+1:]...)
}

var (
	declFuncRE  = regexp.MustCompile(`(?m)^func\s+(?:\(\s*\w*\s*\*?\s*(\w+)[^)]*\)\s*)?(\w+)`)
	declTypeRE  = regexp.MustCompile(`(?m)^(?:type|var|const)\s+(\w+)`)
	declGroupRE = regexp.MustCompile(`(?m)^\t(\w+)\b`)
)

// declaredNames lists the identifiers one dependency block declares: function
// and method names, receiver types, types, vars, consts and grouped members.
func declaredNames(block string) []string {
	var names []string
	for _, m := range declFuncRE.FindAllStringSubmatch(block, -1) {
		names = append(names, m[1], m[2])
	}
	for _, m := range declTypeRE.FindAllStringSubmatch(block, -1) {
		names = append(names, m[1])
	}
	for _, m := range declGroupRE.FindAllStringSubmatch(block, -1) {
		names = append(names, m[1])
	}
	return names
}

// unnamedDeps returns the indexes (ascending) of dependency blocks that declare
// no identifier the contract slice mentions.
func unnamedDeps(deps []string, slice []string) []int {
	text := strings.Join(slice, "\n")
	var out []int
	for i, d := range deps {
		named := false
		for _, name := range declaredNames(d) {
			if name == "" || name == "_" {
				continue
			}
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(text) {
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

func dropBlank(src string, extra func(trimmed string) bool) string {
	var keep []string
	for _, l := range strings.Split(src, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || extra(t) {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n") + "\n"
}
