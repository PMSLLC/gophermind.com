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
	var contract, clarify, other, testwrite int
	for _, rq := range g.fake.Requests() {
		system := rq.Messages[0].Content
		switch {
		case strings.Contains(system, "testwrite:"):
			contract++
			testwrite++
			if rq.JSON || len(rq.Schema) != 0 {
				t.Errorf("a test-writer request (its reply is JSON then a Go file) asked for JSON mode or sent a schema: %q", system)
			}
		case strings.HasSuffix(system, "Stage: coverage"):
			contract++
			if !strings.Contains(string(rq.Schema), "root_tests") {
				t.Errorf("a coverage request sent no schema naming \"root_tests\": %q", system)
			}
			if !rq.JSON {
				t.Errorf("a coverage request (its reply is one object) did not ask for JSON mode: %q", system)
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
	if testwrite == 0 {
		t.Fatal("the fixture plan made no test-writer requests, so the test-writer check proved nothing")
	}
	if contract == 0 || clarify == 0 {
		t.Fatalf("the fixture plan made %d contract and %d clarify requests, want some of each (%d others)", contract, clarify, other)
	}
}
