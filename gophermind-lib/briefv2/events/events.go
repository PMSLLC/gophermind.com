// Package events is how the engine reports progress. The engine writes events
// to a Sink and never reads from one; the terminal, a file, or (later) the
// desktop app implement the Sink.
package events

import (
	"sync"
	"time"

	"gophermind/gophermind-lib/briefv2/ledger"
)

// Event kinds.
const (
	KindStageStarted   = "stage_started"
	KindStageFinished  = "stage_finished"
	KindWaitingOnHuman = "waiting_on_human"
	KindCall           = "call"
	KindLedgerError    = "ledger_error"
	KindWarning        = "warning"
	KindCoverageGap    = "coverage_gap"
)

// Event is one progress report. Call is set only for KindCall and carries the
// ledger row of the attempt.
type Event struct {
	Kind    string
	Stage   string
	NodeID  string
	Message string
	At      time.Time
	Call    *ledger.Call
}

// Sink receives events. Implementations must be safe for concurrent use.
type Sink interface{ Emit(Event) }

type nop struct{}

func (nop) Emit(Event) {}

// Nop discards every event.
var Nop Sink = nop{}

// Collector keeps every event in memory, for tests and for the status command.
type Collector struct {
	mu     sync.Mutex
	events []Event
}

func NewCollector() *Collector { return &Collector{} }

// Emit stamps the event with the current time when At is zero and stores it.
func (c *Collector) Emit(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

// Events returns a copy of everything collected so far, oldest first.
func (c *Collector) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Event(nil), c.events...)
}

// OfKind returns the collected events of one kind, oldest first.
func (c *Collector) OfKind(kind string) []Event {
	var out []Event
	for _, e := range c.Events() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
