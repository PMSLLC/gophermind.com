package planner

import (
	"fmt"
	"strings"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
	"gophermind/gophermind-lib/briefv2/brief"
)

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

// qualityOptions are the names a root test command may use as variables.
func qualityOptions(f brief.Frontmatter) acceptcheck.Options {
	var o acceptcheck.Options
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
	for _, t := range tests {
		for _, f := range acceptcheck.Quality(t.Command, o) {
			if by[t.Requirement] == nil {
				by[t.Requirement] = map[string]bool{}
			}
			by[t.Requirement][string(f)] = true
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
