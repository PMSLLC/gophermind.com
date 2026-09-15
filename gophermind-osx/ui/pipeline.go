package ui

import (
	"sync"
	"time"

	"gophermind/gophermind-lib/phaseflow"
)

// PipelineState is the pipeline panel's plain-Go state (.planning/tasks/
// 04-06.json): the tasks last fetched from GET /pipeline/state, kept live
// by applying GET /pipeline/events frames on top, plus the current wave and
// the run report once one arrives. Same split as Transcript/ApprovalTracker/
// ModelPickerState: mutex-protected, cgo-free, OnChange-driven.
type PipelineState struct {
	mu          sync.Mutex
	tasks       []phaseflow.Task
	generatedAt time.Time
	wave        int
	waveState   string
	report      *phaseflow.RunReport
	onChange    func()
}

// NewPipelineState returns an empty pipeline state.
func NewPipelineState() *PipelineState {
	return &PipelineState{}
}

// OnChange registers f to be called after every mutation. Same
// non-blocking, non-reentrant contract as Transcript.OnChange.
func (p *PipelineState) OnChange(f func()) {
	p.mu.Lock()
	p.onChange = f
	p.mu.Unlock()
}

func (p *PipelineState) notify() {
	if p.onChange != nil {
		p.onChange()
	}
}

// SetTasks replaces the task list wholesale -- the GET /pipeline/state
// snapshot a caller fetches before subscribing to live events.
func (p *PipelineState) SetTasks(tasks []phaseflow.Task, generatedAt time.Time) {
	p.mu.Lock()
	p.tasks = append([]phaseflow.Task(nil), tasks...)
	p.generatedAt = generatedAt
	p.mu.Unlock()
	p.notify()
}

// Tasks returns a snapshot copy of the current task list.
func (p *PipelineState) Tasks() []phaseflow.Task {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]phaseflow.Task, len(p.tasks))
	copy(out, p.tasks)
	return out
}

// GeneratedAt returns the timestamp of the last SetTasks snapshot.
func (p *PipelineState) GeneratedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.generatedAt
}

// findTask returns a pointer into p.tasks for id, or nil. Caller must hold
// p.mu.
func (p *PipelineState) findTask(id string) *phaseflow.Task {
	for i := range p.tasks {
		if p.tasks[i].ID == id {
			return &p.tasks[i]
		}
	}
	return nil
}

// ApplyTaskStatus applies a "task-status" event: updates id's Status and
// Wave if id is already known. An id this state hasn't seen yet (e.g. an
// event arriving before the first SetTasks snapshot) is ignored rather
// than fabricating a new task with missing fields -- and since nothing
// changed, OnChange does not fire.
func (p *PipelineState) ApplyTaskStatus(id, status string, wave int) {
	p.mu.Lock()
	t := p.findTask(id)
	if t == nil {
		p.mu.Unlock()
		return
	}
	t.Status = status
	t.Wave = wave
	p.mu.Unlock()
	p.notify()
}

// ApplyTaskAttempt applies a "task-attempt" event: appends a to taskID's
// attempt history. Same unknown-id handling as ApplyTaskStatus.
func (p *PipelineState) ApplyTaskAttempt(taskID string, a phaseflow.Attempt) {
	p.mu.Lock()
	t := p.findTask(taskID)
	if t == nil {
		p.mu.Unlock()
		return
	}
	t.RecordAttempt(a)
	p.mu.Unlock()
	p.notify()
}

// ApplyWaveChanged applies a "wave-changed" event.
func (p *PipelineState) ApplyWaveChanged(wave int, state string) {
	p.mu.Lock()
	p.wave = wave
	p.waveState = state
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

// SetReport applies a "run-report" event (or a direct GET /pipeline/report
// fetch): the run's final report.
func (p *PipelineState) SetReport(r phaseflow.RunReport) {
	p.mu.Lock()
	p.report = &r
	p.mu.Unlock()
	p.notify()
}

// Report returns the current run report, or nil if none has arrived yet.
func (p *PipelineState) Report() *phaseflow.RunReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.report
}
