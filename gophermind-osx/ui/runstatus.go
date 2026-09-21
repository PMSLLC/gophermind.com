package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// StepCap is the number of tool calls one served turn is allowed, matching
// gophermind-server's llmMaxIter. Quoted in the status line so "step 4" means
// something: it is four of at most twelve before the turn is cut off.
//
// A breakdown is an interview and the model decides how many questions it
// asks, so there is no total step count to report for the run as a whole.
// This cap is the only real ceiling, and saying so beats inventing a
// percentage.
const StepCap = 12

// RunStatus is the live progress of a turn: what it is doing now, how many
// tool calls it has made, how long it has been going, and how fast tokens are
// coming back. Plain Go and safe from any goroutine, so the widget layer can
// render it on a timer while the SSE pump feeds it.
type RunStatus struct {
	mu       sync.Mutex
	label    string // what this run is, e.g. "breakdown"
	running  bool
	started  time.Time
	ended    time.Time
	step     int
	stepName string
	prompt   int
	output   int

	// now is injected so the tests can drive elapsed time.
	now func() time.Time

	onChange []func()
}

func NewRunStatus() *RunStatus {
	return &RunStatus{now: time.Now}
}

// OnChange registers a callback fired whenever the status changes.
func (s *RunStatus) OnChange(f func()) {
	s.mu.Lock()
	s.onChange = append(s.onChange, f)
	s.mu.Unlock()
}

func (s *RunStatus) notify() {
	s.mu.Lock()
	fns := append([]func(){}, s.onChange...)
	s.mu.Unlock()
	for _, f := range fns {
		f()
	}
}

// Begin starts a run named label, clearing any previous run's totals.
func (s *RunStatus) Begin(label string) {
	s.mu.Lock()
	s.label, s.running = label, true
	s.started, s.ended = s.now(), time.Time{}
	s.step, s.stepName = 0, ""
	s.prompt, s.output = 0, 0
	s.mu.Unlock()
	s.notify()
}

// StepStarted records that the run began a tool call named name.
func (s *RunStatus) StepStarted(name string) {
	s.mu.Lock()
	s.step++
	s.stepName = name
	s.mu.Unlock()
	s.notify()
}

// StepFinished records that the current tool call returned.
func (s *RunStatus) StepFinished() {
	s.mu.Lock()
	s.stepName = ""
	s.mu.Unlock()
	s.notify()
}

// Usage records the run's running token totals.
func (s *RunStatus) Usage(prompt, output int) {
	s.mu.Lock()
	s.prompt, s.output = prompt, output
	s.mu.Unlock()
	s.notify()
}

// End marks the run finished, keeping its totals for the summary.
func (s *RunStatus) End() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running, s.ended, s.stepName = false, s.now(), ""
	s.mu.Unlock()
	s.notify()
}

// Running reports whether a turn is in flight.
func (s *RunStatus) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Line renders the one-line status shown beneath the transcript.
func (s *RunStatus) Line() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started.IsZero() {
		return "Idle"
	}

	end := s.ended
	if s.running {
		end = s.now()
	}
	elapsed := end.Sub(s.started)

	var b strings.Builder
	if s.running {
		b.WriteString(s.label)
		if s.stepName != "" {
			fmt.Fprintf(&b, ": %s", s.stepName)
		}
		fmt.Fprintf(&b, " — step %d/%d", s.step, StepCap)
	} else {
		fmt.Fprintf(&b, "%s done — %d step", s.label, s.step)
		if s.step != 1 {
			b.WriteString("s")
		}
	}

	fmt.Fprintf(&b, ", %s", formatElapsed(elapsed))

	total := s.prompt + s.output
	if total > 0 {
		fmt.Fprintf(&b, ", %d tok", total)
	}
	// Throughput on output tokens: that is the part a reader watches appear,
	// and prompt tokens arrive in one lump rather than over the elapsed time.
	if secs := elapsed.Seconds(); secs > 0 && s.output > 0 {
		fmt.Fprintf(&b, ", %.1f tok/s", float64(s.output)/secs)
	}
	return b.String()
}

// formatElapsed renders a duration compactly: seconds under a minute, then
// minutes and seconds.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
