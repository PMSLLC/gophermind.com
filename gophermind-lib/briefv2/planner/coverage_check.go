package planner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/tree"
)

// PlanNode is what the coverage check needs to know about one node of a plan.
type PlanNode struct {
	ID     string    `json:"id"`
	Kind   tree.Kind `json:"kind"`
	Parent string    `json:"parent,omitempty"`
	Title  string    `json:"title"`
	File   string    `json:"file,omitempty"` // contract.file, function nodes only
}

// MapEntry names the nodes whose code satisfies one requirement.
type MapEntry struct {
	Requirement string   `json:"requirement"`
	Nodes       []string `json:"nodes"`
}

// RootTest is an acceptance test for the root node, proving one requirement.
type RootTest struct {
	Requirement string `json:"requirement"`
	Name        string `json:"name"`
	Given       string `json:"given"`
	Expect      string `json:"expect"`
	Command     string `json:"command"`
}

// CoverageReply is the model's proposed mapping from requirements to the plan.
type CoverageReply struct {
	Map       []MapEntry `json:"map"`
	RootTests []RootTest `json:"root_tests"`
	// Serve, when present, says how the executor starts the built server once
	// for the Go acceptance tests (see acceptance_go.go).
	Serve *Serve `json:"serve,omitempty"`
}

// Gap is a requirement nothing in the plan covers, and why.
type Gap struct {
	Requirement string `json:"requirement"`
	Line        int    `json:"line"`
	Text        string `json:"text"`
	Reason      string `json:"reason"`
}

// Covered is a requirement with what covers it.
type Covered struct {
	Requirement string   `json:"requirement"`
	Nodes       []string `json:"nodes"`
	RootTests   []string `json:"root_tests"` // test names
}

// ParseCoverageReply decodes a reply that has already had fences and prose
// stripped. An error means the reply is malformed: invalid JSON, a map entry
// or root test naming a requirement the brief does not have, or a root test
// with no name or no command.
func ParseCoverageReply(text string, reqs []Requirement) (CoverageReply, error) {
	var r CoverageReply
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		return CoverageReply{}, fmt.Errorf("coverage reply is not valid JSON (%s)", jsonErr(err))
	}
	known := map[string]bool{}
	for _, q := range reqs {
		known[q.ID] = true
	}
	for _, m := range r.Map {
		if !known[m.Requirement] {
			return CoverageReply{}, fmt.Errorf("coverage reply: map names unknown requirement (%d bytes)", len(m.Requirement))
		}
	}
	if r.Serve != nil {
		if err := r.Serve.check(); err != nil {
			return CoverageReply{}, err
		}
	}
	for _, t := range r.RootTests {
		if !known[t.Requirement] {
			return CoverageReply{}, fmt.Errorf("coverage reply: root test names unknown requirement (%d bytes)", len(t.Requirement))
		}
		if strings.TrimSpace(t.Name) == "" {
			return CoverageReply{}, fmt.Errorf("coverage reply: a root test for %s has no name", t.Requirement)
		}
		if strings.TrimSpace(t.Command) == "" {
			return CoverageReply{}, fmt.Errorf("coverage reply: root test for %s has no command", t.Requirement)
		}
	}
	return r, nil
}

// Merge returns r with other's entries added. Node lists for the same
// requirement are unioned, keeping order; a root test is appended unless one
// with the same requirement and name is already there.
func (r CoverageReply) Merge(other CoverageReply) CoverageReply {
	out := CoverageReply{Map: []MapEntry{}, RootTests: []RootTest{}, Serve: r.Serve}
	if out.Serve == nil {
		out.Serve = other.Serve
	}
	at := map[string]int{}
	add := func(m MapEntry) {
		i, ok := at[m.Requirement]
		if !ok {
			at[m.Requirement] = len(out.Map)
			out.Map = append(out.Map, MapEntry{Requirement: m.Requirement, Nodes: []string{}})
			i = len(out.Map) - 1
		}
		for _, n := range m.Nodes {
			if !contains(out.Map[i].Nodes, n) {
				out.Map[i].Nodes = append(out.Map[i].Nodes, n)
			}
		}
	}
	for _, m := range r.Map {
		add(m)
	}
	for _, m := range other.Map {
		add(m)
	}
	seen := map[string]bool{}
	for _, t := range append(append([]RootTest{}, r.RootTests...), other.RootTests...) {
		k := t.Requirement + "\x00" + t.Name
		if !seen[k] {
			seen[k] = true
			out.RootTests = append(out.RootTests, t)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// CheckCoverage decides, without a model, which requirements the plan covers.
// A covering node is a function node, or a component node with at least one
// function node under it; the root node and unknown ids cover nothing. A
// constraint needs a covering node or a root test, a feature needs a covering
// node, and an acceptance bullet needs a root test. covered and gaps are both
// in reqs order, and every requirement is in exactly one of them.
func CheckCoverage(reqs []Requirement, nodes []PlanNode, reply CoverageReply) (covered []Covered, gaps []Gap) {
	byID := map[string]PlanNode{}
	hasFunction := map[string]bool{}
	for _, n := range nodes {
		byID[n.ID] = n
		if n.Kind == tree.KindFunction {
			hasFunction[n.Parent] = true
		}
	}
	mapped := map[string]bool{}
	named := map[string][]string{}
	for _, m := range reply.Map {
		mapped[m.Requirement] = true
		for _, id := range m.Nodes {
			if !contains(named[m.Requirement], id) {
				named[m.Requirement] = append(named[m.Requirement], id)
			}
		}
	}
	tests := map[string][]string{}
	for _, t := range reply.RootTests {
		tests[t.Requirement] = append(tests[t.Requirement], t.Name)
	}

	covered, gaps = []Covered{}, []Gap{}
	for _, q := range reqs {
		valid := []string{}
		var why []string
		for _, id := range named[q.ID] {
			n, ok := byID[id]
			switch {
			case !ok:
				why = append(why, "names unknown node "+id)
			case n.Kind == tree.KindRoot:
				why = append(why, "the root node alone covers nothing")
			case n.Kind == tree.KindComponent && !hasFunction[id]:
				why = append(why, "component "+id+" has no functions")
			default:
				valid = append(valid, id)
			}
		}
		names := tests[q.ID]
		if names == nil {
			names = []string{}
		}
		missing := ""
		switch q.Kind {
		case ReqAcceptance:
			if len(names) == 0 {
				missing = "acceptance bullet has no root test"
			}
		case ReqFeature:
			if len(valid) == 0 {
				missing = "no covering node"
			}
		default:
			if len(valid) == 0 && len(names) == 0 {
				missing = "no node and no root test"
			}
		}
		if missing == "" {
			covered = append(covered, Covered{Requirement: q.ID, Nodes: valid, RootTests: names})
			continue
		}
		if q.Kind != ReqAcceptance && !mapped[q.ID] && len(names) == 0 {
			missing = "not mapped"
		}
		gaps = append(gaps, Gap{Requirement: q.ID, Line: q.Line, Text: q.Text, Reason: strings.Join(append(why, missing), "; ")})
	}
	return covered, gaps
}

var (
	backtickRE = regexp.MustCompile("`([^`\n]+)`")
	pathRE     = regexp.MustCompile(`^(cmd|internal)/[A-Za-z0-9_./-]+$`)
)

// PathWarnings lists each backticked cmd/ or internal/ path in the brief that
// no function node's file sits under. A plan that builds cmd/server when the
// brief names cmd/venture-server shows up here.
func PathWarnings(briefSrc []byte, nodes []PlanNode) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range backtickRE.FindAllSubmatch(briefSrc, -1) {
		tok := string(m[1])
		if !pathRE.MatchString(tok) || seen[tok] {
			continue
		}
		seen[tok] = true
		dir := strings.TrimRight(tok, "/")
		found := false
		for _, n := range nodes {
			if n.Kind == tree.KindFunction && (n.File == dir || strings.HasPrefix(n.File, dir+"/")) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, "`"+tok+"` is named in the brief but no function file is under it")
		}
	}
	return out
}

// CommandWarnings lists each acceptance requirement that begins with a
// backticked command none of its root tests runs. A requirement with no root
// test at all is a gap, not a warning, and is skipped here.
func CommandWarnings(reqs []Requirement, reply CoverageReply) []string {
	out := []string{}
	for _, q := range reqs {
		if q.Kind != ReqAcceptance || !strings.HasPrefix(q.Text, "`") {
			continue
		}
		end := strings.Index(q.Text[1:], "`")
		if end < 0 {
			continue
		}
		span := q.Text[1 : 1+end]
		has, runs := false, false
		for _, t := range reply.RootTests {
			if t.Requirement == q.ID {
				has = true
				if strings.Contains(t.Command, span) {
					runs = true
				}
			}
		}
		if has && !runs {
			out = append(out, fmt.Sprintf("%s: no root test runs the command the bullet begins with (`%s`)", q.ID, span))
		}
	}
	return out
}

// StrayCommandWarnings lists each cmd/<name> directory that holds plan files
// although the brief never names it. A plan that spreads its entry point over
// cmd/server and cmd/fake-llm when the brief names one binary shows up here.
func StrayCommandWarnings(briefSrc []byte, nodes []PlanNode) []string {
	named := map[string]bool{}
	for _, m := range backtickRE.FindAllSubmatch(briefSrc, -1) {
		parts := strings.Split(string(m[1]), "/")
		if len(parts) >= 2 && parts[0] == "cmd" && parts[1] != "" {
			named[parts[1]] = true
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, n := range nodes {
		parts := strings.Split(n.File, "/")
		if n.Kind != tree.KindFunction || len(parts) < 3 || parts[0] != "cmd" {
			continue
		}
		if name := parts[1]; !named[name] && !seen[name] {
			seen[name] = true
			out = append(out, "`cmd/"+name+"` holds plan files but the brief never names it")
		}
	}
	return out
}
