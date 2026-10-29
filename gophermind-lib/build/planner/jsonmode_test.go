package planner_test

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/planner"
)

// Only the stages whose reply is one JSON object ask for JSON mode. A stage
// whose reply is a list (clarify, decompose) must not: json_object mode makes a
// server wrap a list in an object, and that would break the parse.
func TestOnlyObjectReplyStagesAskForJSONMode(t *testing.T) {
	g := newRig(t, approving())
	g.mustPlan(planner.Options{})
	var contract, clarify, other int
	for _, rq := range g.fake.Requests() {
		system := rq.Messages[0].Content
		switch {
		case strings.Contains(system, "testwrite:"), strings.HasSuffix(system, "Stage: coverage"):
			contract++
			want := "root_tests"
			if strings.Contains(system, "testwrite:") {
				want = "test_file"
			}
			if !strings.Contains(string(rq.Schema), want) {
				t.Errorf("a request that knows its reply shape sent no schema naming %q: %q", want, system)
			}
			if !rq.JSON {
				t.Errorf("a test-writer or coverage request (its reply is one object) did not ask for JSON mode: %q", system)
			}
		case strings.Contains(system, "contract:"):
			contract++
			if !rq.JSON {
				t.Errorf("a contract request did not ask for JSON mode: %q", system)
			}
		case strings.Contains(system, "clarify"):
			clarify++
			if rq.JSON {
				t.Errorf("a clarify request (its reply is a list) asked for JSON mode: %q", system)
			}
		default:
			other++
		}
	}
	if contract == 0 || clarify == 0 {
		t.Fatalf("the fixture plan made %d contract and %d clarify requests, want some of each (%d others)", contract, clarify, other)
	}
}
