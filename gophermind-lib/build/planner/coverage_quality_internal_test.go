package planner

import (
	"reflect"
	"testing"
)

// A constraint that names a tool in backticks is tested by running that tool.
func TestToolsNotInvoked(t *testing.T) {
	c := Requirement{ID: "C5", Kind: ReqConstraint, Text: "`gofmt` reports no formatting changes and `go vet ./...` reports no problems."}
	rt := func(id string, cmds ...string) []RootTest {
		var out []RootTest
		for _, cmd := range cmds {
			out = append(out, RootTest{Requirement: id, Command: cmd})
		}
		return out
	}
	if got := toolsNotInvoked(c, rt("C5", "hello | grep -q x")); !reflect.DeepEqual(got, []string{"gofmt", "go vet"}) {
		t.Errorf("binary test = %q, want [gofmt go vet]", got)
	}
	if got := toolsNotInvoked(c, rt("C5", `test -z "$(gofmt -l .)"`, "go vet ./...")); got != nil {
		t.Errorf("tools run = %q, want nil", got)
	}
	if got := toolsNotInvoked(c, append(rt("C5", "hello | grep -q x"), rt("C6", `test -z "$(gofmt -l .)" && go vet ./...`)...)); !reflect.DeepEqual(got, []string{"gofmt", "go vet"}) {
		t.Errorf("another requirement's test counted: %q", got)
	}
	plain := Requirement{ID: "C1", Kind: ReqConstraint, Text: "Standard library only."}
	if got := toolsNotInvoked(plain, rt("C1", "hello | grep -q x")); got != nil {
		t.Errorf("no backticked tool = %q, want nil", got)
	}
	a := Requirement{ID: "A2", Kind: ReqAcceptance, Text: "`go vet ./...` reports nothing."}
	if got := toolsNotInvoked(a, rt("A2", "hello | grep -q x")); got != nil {
		t.Errorf("acceptance checked = %q, want nil", got)
	}
}
