package planner

import (
	"fmt"
	"path"
	"strings"

	"gophermind/gophermind-lib/build/acceptcheck"
	"gophermind/gophermind-lib/build/brief"
	"gophermind/gophermind-lib/build/contract"
)

// FindVacuous is the finding of an acceptance root test the executor would
// refuse with acceptance_vacuous: it never runs a binary the harness builds.
const FindVacuous = "acceptance_vacuous"

// BinName is the name of the binary the harness builds for a main package
// whose package clause is clause and whose slash directory is dir: the first
// directory under cmd/. The executor and the planner share this rule.
func BinName(clause, dir string) (string, bool) {
	if clause != "main" {
		return "", false
	}
	rest, ok := strings.CutPrefix(dir, "cmd/")
	if !ok {
		return "", false
	}
	name := strings.SplitN(rest, "/", 2)[0]
	return name, name != ""
}

// FileDir is the slash directory a contract function's file lives in. A file
// with a directory part ("cmd/x/main.go") names it; a bare file name
// ("main.go") sits in the package directory, which is the contract package
// path. The executor and the planner share this rule.
func FileDir(pkg, file string) string {
	if strings.Contains(file, "/") {
		return path.Dir(file)
	}
	if pkg == "" {
		return "."
	}
	return path.Clean(pkg)
}

// contractBins are the binary names the contract's functions build, sorted.
func contractBins(c *contract.Contracts) []string {
	if c == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range c.Functions {
		if name, ok := BinName(PackageClause(f.Package), FileDirIn(c.Module, f.Package, f.File)); ok && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sortStrings(out)
	return out
}

// maxQualityNamed is how many requirement ids a quality stop names.
const maxQualityNamed = 10

// QualityItem is one requirement whose root test fails the quality gate, with
// the fixed-vocabulary finding names (internal/acceptcheck), never the command.
type QualityItem struct {
	Requirement string
	Findings    []string
}

// QualityError stops a run whose root tests are still weak after every fill
// round: a command that cannot fail, or that hides its failure, proves nothing.
// Nothing is approved in that state.
type QualityError struct{ Items []QualityItem }

func (e *QualityError) Error() string {
	var parts []string
	for i, it := range e.Items {
		if i == maxQualityNamed {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", it.Requirement, strings.Join(it.Findings, ", ")))
	}
	msg := fmt.Sprintf("%d root test(s) fail the quality gate, so the plan is not approved: %s", len(e.Items), strings.Join(parts, ", "))
	if len(e.Items) > maxQualityNamed {
		msg += fmt.Sprintf(" and %d more", len(e.Items)-maxQualityNamed)
	}
	return msg
}

// qualityOptions are the names a root test command may use as variables and
// the binaries the contract builds (c may be nil: no binary).
func qualityOptions(f brief.Frontmatter, c *contract.Contracts) acceptcheck.Options {
	o := acceptcheck.Options{Bins: contractBins(c)}
	for _, s := range f.Secrets {
		o.Env = append(o.Env, s.Name)
	}
	for _, e := range f.Env {
		o.Env = append(o.Env, e.Name)
	}
	return o
}

// rootTestDefects is the requirements, in brief order, with a root test that
// fails the quality level of the shared command checker. A requirement with
// several root tests collects the findings of all of them.
func rootTestDefects(reqs []Requirement, tests []RootTest, o acceptcheck.Options) []QualityItem {
	by := map[string]map[string]bool{}
	add := func(id, finding string) {
		if by[id] == nil {
			by[id] = map[string]bool{}
		}
		by[id][finding] = true
	}
	kind := map[string]ReqKind{}
	for _, q := range reqs {
		kind[q.ID] = q.Kind
	}
	for _, t := range tests {
		for _, f := range acceptcheck.Quality(t.Command, o) {
			add(t.Requirement, string(f))
		}
		// Only an acceptance bullet is held to the executor's vacuity rule.
		if kind[t.Requirement] == ReqAcceptance && acceptcheck.Vacuous(t.Command, o.Bins) {
			add(t.Requirement, FindVacuous)
		}
	}
	var out []QualityItem
	for _, q := range reqs {
		set := by[q.ID]
		if len(set) == 0 {
			continue
		}
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sortStrings(names)
		out = append(out, QualityItem{Requirement: q.ID, Findings: names})
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// mergeFill adds a fill reply to the reply so far. A root test the quality
// gate flagged is dropped for each requirement the fill supplies a new root
// test for, so the replacement takes its place; a flagged test the fill leaves
// alone stays, and is flagged again.
func mergeFill(reply, fill CoverageReply, weak []QualityItem) CoverageReply {
	replaced := map[string]bool{}
	for _, t := range fill.RootTests {
		replaced[t.Requirement] = true
	}
	drop := map[string]bool{}
	for _, w := range weak {
		if replaced[w.Requirement] {
			drop[w.Requirement] = true
		}
	}
	kept := CoverageReply{Map: reply.Map, Serve: reply.Serve, RootTests: []RootTest{}}
	for _, t := range reply.RootTests {
		if !drop[t.Requirement] {
			kept.RootTests = append(kept.RootTests, t)
		}
	}
	return kept.Merge(fill)
}

// weakText is the fill prompt's list of weak root tests: ids and finding names.
func weakText(items []QualityItem) string {
	if len(items) == 0 {
		return "none"
	}
	var b strings.Builder
	for i, it := range items {
		if i == 40 {
			fmt.Fprintf(&b, "(%d more)\n", len(items)-40)
			break
		}
		fmt.Fprintf(&b, "%s: %s\n", it.Requirement, strings.Join(it.Findings, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// qualityCell is the quality column of the approval summary.
func qualityCell(items map[string][]string, id string) string {
	if f := items[id]; len(f) > 0 {
		return strings.Join(f, ", ")
	}
	return "ok"
}
