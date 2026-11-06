package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

func TestHelloContractKeepsTheFixedArchitecture(t *testing.T) {
	g := newRigFor(t, approving(), hello, helloID)
	g.mustPlan(planner.Options{StopAfter: "contract"})
	c := g.contracts()

	byName := map[string]int{}
	for i, f := range c.Functions {
		sig := strings.TrimPrefix(f.Signature, "func ")
		name := sig[:strings.IndexAny(sig, "(")]
		byName[name] = i
		if strings.Contains(f.Signature, "stdin") {
			t.Errorf("%s signature %q mentions stdin (out of scope)", f.ID, f.Signature)
		}
		if name == "FormatGreeting" {
			t.Errorf("function %s: FormatGreeting must not exist", f.ID)
		}
	}
	for _, comp := range c.Components {
		if comp.ID == "entry-point-main" {
			t.Errorf("component id %q must not exist", comp.ID)
		}
	}

	want := []struct{ name, pkg, sig string }{
		{"Greeting", "internal/greet", "func Greeting(name string) string"},
		{"Run", "internal/cli", "func Run(args []string, stdout, stderr io.Writer) int"},
		{"main", "cmd/hello", "func main()"},
	}
	for _, w := range want {
		i, ok := byName[w.name]
		if !ok {
			t.Errorf("no function named %s", w.name)
			continue
		}
		if f := c.Functions[i]; f.Package != w.pkg || f.Signature != w.sig {
			t.Errorf("%s = %s in %s, want %s in %s", w.name, f.Signature, f.Package, w.sig, w.pkg)
		}
	}

	var mainComp string
	for _, comp := range c.Components {
		if comp.Package == "cmd/hello" {
			mainComp = comp.ID
		}
	}
	if mainComp != "" {
		for _, f := range c.Functions {
			if f.Component == mainComp && !strings.HasPrefix(f.Signature, "func main(") {
				t.Errorf("function %s (%s) is in the cmd/hello component %s; only main belongs there", f.ID, f.Signature, mainComp)
			}
		}
	}

	if r, ok := byName["Run"]; ok {
		if gr, ok := byName["Greeting"]; ok {
			found := false
			for _, u := range c.Functions[r].Uses {
				found = found || u == c.Functions[gr].ID
			}
			if !found {
				t.Errorf("Run uses %v, want it to include Greeting (%s)", c.Functions[r].Uses, c.Functions[gr].ID)
			}
		}
	}

	for _, s := range g.stagesCalled() {
		if s == "contract:outline:entry" {
			t.Errorf("stagesCalled has %q", s)
		}
	}
}
