package projectrun

import (
	"reflect"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func att(rev, order int, v blackboard.Verdict) blackboard.Attempt {
	return blackboard.Attempt{Provider: "p", Model: "m", Revision: rev, Order: order, Verdict: v}
}

func TestClassStatsAggregatesRowsClassesAndCalls(t *testing.T) {
	rows := []blackboard.Row{
		// pure: first counted attempt passes (an error attempt before it is skipped)
		{NodeID: "a/f1", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{
			att(1, 1, blackboard.VerdictError), att(1, 2, blackboard.VerdictPass)}},
		// pure: fails first (revision 1), passes in revision 2, listed out of order
		{NodeID: "a/f2", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{
			att(2, 1, blackboard.VerdictPass), att(1, 1, blackboard.VerdictFail)}},
		// handler: failed, one failing attempt
		{NodeID: "a/f3", Status: blackboard.StatusFailed, Attempts: []blackboard.Attempt{att(1, 1, blackboard.VerdictFail)}},
		// handler: never started
		{NodeID: "a/f4", Status: blackboard.StatusPending},
		// handler: escalated
		{NodeID: "a/f5", Status: blackboard.StatusEscalated, Attempts: []blackboard.Attempt{
			att(1, 1, blackboard.VerdictFail), att(2, 1, blackboard.VerdictFail)}},
		// id not in the class map
		{NodeID: "a/f6", Status: blackboard.StatusReady},
		// class off the list
		{NodeID: "a/f7", Status: blackboard.StatusVerified, Attempts: []blackboard.Attempt{att(1, 1, blackboard.VerdictPass)}},
	}
	classes := map[string]string{
		"a/f1": "pure", "a/f2": "pure", "a/f3": "handler", "a/f4": "handler", "a/f5": "handler",
		"a/f7": "mystery",
	}
	calls := []ledger.Call{
		{NodeClass: "pure", PromptTokens: 10, CompletionTokens: 5},
		{NodeClass: "pure", PromptTokens: 20, CompletionTokens: 1},
		{NodeClass: "handler", PromptTokens: 7, CompletionTokens: 3},
		{NodeClass: "storage", PromptTokens: 99, CompletionTokens: 99}, // class with no rows: folded into unclassified
	}
	got := ClassStats(rows, classes, calls)
	want := []ClassStat{
		{Class: "pure", Leaves: 2, Verified: 2, Attempts: 3, Passes: 2, FirstTryWins: 1, FirstPassRate: 0.5, Calls: 2, PromptTokens: 30, CompletionTokens: 6},
		{Class: "handler", Leaves: 3, Failed: 1, Escalated: 1, NotRun: 1, Attempts: 3, FirstPassRate: 0, Calls: 1, PromptTokens: 7, CompletionTokens: 3},
		{Class: "unclassified", Leaves: 2, Verified: 1, NotRun: 1, Attempts: 1, Passes: 1, FirstTryWins: 1, FirstPassRate: 0.5, Calls: 1, PromptTokens: 99, CompletionTokens: 99},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	sum := 0
	for _, c := range got {
		sum += c.Leaves
	}
	if sum != len(rows) {
		t.Fatalf("leaves sum %d, rows %d", sum, len(rows))
	}
}

func TestClassStatsRateRoundsToFourPlaces(t *testing.T) {
	var rows []blackboard.Row
	classes := map[string]string{}
	for i, id := range []string{"x/1", "x/2", "x/3"} {
		r := blackboard.Row{NodeID: id, Status: blackboard.StatusVerified}
		if i == 0 {
			r.Attempts = []blackboard.Attempt{att(1, 1, blackboard.VerdictPass)}
		}
		rows = append(rows, r)
		classes[id] = "wiring"
	}
	got := ClassStats(rows, classes, nil)
	if len(got) != 1 || got[0].FirstPassRate != 0.3333 {
		t.Fatalf("%+v", got)
	}
	if len(ClassStats(nil, nil, nil)) != 0 {
		t.Fatal("no rows must give no entries")
	}
}

func TestClassStatsTotalsReconcileWithLedger(t *testing.T) {
	rows := []blackboard.Row{{NodeID: "a/f1", Status: blackboard.StatusVerified}}
	classes := map[string]string{"a/f1": "pure"}
	calls := []ledger.Call{
		{NodeClass: "pure", PromptTokens: 1, CompletionTokens: 2},
		{NodeClass: "", PromptTokens: 10, CompletionTokens: 20},
		{NodeClass: "bogus", PromptTokens: 100, CompletionTokens: 200},
		{NodeClass: "storage", PromptTokens: 1000, CompletionTokens: 2000},
	}
	var n int
	var p, c int64
	for _, s := range ClassStats(rows, classes, calls) {
		n += s.Calls
		p += s.PromptTokens
		c += s.CompletionTokens
	}
	if n != 4 || p != 1111 || c != 2222 {
		t.Fatalf("calls %d prompt %d completion %d", n, p, c)
	}
}
