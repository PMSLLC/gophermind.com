package ui

import (
	"sync/atomic"
	"testing"

	"gophermind/gophermind-lib/runview"
)

func sampleState(run string, seq int64) runview.StateBody {
	return runview.StateBody{
		SchemaVersion: 1, GeneratedAt: "2026-10-08T13:00:00Z", EventSeq: seq,
		Run: &runview.RunSummary{RunID: run, Status: "building"},
		Nodes: []runview.NodeSummary{
			{ID: "greeting", Kind: "component", Title: "Greeting", Status: "pending"},
			{ID: "fn-a", Kind: "function", Parent: "greeting", Status: "pending", Wave: 0},
			{ID: "fn-b", Kind: "function", Parent: "greeting", Status: "in_progress", Wave: 1},
		},
	}
}

func TestPipelineState_SetStateStoresRunNodesAndSeq(t *testing.T) {
	p := NewPipelineState()
	var changes int32
	p.OnChange(func() { atomic.AddInt32(&changes, 1) })
	p.SetState(sampleState("gm-2026-10-08-001", 12))
	if p.RunID() != "gm-2026-10-08-001" || len(p.Nodes()) != 3 || p.LastSeq() != 12 || p.GeneratedAt() == "" {
		t.Errorf("state = %q %d nodes seq %d", p.RunID(), len(p.Nodes()), p.LastSeq())
	}
	if atomic.LoadInt32(&changes) != 1 {
		t.Errorf("OnChange fired %d times, want 1", changes)
	}
	// The returned slices are copies.
	p.Nodes()[0].Status = "x"
	if p.Nodes()[0].Status == "x" {
		t.Error("Nodes() must return a copy")
	}
}

func TestPipelineState_RefetchKeepsTheSeqOfTheSameRunAndResetsOnANewRun(t *testing.T) {
	p := NewPipelineState()
	p.SetState(sampleState("gm-2026-10-08-001", 10))
	p.SetLastSeq(25) // the stream has moved on
	p.SetState(sampleState("gm-2026-10-08-001", 20))
	if p.LastSeq() != 25 {
		t.Errorf("LastSeq = %d, want 25 (an older snapshot must not rewind the stream)", p.LastSeq())
	}
	p.SetState(sampleState("gm-2026-10-08-001", 40))
	if p.LastSeq() != 40 {
		t.Errorf("LastSeq = %d, want 40", p.LastSeq())
	}
	p.SetState(sampleState("gm-2026-10-09-001", 3))
	if p.LastSeq() != 3 {
		t.Errorf("LastSeq = %d, want 3 after a different run", p.LastSeq())
	}
	p.SetLastSeq(1)
	if p.LastSeq() != 3 {
		t.Error("SetLastSeq must never move backwards")
	}
}

func TestPipelineState_ApplyNodeEvents(t *testing.T) {
	p := NewPipelineState()
	p.SetState(sampleState("gm-2026-10-08-001", 1))

	p.ApplyNodeStatus("fn-a", "verified", 0, 2)
	p.ApplyNodeStatus("nope", "verified", 0, 0) // unknown id: ignored
	a1 := runview.Attempt{Model: "m", Verdict: "fail", StartedAt: "t1"}
	p.ApplyNodeAttempt("fn-a", a1)
	p.ApplyNodeAttempt("fn-a", a1) // the same attempt twice (a replay): counted once
	p.ApplyNodeAttempt("fn-a", runview.Attempt{Model: "m", Verdict: "pass", StartedAt: "t2"})
	p.ApplyNodeResult("fn-a", runview.NodeResult{Commit: "abc"})
	n := p.Nodes()[1]
	if n.Status != "verified" || n.Revision != 2 || n.AttemptCount != 2 || n.LastAttempt == nil || n.LastAttempt.Verdict != "pass" || n.Result == nil || n.Result.Commit != "abc" {
		t.Errorf("fn-a = %+v", n)
	}
	p.ApplyWaveChanged(1, "started")
	if w, s := p.CurrentWave(); w != 1 || s != "started" {
		t.Errorf("wave = %d %q", w, s)
	}
	p.ApplyRunStatus("verified", "")
	if p.Run().Status != "verified" {
		t.Errorf("run status = %q", p.Run().Status)
	}
}

func TestPipelineState_ReportAndMessage(t *testing.T) {
	p := NewPipelineState()
	if p.Report() != nil || p.Message() != "" {
		t.Fatal("a new state has no report and no message")
	}
	var r runview.ReportBody
	r.Status = "verified"
	p.SetReport(r)
	p.SetMessage("this server and this app disagree")
	if p.Report() == nil || p.Report().Status != "verified" || p.Message() == "" {
		t.Errorf("report %+v message %q", p.Report(), p.Message())
	}
}

func TestPipelineState_ReportClearsOnRunChangeNotSameRun(t *testing.T) {
	p := NewPipelineState()
	p.SetState(runview.StateBody{Run: &runview.RunSummary{RunID: "r1"}})
	var rb runview.ReportBody
	rb.Status = "verified"
	p.SetReport(rb)
	p.SetState(runview.StateBody{Run: &runview.RunSummary{RunID: "r1"}})
	if p.Report() == nil {
		t.Error("report cleared on a same-run snapshot")
	}
	p.SetState(runview.StateBody{Run: &runview.RunSummary{RunID: "r2"}})
	if p.Report() != nil {
		t.Error("report kept after the run changed")
	}
}
