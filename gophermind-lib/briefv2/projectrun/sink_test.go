package projectrun

import (
	"bytes"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
)

func TestSinkLines(t *testing.T) {
	canary := "CANARY-PROMPT-TEXT"
	cases := []struct {
		name string
		ev   events.Event
		want string
	}{
		{"started", events.Event{Kind: events.KindStageStarted, Stage: "clarify"}, "clarify: started\n"},
		{"done", events.Event{Kind: events.KindStageFinished, Stage: "contract"}, "contract: done\n"},
		{"warning", events.Event{Kind: events.KindWarning, Message: "leaf_defaulted: 2 nodes"}, "warning: leaf_defaulted: 2 nodes\n"},
		{"ledger error", events.Event{Kind: events.KindLedgerError, Message: "disk full"}, "warning: disk full\n"},
		{"gap", events.Event{Kind: events.KindCoverageGap, Message: "R3 uncovered"}, "coverage gap, R3 uncovered\n"},
		{"other with message", events.Event{Kind: events.KindWaitingOnHuman, NodeID: "a/f1", Message: "needs a person"}, "a/f1: waiting_on_human needs a person\n"},
		{"other without message", events.Event{Kind: events.KindWaitingOnHuman, NodeID: "a/f1"}, ""},
		{"call never printed", events.Event{Kind: events.KindCall, NodeID: "a/f1", Call: &ledger.Call{PromptSHA256: canary}}, ""},
		{"call with message", events.Event{Kind: events.KindCall, NodeID: "a/f1", Message: "ok", Call: &ledger.Call{PromptSHA256: canary}}, "a/f1: call ok\n"},
		{"multi-line message is one line", events.Event{Kind: events.KindWarning, Message: "first\nsecond"}, "warning: first\n"},
	}
	for _, c := range cases {
		var b bytes.Buffer
		NewSink(&b).Emit(c.ev)
		if b.String() != c.want {
			t.Errorf("%s: got %q want %q", c.name, b.String(), c.want)
		}
		if strings.Contains(b.String(), canary) {
			t.Errorf("%s: call payload printed", c.name)
		}
	}
}

func TestSinkCountsPlannerWarnings(t *testing.T) {
	var b bytes.Buffer
	s := NewSink(&b)
	w := func(m string) { s.Emit(events.Event{Kind: events.KindWarning, Message: m}) }
	w("leaf_defaulted: 2 nodes still failed (a)")
	w("leaf_defaulted: 3 nodes still failed (b)")
	w("doc_defaulted: 4 functions lacked a doc")
	w("leaf_normalized: 5 values were reshaped")
	w("outline_id_normalized: 6 ids rewritten")
	w("something else: 9 things")
	w("leaf_defaulted: many")
	s.Emit(events.Event{Kind: events.KindStageStarted, Message: "leaf_defaulted: 50"})
	got := s.Counts()
	want := Counts{LeafDefaulted: 5, DocDefaulted: 4, LeafNormalized: 5, OutlineIDNormalized: 6}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}
