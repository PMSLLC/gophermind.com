package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gophermind/gophermind-osx/client"
)

func eventsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gophermind-Pipeline-Schema", "1")
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelinePump_RunAppliesEveryEventTypeAndTracksTheLastID(t *testing.T) {
	srv := eventsServer(t,
		"id: 5\nevent: node-status\ndata: {\"id\":\"fn-a\",\"status\":\"in_progress\",\"wave\":0,\"revision\":3}\n\n"+
			"id: 6\nevent: node-attempt\ndata: {\"id\":\"fn-a\",\"attempt\":{\"model\":\"strong\",\"verdict\":\"pass\",\"started_at\":\"t\"}}\n\n"+
			"id: 7\nevent: node-result\ndata: {\"id\":\"fn-a\",\"result\":{\"commit\":\"abc\",\"files_changed\":[]}}\n\n"+
			"id: 8\nevent: wave-changed\ndata: {\"wave\":0,\"state\":\"finished\"}\n\n"+
			"id: 8\nevent: run-status\ndata: {\"status\":\"verified\",\"stop_reason\":\"\"}\n\n"+
			"id: 8\nevent: run-report\ndata: {\"schema_version\":2,\"run_id\":\"r\",\"status\":\"verified\"}\n\n")
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background(), "", -1)
	if err != nil {
		t.Fatal(err)
	}
	state := NewPipelineState()
	state.SetState(sampleState("gm-2026-10-08-001", 4))

	if err := (&PipelinePump{State: state}).Run(context.Background(), stream); err != nil {
		t.Fatalf("Run: %v", err)
	}
	n := state.Nodes()[1]
	if n.Status != "in_progress" || n.Revision != 3 || n.AttemptCount != 1 || n.LastAttempt == nil || n.LastAttempt.Model != "strong" || n.Result == nil {
		t.Errorf("fn-a = %+v", n)
	}
	if w, s := state.CurrentWave(); w != 0 || s != "finished" {
		t.Errorf("wave = %d %q", w, s)
	}
	if state.Run().Status != "verified" {
		t.Errorf("run status = %q", state.Run().Status)
	}
	if r := state.Report(); r == nil || r.RunID != "r" {
		t.Errorf("report = %+v", r)
	}
	if state.LastSeq() != 8 {
		t.Errorf("LastSeq = %d, want 8", state.LastSeq())
	}
}

func TestPipelinePump_RunReturnsNilOnCleanEOF(t *testing.T) {
	srv := eventsServer(t, "")
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background(), "", -1)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&PipelinePump{State: NewPipelineState()}).Run(context.Background(), stream); err != nil {
		t.Errorf("Run: %v, want nil for a clean stream end", err)
	}
}

func TestPipelinePump_AnUndecodableFrameStillAdvancesTheSeq(t *testing.T) {
	srv := eventsServer(t, "id: 9\nevent: node-status\ndata: {broken\n\n")
	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, _ := c.PipelineEvents(context.Background(), "", -1)
	state := NewPipelineState()
	if err := (&PipelinePump{State: state}).Run(context.Background(), stream); err != nil {
		t.Fatal(err)
	}
	if state.LastSeq() != 9 {
		t.Errorf("LastSeq = %d, want 9: the line was consumed, so a reconnect must not replay it", state.LastSeq())
	}
}
