package planner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// requirementIDsFor is the requirements a function serves: the ones whose
// coverage names the function, else the ones whose coverage names its
// component. A function that neither names gets none; the approval summary
// lists it as a warning.
func requirementIDsFor(cov CoverageFile, nodeID, parentID string) []string {
	own, viaParent := map[string]bool{}, map[string]bool{}
	for _, c := range cov.Covered {
		for _, n := range c.Nodes {
			switch n {
			case nodeID:
				own[c.Requirement] = true
			case parentID:
				viaParent[c.Requirement] = true
			}
		}
	}
	set := own
	if len(set) == 0 {
		set = viaParent
	}
	return sortedKeys(set)
}

// allRequirementIDs is every requirement id in the coverage, sorted.
func allRequirementIDs(cov CoverageFile) []string {
	out := make([]string, 0, len(cov.Covered))
	for _, c := range cov.Covered {
		out = append(out, c.Requirement)
	}
	sort.Strings(out)
	return out
}

// hookList returns profile_hooks when it is a real list; a not-applicable
// answer is no hooks.
func hookList(d map[string]any) []string {
	if _, na := notApplicable(d["profile_hooks"]); na {
		return nil
	}
	return strList(d["profile_hooks"])
}

// testwriterView is the part of a node the Test-writer sees. A node-scope call
// may be shown to a public provider, so it never carries the reasoning the
// groups hold (rationale, alternatives, refactor notes, assumptions, open
// questions, decisions): only the contract, its context and the hooks.
func testwriterView(d map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"id", "title", "description", "contract", "context"} {
		if v, ok := d[k]; ok {
			out[k] = v
		}
	}
	if hooks := hookList(d); len(hooks) > 0 {
		out["profile_hooks"] = hooks
	}
	return out
}

var (
	polarities = []string{"success", "negative", "boundary", "property"}
	coversRE   = regexp.MustCompile(`^(happy|error:[0-9]+|input:[A-Za-z_][A-Za-z0-9_]*)$`)
)

// checkPolarity applies the coverage rules to a Test-writer reply: every test
// has a polarity and a covers; there is a success test for the happy path; and
// each error condition has a negative test that covers it. Every refusal starts
// with the fixed defect word and quotes nothing from the reply.
func checkPolarity(tests []map[string]any, errors int) error {
	happy := false
	covered := map[int]bool{}
	for i, t := range tests {
		pol, _ := t["polarity"].(string)
		cov, _ := t["covers"].(string)
		if !contains(polarities, pol) {
			return fmt.Errorf("%s: test %d needs a polarity: success, negative, boundary or property", grpEnum, i+1)
		}
		if !coversRE.MatchString(cov) {
			return fmt.Errorf("%s: test %d needs covers: happy, error:<n> or input:<name>", grpEnum, i+1)
		}
		if cov == "happy" && pol == "success" {
			happy = true
		}
		var n int
		if _, err := fmt.Sscanf(cov, "error:%d", &n); err == nil {
			if pol != "negative" {
				return fmt.Errorf("%s: test %d covers an error condition, so its polarity must be negative", grpPolarity, i+1)
			}
			covered[n] = true
		}
	}
	if !happy {
		return fmt.Errorf("%s: no success test covers the happy path", grpPolarity)
	}
	for n := 1; n <= errors; n++ {
		if !covered[n] {
			return fmt.Errorf("%s: error condition %d has no negative test (covers error:%d)", grpErrTest, n, n)
		}
	}
	return nil
}

// hasBenchmark reports whether src defines Benchmark<Name> for the test
// function funcName (TestRegister gives BenchmarkRegister).
func hasBenchmark(src, funcName string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	want := "Benchmark" + strings.TrimPrefix(funcName, "Test")
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == want {
			return true
		}
	}
	return false
}

// linkErrorTests writes, into each error of the contract, the name of the test
// that covers it. A model never writes the link: it is read from covers.
func linkErrorTests(ct map[string]any, tests []map[string]any) error {
	for i, e := range objects(ct["errors"]) {
		want := fmt.Sprintf("error:%d", i+1)
		found := false
		for _, t := range tests {
			if cov, _ := t["covers"].(string); cov == want {
				if name, _ := t["name"].(string); name != "" {
					e["test"] = name
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("%s: error condition %d has no covering test", grpErrTest, i+1)
		}
	}
	return nil
}

// finishGroupDefects checks the finished tree documents: the decisions
// embedded in the root and in every other node must match the question store.
// It names nodes and kinds only.
func finishGroupDefects(docs []map[string]any, qs qstore) []string {
	var root map[string]any
	var others []map[string]any
	for _, d := range docs {
		if d["kind"] == "root" {
			root = d
			continue
		}
		others = append(others, d)
	}
	var out []string
	for _, p := range checkEmbedded(qs, root, others) {
		out = append(out, grpDecisionsEmbed+": "+p)
	}
	return out
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
