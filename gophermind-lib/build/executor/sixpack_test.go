package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/runner"
)

// sixFn is one of the four functions the six-leaf fixture adds to the
// greeter's greet package (ruling 18).
type sixFn struct{ id, name, word string }

var sixNew = []sixFn{
	{"fn-cheer", "Cheer", "Hooray"},
	{"fn-shout", "Shout", "Hey"},
	{"fn-thank", "Thank", "Thank you"},
	{"fn-welcome", "Welcome", "Welcome"},
}

// sixIDs is the six independent leaves of the greet level, in id order.
var sixIDs = []string{"fn-cheer", "fn-farewell", "fn-greet", "fn-shout", "fn-thank", "fn-welcome"}

const sixImpl = `package greet

import "strings"

// %[1]s trims the name and returns %[2]s, <name>!. An empty name is an error.
func %[1]s(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &NameError{Reason: "name is empty"}
	}
	return "%[2]s, " + name + "!", nil
}
`

const sixTest = `package greet

import "testing"

func Test%[1]s(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"builds the message", "Ada", "%[2]s, Ada!", false},
		{"trims the name", "  Ada  ", "%[2]s, Ada!", false},
		{"refuses an empty name", "   ", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := %[1]s(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("%[1]s(%%q) error = %%v, wantErr %%v", c.in, err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("%[1]s(%%q) = %%q, want %%q", c.in, got, c.want)
			}
		})
	}
}
`

// sixGood is the passing reply of a leaf of the six-leaf plan.
func sixGood(id string) string {
	for _, f := range sixNew {
		if f.id == id {
			return fmt.Sprintf(sixImpl, f.name, f.word)
		}
	}
	return good(id)
}

func readFixtureJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
}

func writeFixtureJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func cloneJSON(t *testing.T, n map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(n)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// sixpackPlanner writes the planner fixture of the six-leaf level into a new
// directory searched before testdata/greeter/planner: the greet component's
// decomposition, contract and enrichment with four functions cloned from
// fn-greet, their Test-writer replies, and a coverage map that names them.
func sixpackPlanner(t *testing.T) string {
	t.Helper()
	src, dir := filepath.Join(greeterDir, "planner"), t.TempDir()
	find := func(list []map[string]any, id string) map[string]any {
		for _, n := range list {
			if n["id"] == id {
				return n
			}
		}
		t.Fatalf("the greeter fixture has no %s", id)
		return nil
	}

	var dec []map[string]any
	readFixtureJSON(t, filepath.Join(src, "decompose.greet.txt"), &dec)
	baseDec := find(dec, "fn-greet")
	for _, f := range sixNew {
		n := cloneJSON(t, baseDec)
		n["id"] = f.id
		n["title"] = f.name + " a name"
		n["description"] = "Build the " + strings.ToLower(f.name) + " message for a name, refusing an empty one."
		outs := n["contract"].(map[string]any)["outputs"].([]any)
		outs[0].(map[string]any)["description"] = f.word + ", <name>! with the name trimmed"
		dec = append(dec, n)
	}
	writeFixtureJSON(t, filepath.Join(dir, "decompose.greet.txt"), dec)

	var con map[string]any
	readFixtureJSON(t, filepath.Join(src, "contract.greet.txt"), &con)
	fns := con["functions"].([]any)
	for _, f := range sixNew {
		fns = append(fns, map[string]any{
			"id": f.id, "package": "greet", "file": "internal/greet/" + strings.ToLower(f.name) + ".go",
			"signature": "func " + f.name + "(name string) (string, error)",
			"doc":       f.name + " trims the name and returns " + f.word + ", <name>!. An empty name is an error.",
			"uses":      []any{"name-error"},
		})
	}
	con["functions"] = fns
	writeFixtureJSON(t, filepath.Join(dir, "contract.greet.txt"), con)

	var enr []map[string]any
	readFixtureJSON(t, filepath.Join(src, "enrich.greet.txt"), &enr)
	baseEnr := find(enr, "fn-greet")
	for _, f := range sixNew {
		n := cloneJSON(t, baseEnr)
		n["id"] = f.id
		n["rationale"] = "Serves the " + f.id + " part of the brief so callers get the behaviour the contract promises."
		enr = append(enr, n)
	}
	writeFixtureJSON(t, filepath.Join(dir, "enrich.greet.txt"), enr)

	var cov map[string]any
	readFixtureJSON(t, filepath.Join(src, "coverage.txt"), &cov)
	for _, m := range cov["map"].([]any) {
		e := m.(map[string]any)
		if e["requirement"] == "F1" || e["requirement"] == "C2" {
			nodes := e["nodes"].([]any)
			for _, f := range sixNew {
				nodes = append(nodes, f.id)
			}
			e["nodes"] = nodes
		}
	}
	writeFixtureJSON(t, filepath.Join(dir, "coverage.txt"), cov)

	for _, f := range sixNew {
		writeFixtureJSON(t, filepath.Join(dir, "testwrite."+f.id+".txt"), map[string]any{
			"tests": []any{
				map[string]any{"name": "builds the message", "polarity": "success", "covers": "happy", "given": `"Ada"`, "expect": f.word + ", Ada! and a nil error"},
				map[string]any{"name": "trims the name", "polarity": "boundary", "covers": "input:name", "given": `"  Ada  "`, "expect": f.word + ", Ada! and a nil error"},
				map[string]any{"name": "refuses an empty name", "polarity": "negative", "covers": "error:1", "given": `"   "`, "expect": "an empty string and a *NameError"},
			},
			"test_file": fmt.Sprintf(sixTest, f.name, f.word),
		})
	}
	return dir
}

// sixpack is the rig of the six-leaf level with executor.workers set.
func sixpack(t *testing.T, workers int) func(*rigOpts) {
	dir := sixpackPlanner(t)
	return func(o *rigOpts) {
		o.PlannerDirs = append([]string{dir}, o.PlannerDirs...)
		workersN(workers)(o)
	}
}

// sixScript has one passing reply per leaf of the plan.
func sixScript(g *rig) Script {
	s := Script{}
	for _, l := range g.plan.Leaves {
		s["implement:"+l.ID] = []step{reply(sixGood(l.ID))}
	}
	return s
}

// sixChecker passes every check: three verdicts per leaf cover a private pass,
// a landing and a spare; the repository-wide checks pass.
func (g *rig) sixChecker() *fakeChecker {
	fc := g.fastChecker()
	for _, l := range g.plan.Leaves {
		fc.LeafScript[l.ID] = []runner.Verdict{passVerdict(), passVerdict(), passVerdict()}
	}
	return fc
}

func TestSixpackPlansSixIndependentLeavesInOneLevel(t *testing.T) {
	t.Parallel()
	g := newRig(t, sixpack(t, 3))
	wave := g.plan.Leaf("fn-greet").Wave
	if len(levels(g.leaves(sixIDs...))) != 1 {
		t.Fatal("the six leaves are not one level")
	}
	for _, id := range sixIDs {
		if g.plan.Leaf(id) == nil || g.plan.Leaf(id).Wave != wave || g.plan.Leaf(id).Package != "greet" {
			t.Fatalf("%s is not a greet leaf of wave %d: %+v", id, wave, g.plan.Leaf(id))
		}
	}
}
