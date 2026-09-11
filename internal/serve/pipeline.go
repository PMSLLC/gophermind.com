package serve

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"gophermind/internal/phaseflow"
)

// pipelineAssets embeds the dashboard page (pipeline piece 5, task 3). It
// adapts docs/design/pipeline_view_mockup.html: same layout, typography and
// status vocabulary, but its data comes from GET /pipeline/state and GET
// /pipeline/events instead of the mockup's hardcoded `const tasks`.
//
//go:embed assets/pipeline.html
var pipelineAssets embed.FS

// pipelineDashboardHandler handles GET /pipeline: it serves the embedded
// dashboard page. The page itself carries no data and is not behind the
// bearer-token auth the data routes require - a plain browser navigation
// cannot attach an Authorization header, so gating the shell the same way
// would make it unreachable. The page's own JS prompts for the token and
// sends it on every /pipeline/state, /pipeline/events and /pipeline/report
// call, which are the routes that actually carry task data.
func pipelineDashboardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		f, err := pipelineAssets.Open("assets/pipeline.html")
		if err != nil {
			http.Error(w, "dashboard asset missing", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		_, _ = io.Copy(w, f)
	}
}

// This file implements the server side of the live pipeline view (pipeline
// piece 5, see docs/superpowers/specs/2026-09-11-harness-pipeline-design.md,
// from source spec sections 4 and 5): GET /pipeline/state (current tasks,
// waves, statuses and attempts), GET /pipeline/events (SSE: task and
// attempt changes as they happen) and GET /pipeline/report (the run
// summary). GET /pipeline, the dashboard itself, is added alongside this in
// the same piece.
//
// State is always read fresh from assignments.json on request - there is no
// cache here to go stale. Live events are a separate concern: PipelineHub
// fans out frames to every connected /pipeline/events client, fed by
// whatever is driving a run (see FallbackRunner.OnAttempt in fallback.go,
// which piece 3 added for exactly this purpose).

// PipelineDeps configures the pipeline routes. Root is the project root
// whose .planning/assignments.json backs GET /pipeline/state and GET
// /pipeline/report. Hub, when nil, is replaced with a fresh empty
// PipelineHub, so GET /pipeline/events always serves a valid stream even
// when nothing is currently publishing to it.
type PipelineDeps struct {
	Root string
	Hub  *PipelineHub
}

// pipelineEvent is one frame queued to a PipelineHub subscriber: an SSE
// event name plus its already-encoded JSON data.
type pipelineEvent struct {
	event string
	data  string
}

// pipelineSubBuffer bounds how many unread events a slow /pipeline/events
// client can fall behind by before PipelineHub.publish starts dropping
// frames for that subscriber specifically, rather than letting one slow
// reader block every other subscriber or the run that is publishing.
const pipelineSubBuffer = 64

// PipelineHub fans out pipeline events (task-status, task-attempt,
// wave-changed, run-report) to every connected GET /pipeline/events client.
// The zero value is not usable; build one with NewPipelineHub.
//
// Publishing never blocks the publisher: each subscriber owns a buffered
// channel, and a full channel means that one subscriber drops the frame
// rather than stalling the goroutine doing the publishing, or every other
// subscriber.
type PipelineHub struct {
	mu   sync.Mutex
	subs map[chan pipelineEvent]struct{}
}

// NewPipelineHub builds an empty hub.
func NewPipelineHub() *PipelineHub {
	return &PipelineHub{subs: make(map[chan pipelineEvent]struct{})}
}

// subscribe registers a new subscriber and returns its event channel plus a
// function that removes it. Callers must invoke the returned function
// exactly once on every exit path, including a client disconnect, or the
// subscription leaks.
func (h *PipelineHub) subscribe() (chan pipelineEvent, func()) {
	ch := make(chan pipelineEvent, pipelineSubBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// subCount reports the current number of subscribers. It exists for tests
// that need to confirm a disconnected client's subscription was actually
// removed, not just that its handler returned.
func (h *PipelineHub) subCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish fans data out to every current subscriber. A subscriber whose
// channel is already full is skipped rather than blocked, so one slow or
// stalled client can never hold up publish for anyone else.
func (h *PipelineHub) publish(event, data string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- pipelineEvent{event: event, data: data}:
		default:
		}
	}
}

// TaskStatus publishes a "task-status" event: task id changed to status,
// which belongs to wave.
func (h *PipelineHub) TaskStatus(id, status string, wave int) {
	b, _ := json.Marshal(struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Wave   int    `json:"wave"`
	}{ID: id, Status: status, Wave: wave})
	h.publish("task-status", string(b))
}

// TaskAttempt publishes a "task-attempt" event: one model's attempt at
// taskID completed, whether it passed or failed.
func (h *PipelineHub) TaskAttempt(taskID string, a phaseflow.Attempt) {
	b, _ := json.Marshal(struct {
		TaskID   string `json:"task_id"`
		Model    string `json:"model"`
		Duration string `json:"duration"`
		Verdict  string `json:"verdict"`
		Reason   string `json:"reason"`
	}{TaskID: taskID, Model: a.Model, Duration: a.Duration, Verdict: a.Verdict, Reason: a.Reason})
	h.publish("task-attempt", string(b))
}

// WaveChanged publishes a "wave-changed" event: wave started or finished,
// per state ("started" or "finished").
func (h *PipelineHub) WaveChanged(wave int, state string) {
	b, _ := json.Marshal(struct {
		Wave  int    `json:"wave"`
		State string `json:"state"`
	}{Wave: wave, State: state})
	h.publish("wave-changed", string(b))
}

// RunReport publishes a "run-report" event: a run ended and produced r.
func (h *PipelineHub) RunReport(r phaseflow.RunReport) {
	b, _ := json.Marshal(r)
	h.publish("run-report", string(b))
}

// pipelineStateResponse is the JSON body of GET /pipeline/state.
type pipelineStateResponse struct {
	Tasks       []phaseflow.Task `json:"tasks"`
	GeneratedAt time.Time        `json:"generated_at"`
}

// pipelineStateHandler handles GET /pipeline/state: every task in root's
// assignments.json, with its wave, status and attempt history, read fresh
// on every request. A project with no plan yet (no assignments.json) is not
// an error - it returns an empty task list, matching LoadAssignments' own
// "not found" contract.
func pipelineStateHandler(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		a, _, err := phaseflow.LoadAssignments(root)
		if err != nil {
			http.Error(w, "load assignments", http.StatusInternalServerError)
			return
		}
		tasks := a.Tasks
		if tasks == nil {
			tasks = []phaseflow.Task{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pipelineStateResponse{Tasks: tasks, GeneratedAt: time.Now()})
	}
}

// pipelineReportHandler handles GET /pipeline/report: the run report
// aggregated from root's current assignments.json via
// phaseflow.BuildRunReport. The report is a pure derived view over
// Attempts, so it is well defined at any point in a run; callers that want
// the final report simply call it after the run ends.
func pipelineReportHandler(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		a, _, err := phaseflow.LoadAssignments(root)
		if err != nil {
			http.Error(w, "load assignments", http.StatusInternalServerError)
			return
		}
		report := phaseflow.BuildRunReport(a.Tasks, time.Now())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	}
}

// pipelineEventsHandler handles GET /pipeline/events: an SSE stream of
// task-status, task-attempt, wave-changed and run-report frames, written in
// the order hub publishes them via writeSSEEvent (sse.go), which already
// handles the newline escaping that makes multi-line data safe.
//
// It subscribes for the lifetime of the request and always unsubscribes on
// return - including when the client disconnects, detected via
// r.Context().Done() - so a client that goes away never leaks a
// subscription or blocks whatever is publishing.
func pipelineEventsHandler(hub *PipelineHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "use GET", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)
		// Write and flush the headers immediately, before subscribing or
		// waiting for the first event: otherwise a client whose request
		// method blocks until it sees a response (e.g. net/http's Do) would
		// hang until the first event happened to arrive, rather than
		// establishing the stream right away.
		w.WriteHeader(http.StatusOK)
		if flusher != nil {
			flusher.Flush()
		}

		ch, unsubscribe := hub.subscribe()
		defer unsubscribe()

		for {
			select {
			case ev := <-ch:
				writeSSEEvent(w, flusher, ev.event, ev.data)
			case <-r.Context().Done():
				return
			}
		}
	}
}
