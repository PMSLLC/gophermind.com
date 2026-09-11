package serve

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/internal/phaseflow"
)

// TestPipelineDashboardServesWithoutToken checks that GET /pipeline serves
// the dashboard HTML with no Authorization header required - a plain
// browser navigation cannot attach one, so the page itself cannot be
// behind the same auth as the data routes. It also checks the served page
// has no hardcoded demo data (the mockup's `const tasks = [...]`), since
// the source spec's own test requires the dashboard to show real state,
// not a replayed mock.
//
// It also guards a real finding from review: the bearer token this page
// asks for can run shell commands and write files on the machine, so
// leaking it is leaking code execution, not just a session id. The page
// must never read it from location.search (the query string reaches
// server access logs, browser history, and - since a subresource fetch
// with no referrer policy sends the full URL as Referer - third-party
// servers too) and must carry a no-referrer meta tag so nothing on the
// page can leak its URL to a subresource origin.
func TestPipelineDashboardServesWithoutToken(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: t.TempDir()}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /pipeline with no token: got %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "const tasks = [") {
		t.Error("dashboard still embeds the mockup's hardcoded demo tasks")
	}
	if !strings.Contains(body, "/pipeline/state") || !strings.Contains(body, "/pipeline/events") {
		t.Error("dashboard does not reference the real state/events routes")
	}
	if strings.Contains(body, "location.search") {
		t.Error("dashboard reads a token (or anything else) from the query string - " +
			"it reaches server logs, browser history and, with no referrer policy, " +
			"third-party subresource requests; the token must only ever come from the URL fragment")
	}
	if !strings.Contains(body, `<meta name="referrer" content="no-referrer">`) {
		t.Error("dashboard is missing <meta name=\"referrer\" content=\"no-referrer\">, " +
			"so a subresource fetch could leak the page URL (and any token in it) to a third party")
	}
}

// TestPipelineDashboardSkippedWhenPipelineNil checks that GET /pipeline is
// simply not registered when Deps.Pipeline is nil, matching every other
// optional route's nil-disables convention.
func TestPipelineDashboardSkippedWhenPipelineNil(t *testing.T) {
	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pipeline")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /pipeline with Pipeline unset: got %d, want 404", resp.StatusCode)
	}
}

// TestPipelineStateReturnsRealTasksAndRequiresToken checks that GET
// /pipeline/state returns the tasks (with wave, status and attempts) from a
// real assignments.json, and that it is behind the same bearer-token auth
// as the rest of the mux.
func TestPipelineStateReturnsRealTasksAndRequiresToken(t *testing.T) {
	root := t.TempDir()
	a := phaseflow.Assignments{Tasks: []phaseflow.Task{
		{
			ID: "t1", Title: "Implement endpoint", Wave: 2, Status: phaseflow.StatusDone,
			Attempts: []phaseflow.Attempt{
				{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "timeout"},
				{Model: "model-b", Duration: "2s", Verdict: "pass", Reason: "6/6 passing"},
			},
		},
		{ID: "t2", Title: "Write tests", Wave: 1, Status: phaseflow.StatusPending},
	}}
	if err := a.Save(root); err != nil {
		t.Fatalf("save assignments: %v", err)
	}

	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: root}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// No token: unauthorized.
	resp, err := http.Get(srv.URL + "/pipeline/state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token request: got %d, want 401", resp.StatusCode)
	}

	// With token: real task state.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pipeline/state", nil)
	req.Header.Set("Authorization", "Bearer t")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized request: got %d, want 200", resp.StatusCode)
	}
	var body pipelineStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(body.Tasks))
	}
	var t1 phaseflow.Task
	for _, tk := range body.Tasks {
		if tk.ID == "t1" {
			t1 = tk
		}
	}
	if t1.Wave != 2 || t1.Status != phaseflow.StatusDone {
		t.Errorf("t1 = %+v, want wave=2 status=done", t1)
	}
	if len(t1.Attempts) != 2 || t1.Attempts[1].Model != "model-b" || t1.Attempts[1].Verdict != "pass" {
		t.Errorf("t1 attempts = %+v, want the two real recorded attempts", t1.Attempts)
	}
}

// TestPipelineReportRequiresToken checks GET /pipeline/report is behind the
// same auth as the other pipeline routes and returns a real aggregation.
func TestPipelineReportRequiresToken(t *testing.T) {
	root := t.TempDir()
	a := phaseflow.Assignments{Tasks: []phaseflow.Task{
		{ID: "t1", Attempts: []phaseflow.Attempt{
			{Model: "model-a", Duration: "1s", Verdict: "pass", Reason: "ok"},
		}},
	}}
	if err := a.Save(root); err != nil {
		t.Fatalf("save: %v", err)
	}

	t.Setenv("GOPHERMIND_SERVE_TOKEN", "t")
	mux, err := NewMux(Deps{Pipeline: &PipelineDeps{Root: root}}, Options{})
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pipeline/report")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token request: got %d, want 401", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pipeline/report", nil)
	req.Header.Set("Authorization", "Bearer t")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var report phaseflow.RunReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(report.Models) != 1 || report.Models[0].Model != "model-a" || report.Models[0].Passes != 1 {
		t.Errorf("report.Models = %+v, want one model-a entry with 1 pass", report.Models)
	}
}

// TestPipelineHubTaskStatusEvent checks that publishing a task-status event
// through the hub reaches a subscribed /pipeline/events client as a
// "task-status" SSE frame carrying the real field values, not placeholders.
func TestPipelineHubTaskStatusEvent(t *testing.T) {
	hub := NewPipelineHub()
	srv := httptest.NewServer(pipelineEventsHandler(hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Give the handler a moment to subscribe before publishing, since the
	// subscription happens inside the handler goroutine after the response
	// headers are already flowing.
	waitForSubscribers(t, hub, 1)

	hub.TaskStatus("t1", phaseflow.StatusRunning, 2)

	event, data := readOneSSEFrame(t, bufio.NewReader(resp.Body))
	if event != "task-status" {
		t.Fatalf("event = %q, want task-status", event)
	}
	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Wave   int    `json:"wave"`
	}
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
	if got.ID != "t1" || got.Status != phaseflow.StatusRunning || got.Wave != 2 {
		t.Errorf("got %+v, want id=t1 status=running wave=2", got)
	}
}

// TestPipelineHubTaskAttemptEvent checks that a completed attempt emits a
// task-attempt frame with model, duration, verdict and reason all populated
// from the real Attempt, not placeholder text.
func TestPipelineHubTaskAttemptEvent(t *testing.T) {
	hub := NewPipelineHub()
	srv := httptest.NewServer(pipelineEventsHandler(hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	waitForSubscribers(t, hub, 1)

	hub.TaskAttempt("t1", phaseflow.Attempt{
		Model: "llama-3.3-70b", Duration: "4.8s", Verdict: "fail", Reason: "2/6 failing",
	})

	event, data := readOneSSEFrame(t, bufio.NewReader(resp.Body))
	if event != "task-attempt" {
		t.Fatalf("event = %q, want task-attempt", event)
	}
	var got struct {
		TaskID   string `json:"task_id"`
		Model    string `json:"model"`
		Duration string `json:"duration"`
		Verdict  string `json:"verdict"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
	if got.TaskID != "t1" || got.Model != "llama-3.3-70b" || got.Duration != "4.8s" ||
		got.Verdict != "fail" || got.Reason != "2/6 failing" {
		t.Errorf("got %+v, want the real attempt fields, not placeholders", got)
	}
}

// TestPipelineHubEventsArriveInOrder checks that several published events
// reach a subscriber in the order they were published.
func TestPipelineHubEventsArriveInOrder(t *testing.T) {
	hub := NewPipelineHub()
	srv := httptest.NewServer(pipelineEventsHandler(hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	waitForSubscribers(t, hub, 1)

	hub.TaskStatus("t1", phaseflow.StatusRunning, 1)
	hub.TaskAttempt("t1", phaseflow.Attempt{Model: "model-a", Duration: "1s", Verdict: "fail", Reason: "r1"})
	hub.TaskStatus("t1", phaseflow.StatusNeedsRevision, 1)

	br := bufio.NewReader(resp.Body)
	var got []string
	for i := 0; i < 3; i++ {
		event, _ := readOneSSEFrame(t, br)
		got = append(got, event)
	}
	want := []string{"task-status", "task-attempt", "task-status"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frame order = %v, want %v", got, want)
		}
	}
}

// TestPipelineEventsDisconnectDoesNotLeak checks that a client disconnecting
// mid-stream is unsubscribed (no goroutine or subscription left behind) and
// that publishing afterward does not block.
func TestPipelineEventsDisconnectDoesNotLeak(t *testing.T) {
	hub := NewPipelineHub()
	srv := httptest.NewServer(pipelineEventsHandler(hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	waitForSubscribers(t, hub, 1)

	// Simulate the client going away.
	resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.subCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("subscriber not removed after disconnect, subCount=%d", hub.subCount())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Publishing after every subscriber has gone must not block.
	done := make(chan struct{})
	go func() {
		hub.TaskStatus("t1", phaseflow.StatusDone, 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked after subscriber disconnected")
	}
}

// waitForSubscribers polls until hub has exactly n subscribers, failing the
// test if that never happens. Needed because /pipeline/events subscribes
// from inside its handler goroutine, asynchronously with the client's
// response arriving.
func waitForSubscribers(t *testing.T, hub *PipelineHub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for hub.subCount() != n {
		if time.Now().After(deadline) {
			t.Fatalf("subCount = %d after waiting, want %d", hub.subCount(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readOneSSEFrame reads and parses exactly one "event: ...\ndata: ...\n\n"
// frame from br, a reader held open across the caller's successive calls -
// necessary because a single Read can return more than one frame's worth of
// bytes, and a fresh reader per call would silently drop whatever it did not
// consume.
func readOneSSEFrame(t *testing.T, br *bufio.Reader) (event, data string) {
	t.Helper()
	inFrame := false
	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\n")
		if trimmed == "" {
			if inFrame {
				return event, data
			}
		} else {
			inFrame = true
			switch {
			case strings.HasPrefix(trimmed, "event: "):
				event = strings.TrimPrefix(trimmed, "event: ")
			case strings.HasPrefix(trimmed, "data: "):
				if data != "" {
					data += "\n"
				}
				data += strings.TrimPrefix(trimmed, "data: ")
			}
		}
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
	}
}
