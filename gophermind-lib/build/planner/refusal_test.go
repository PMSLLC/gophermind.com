package planner_test

import (
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/events"
	"gophermind/gophermind-lib/build/planner"
)

// A refused reply leaves a refusal event and a line in _state/refusals.jsonl
// with the planner's own wording, never the reply.
func TestARefusedTestFileIsRecordedAsARefusal(t *testing.T) {
	good := string(mustRead(t, filepath.Join(greeter, "testwrite.fn-greet.txt")))
	one := `{"tests": [{"name": "SECRETMARKER", "given": "Ada", "expect": "Hello, Ada!"}], "test_file": "package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {}\n"}`
	g := newRig(t, approving(), variant(t, map[string]string{"testwrite.fn-greet.txt": one, "testwrite.fn-greet.2.txt": good}))
	g.mustPlan(planner.Options{})

	got := planner.ReadRefusals(g.runDir)
	if len(got) != 1 {
		t.Fatalf("refusals = %+v, want exactly the one refused reply", got)
	}
	r := got[0]
	if r.Stage != "testwrite:fn-greet" || r.NodeID != "fn-greet" || r.Round != 1 || r.Text == "" {
		t.Errorf("refusal = %+v", r)
	}
	if strings.Contains(r.Text, "SECRETMARKER") || strings.ContainsAny(r.Text, "\n\r") {
		t.Errorf("refusal text carries reply text or a newline: %q", r.Text)
	}
	ev := g.sink.OfKind(events.KindRefusal)
	if len(ev) != 1 || ev[0].Stage != r.Stage || ev[0].NodeID != "fn-greet" || ev[0].Message != r.Text {
		t.Errorf("refusal events = %+v, want one matching the record", ev)
	}
}

func TestAMalformedClarifyReplyIsRecordedWithoutItsText(t *testing.T) {
	g := newRig(t, approving(), variant(t, map[string]string{
		"clarify.txt":   "I have some thoughts but no JSON.",
		"clarify.2.txt": "[]",
	}))
	g.mustPlan(planner.Options{StopAfter: "clarify"})
	got := planner.ReadRefusals(g.runDir)
	if len(got) != 1 || got[0].NodeID != "" || strings.Contains(got[0].Text, "thoughts") {
		t.Errorf("refusals = %+v", got)
	}
}
