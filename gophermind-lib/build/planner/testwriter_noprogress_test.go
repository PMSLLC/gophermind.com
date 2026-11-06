package planner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// neverCallsGreet is a test file that compiles and can fail but never calls
// Greet, which the test writer refuses every time.
const neverCallsGreet = `package greet

import "testing"

func TestGreet(t *testing.T) {
	want := "Hello, Ada!"
	if got := "Hello, " + "Ada!"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
`

func TestTestwriterStopsOnNoProgress(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{"testwrite.fn-greet.txt": testwriteReply(t, neverCallsGreet)}))
	out, err := g.plan(planner.Options{})
	if err == nil || out == planner.Done {
		t.Fatal("a reply refused twice the same way was accepted")
	}
	const want = "testwriter_no_progress: node fn-greet: test file never calls Greet"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Run error = %v, want it to carry %q", err, want)
	}
	if n := count(g.stagesCalled(), "testwrite:fn-greet"); n != 2 {
		t.Errorf("testwrite:fn-greet calls = %d, want 2 (stop after two identical refusals): %v", n, g.stagesCalled())
	}
	raw, rerr := os.ReadFile(filepath.Join(g.runDir, "_state", "planner_error.json"))
	if rerr != nil || !strings.Contains(string(raw), want) {
		t.Errorf("planner_error.json = %s, %v; want it to carry %q", raw, rerr, want)
	}
}
