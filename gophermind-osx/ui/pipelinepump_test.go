package ui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-osx/client"
)

// fakePipelineEventsServer serves one task-status, one task-attempt, one
// wave-changed, and one run-report frame, matching the exact SSE shape
// gophermind-lib/serve's pipelineEventsHandler writes.
func fakePipelineEventsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: task-status\ndata: {\"id\":\"01-01\",\"status\":\"running\",\"wave\":1}\n\n")
		io.WriteString(w, "event: task-attempt\ndata: {\"task_id\":\"01-01\",\"model\":\"strong\",\"duration\":\"1m\",\"verdict\":\"pass\"}\n\n")
		io.WriteString(w, "event: wave-changed\ndata: {\"wave\":1,\"state\":\"finished\"}\n\n")
		io.WriteString(w, "event: run-report\ndata: {\"Models\":[{\"Model\":\"strong\"}]}\n\n")
	}))
}

func TestPipelinePump_RunAppliesEveryEventType(t *testing.T) {
	srv := fakePipelineEventsServer(t)
	t.Cleanup(srv.Close)

	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background())
	if err != nil {
		t.Fatalf("PipelineEvents: %v", err)
	}

	state := NewPipelineState()
	// Seed task "01-01" first: ApplyTaskStatus/ApplyTaskAttempt ignore an
	// id PipelineState hasn't seen yet (see pipeline.go's documented
	// contract), and this test wants to observe both applied.
	state.SetTasks([]phaseflow.Task{{ID: "01-01", Status: phaseflow.StatusPending}}, time.Time{})

	pump := &PipelinePump{State: state}
	if err := pump.Run(context.Background(), stream); err != nil {
		t.Fatalf("Run: %v", err)
	}

	tasks := state.Tasks()
	if len(tasks) != 1 || tasks[0].Status != phaseflow.StatusRunning {
		t.Errorf("Tasks() = %+v, want 01-01 running", tasks)
	}
	if len(tasks[0].Attempts) != 1 || tasks[0].Attempts[0].Model != "strong" {
		t.Errorf("Attempts = %+v", tasks[0].Attempts)
	}
	wave, waveState := state.CurrentWave()
	if wave != 1 || waveState != "finished" {
		t.Errorf("CurrentWave() = (%d, %q), want (1, \"finished\")", wave, waveState)
	}
	report := state.Report()
	if report == nil || len(report.Models) != 1 || report.Models[0].Model != "strong" {
		t.Errorf("Report() = %+v", report)
	}
}

func TestPipelinePump_RunReturnsNilOnCleanEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	t.Cleanup(srv.Close)

	c := client.New(client.Config{BaseURL: srv.URL, Token: "t"})
	stream, err := c.PipelineEvents(context.Background())
	if err != nil {
		t.Fatalf("PipelineEvents: %v", err)
	}

	pump := &PipelinePump{State: NewPipelineState()}
	if err := pump.Run(context.Background(), stream); err != nil {
		t.Errorf("Run: %v, want nil for a clean stream end", err)
	}
}
