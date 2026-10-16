package ui

import (
	"sync"

	"gophermind/gophermind-lib/runview"
)

// PipelineState is the pipeline panel's plain-Go state: the newest run's
// summary and node list from GET /pipeline/state, kept live by applying the
// GET /pipeline/events frames on top, the current wave, the run report once one
// arrives, and a message for the panel (a schema mismatch). It also remembers
// the last event id applied, which is where a reconnect resumes. Same split as
// Transcript/ApprovalTracker/ModelPickerState: mutex-protected, cgo-free,
// OnChange-driven.
type PipelineState struct {
	mu          sync.Mutex
	run         *runview.RunSummary
	nodes       []runview.NodeSummary
	generatedAt string
	lastSeq     int64
	snapSeq     int64
	wave        int
	waveState   string
	report      *runview.ReportBody
	message     string
	onChange    func()
}

// NewPipelineState returns an empty pipeline state.
func NewPipelineState() *PipelineState { return &PipelineState{} }

// OnChange registers f to be called after every mutation. Same non-blocking,
// non-reentrant contract as Transcript.OnChange.
func (p *PipelineState) OnChange(f func()) {
	p.mu.Lock()
	p.onChange = f
	p.mu.Unlock()
}

func (p *PipelineState) notify() {
	p.mu.Lock()
	f := p.onChange
	p.mu.Unlock()
	if f != nil {
		f()
	}
}

// SetState replaces the run and its nodes with a GET /pipeline/state snapshot.
// The stream position becomes the snapshot's event_seq for a new run; for the
// same run it only moves forward, so an older snapshot cannot rewind a stream
// that has already delivered later events.
func (p *PipelineState) SetState(b runview.StateBody) {
	p.mu.Lock()
	sameRun := p.run != nil && b.Run != nil && p.run.RunID == b.Run.RunID
	if b.Run != nil {
		r := *b.Run
		p.run = &r
	} else {
		p.run = nil
	}
	p.nodes = append([]runview.NodeSummary(nil), b.Nodes...)
	p.generatedAt = b.GeneratedAt
	p.snapSeq = b.EventSeq
	if !sameRun || b.EventSeq > p.lastSeq {
		p.lastSeq = b.EventSeq
	}
	p.mu.Unlock()
	p.notify()
}

// SnapSeq is the event_seq of the last SetState snapshot. Node events at or
// below it are already reflected in the nodes, so a replay must not apply them.
func (p *PipelineState) SnapSeq() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapSeq
}

// Run returns a copy of the current run summary, or nil.
func (p *PipelineState) Run() *runview.RunSummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return nil
	}
	r := *p.run
	return &r
}

// RunID is the current run's id, or "".
func (p *PipelineState) RunID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return ""
	}
	return p.run.RunID
}

// Nodes returns a copy of the node list.
func (p *PipelineState) Nodes() []runview.NodeSummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]runview.NodeSummary(nil), p.nodes...)
}

// GeneratedAt is the timestamp of the last SetState snapshot.
func (p *PipelineState) GeneratedAt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.generatedAt
}

// LastSeq is the id of the last event applied: where a reconnect resumes.
func (p *PipelineState) LastSeq() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSeq
}

// SetLastSeq moves the stream position forward; it never moves it back.
func (p *PipelineState) SetLastSeq(n int64) {
	p.mu.Lock()
	if n > p.lastSeq {
		p.lastSeq = n
	}
	p.mu.Unlock()
}

// find returns a pointer into p.nodes, or nil. Caller must hold p.mu.
func (p *PipelineState) find(id string) *runview.NodeSummary {
	for i := range p.nodes {
		if p.nodes[i].ID == id {
			return &p.nodes[i]
		}
	}
	return nil
}

// ApplyNodeStatus applies a node-status event. An id this state has not seen
// (an event before the first snapshot) is ignored, and nothing fires. The
// revision is the node's value as it is now: a revision log line and a status
// line both arrive as node-status.
func (p *PipelineState) ApplyNodeStatus(id, status string, wave, revision int) {
	p.mu.Lock()
	n := p.find(id)
	if n == nil {
		p.mu.Unlock()
		return
	}
	n.Status, n.Wave, n.Revision = status, wave, revision
	p.mu.Unlock()
	p.notify()
}

// ApplyNodeAttempt applies a node-attempt event. An attempt with the same
// started_at as the last one is a replay and is not counted twice.
func (p *PipelineState) ApplyNodeAttempt(id string, a runview.Attempt) {
	p.mu.Lock()
	n := p.find(id)
	if n == nil || (n.LastAttempt != nil && n.LastAttempt.StartedAt == a.StartedAt && a.StartedAt != "") {
		p.mu.Unlock()
		return
	}
	n.LastAttempt = &a
	n.AttemptCount++
	p.mu.Unlock()
	p.notify()
}

// ApplyNodeResult applies a node-result event.
func (p *PipelineState) ApplyNodeResult(id string, r runview.NodeResult) {
	p.mu.Lock()
	n := p.find(id)
	if n == nil {
		p.mu.Unlock()
		return
	}
	n.Result = &r
	p.mu.Unlock()
	p.notify()
}

// ApplyWaveChanged applies a wave-changed event.
func (p *PipelineState) ApplyWaveChanged(wave int, state string) {
	p.mu.Lock()
	p.wave, p.waveState = wave, state
	p.mu.Unlock()
	p.notify()
}

// CurrentWave returns the most recently reported wave and its state
// ("started" or "finished").
func (p *PipelineState) CurrentWave() (wave int, state string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wave, p.waveState
}

// ApplyRunStatus applies a run-status event to the run summary.
func (p *PipelineState) ApplyRunStatus(status, reason string) {
	p.mu.Lock()
	if p.run == nil {
		p.mu.Unlock()
		return
	}
	p.run.Status, p.run.StopReason = status, reason
	p.mu.Unlock()
	p.notify()
}

// SetReport applies a run-report event or a direct report fetch.
func (p *PipelineState) SetReport(r runview.ReportBody) {
	p.mu.Lock()
	p.report = &r
	p.mu.Unlock()
	p.notify()
}

// Report returns the current report, or nil if none has arrived. The returned
// value is read-only: it is shared, and SetReport replaces it rather than
// mutating it.
func (p *PipelineState) Report() *runview.ReportBody {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.report
}

// SetMessage sets the text the panel shows in place of the run header (a
// schema mismatch); an empty string clears it.
func (p *PipelineState) SetMessage(m string) {
	p.mu.Lock()
	p.message = m
	p.mu.Unlock()
	p.notify()
}

// Message returns the panel message.
func (p *PipelineState) Message() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.message
}
