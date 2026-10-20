//go:build e2e

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/build/blackboard"
	"gophermind/gophermind-lib/runview/runviewtest"
	"gophermind/gophermind-osx/client"
	"gophermind/gophermind-osx/connection"
	appui "gophermind/gophermind-osx/ui"
)

// e2eConnectLocalWithRoot is e2eConnectLocal (e2e_local_test.go) but also
// returns the workspace root: the pipeline routes read root/.gophermind/<run>/
// of the spawned gophermind-server.
func e2eConnectLocalWithRoot(t *testing.T) (*connection.Connection, string) {
	t.Helper()
	bin := e2eBuildServerBinary(t)
	root := t.TempDir()

	conn := connection.New(connection.BackendConfig{
		Name: "e2e-pipeline",
		Mode: connection.ModeLocal,
		Local: connection.LocalConfig{
			ServerBinaryPath: bin,
			Root:             root,
			StartupTimeout:   15 * time.Second,
			// A real breakdown turn can run several LLM round trips plus tool
			// calls, easily exceeding client.DefaultTimeout (30s).
			ClientTimeout: 3 * time.Minute,
		},
		HealthInterval: 500 * time.Millisecond,
	})
	t.Cleanup(conn.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.Status() != connection.StatusConnected {
		t.Fatalf("Status() = %v, want Connected", conn.Status())
	}
	return conn, root
}

// TestE2E_Pipeline_BreakdownSeed is the "start breakdown" half of the old
// pipeline e2e: create a session, send appui.BreakdownSeedPrompt through a real
// Stream call and confirm the round trip completes. Whether the model's
// answer is a good plan is model behavior and is not asserted.
func TestE2E_Pipeline_BreakdownSeed(t *testing.T) {
	conn, _ := e2eConnectLocalWithRoot(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	sessionID, err := cl.CreateSession(ctx, client.CreateSessionOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seed := appui.BreakdownSeedPrompt("brief.md", "Build a tiny CLI that reverses stdin.")
	stream, err := cl.Stream(ctx, sessionID, seed)
	if err != nil {
		t.Fatalf("Stream (breakdown seed): %v", err)
	}
	// A breakdown turn may include gated tool calls; auto-approve them the way
	// TestE2E_LocalMode_ApprovalFlow does, so an unresolved approval never
	// stalls the stream.
	tracker := appui.NewApprovalTracker(cl.Approve)
	sawDone := false
	deadline := time.After(2 * time.Minute)
readSeed:
	for {
		select {
		case <-deadline:
			break readSeed
		default:
		}
		ev, err := stream.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break readSeed
			}
			t.Fatalf("stream.Next (breakdown seed): %v", err)
		}
		if ev.Type == "approval-needed" {
			if an, err := ev.ApprovalNeeded(); err == nil {
				tracker.Add(sessionID, an.ApprovalID, an.Tool, an.Args)
				_ = tracker.Resolve(ctx, an.ApprovalID, true)
			}
		}
		if ev.Type == "done" {
			sawDone = true
			break readSeed
		}
	}
	stream.Close()
	if !sawDone {
		t.Fatal("breakdown-seed stream never reached done")
	}
}

type evResult struct {
	ev  client.Event
	err error
}

// nextEvent reads one event with a deadline; the read runs in its own
// goroutine because Next blocks on the connection.
func nextEvent(t *testing.T, s *client.EventStream, d time.Duration) client.Event {
	t.Helper()
	ch := make(chan evResult, 1)
	go func() {
		ev, err := s.Next()
		ch <- evResult{ev, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Next: %v", r.err)
		}
		return r.ev
	case <-time.After(d):
		t.Fatal("timed out waiting for a pipeline event")
	}
	return client.Event{}
}

// TestE2E_Pipeline_RunFolder serves a real run folder from the spawned
// server and reads every endpoint through the app's client, then watches a node
// change arrive on the event stream. No model is involved.
func TestE2E_Pipeline_RunFolder(t *testing.T) {
	conn, root := e2eConnectLocalWithRoot(t)
	cl := conn.Client()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	fx := runviewtest.Build(t, root, runviewtest.Building)

	runs, err := cl.PipelineRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].RunID != fx.RunID || runs[0].Status != "building" {
		t.Fatalf("PipelineRuns = %+v, %v", runs, err)
	}
	st, err := cl.PipelineState(ctx, "")
	if err != nil || st.Run == nil || st.Run.RunID != fx.RunID || len(st.Nodes) != 5 || st.EventSeq == 0 {
		t.Fatalf("PipelineState = %+v, %v", st, err)
	}

	// The stream resumes exactly at the snapshot.
	events, err := cl.PipelineEvents(ctx, "", st.EventSeq)
	if err != nil {
		t.Fatalf("PipelineEvents: %v", err)
	}
	defer events.Close()

	if err := fx.BB.SetStatus(ctx, fx.RunID, "fn-bye", blackboard.StatusInProgress, blackboard.StatusVerified); err != nil {
		t.Fatal(err)
	}
	ev := nextEvent(t, events, 15*time.Second)
	ns, err := ev.NodeStatus()
	if ev.Type != "node-status" || err != nil || ns.ID != "fn-bye" || ns.Status != "verified" || ev.ID != st.EventSeq+1 {
		t.Fatalf("event = %+v %+v %v; want node-status fn-bye verified with id %d", ev, ns, err, st.EventSeq+1)
	}

	nb, err := cl.PipelineNode(ctx, "", "fn-bye")
	if err != nil || nb.Runtime == nil || nb.Runtime.Status != "verified" {
		t.Fatalf("PipelineNode = %+v, %v", nb, err)
	}
	txt, err := cl.PipelineArtifact(ctx, "", "fn-bye", 1, "check-output.txt")
	if err != nil || !strings.Contains(txt, "want bye, got hello") {
		t.Fatalf("PipelineArtifact = %q, %v", txt, err)
	}
	db, err := cl.PipelineDecisions(ctx, "")
	if err != nil || len(db.Decisions) != 2 { // the clarify decision and the Building fixture's revision decision
		t.Fatalf("PipelineDecisions = %+v, %v", db, err)
	}
	rb, err := cl.PipelineReport(ctx, "")
	if err != nil || rb.IsComplete() {
		t.Fatalf("PipelineReport = %+v, %v (a running run gives a partial report)", rb, err)
	}
}
