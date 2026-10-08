package projectrun

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"gophermind/gophermind-lib/briefv2/events"
)

// Sink prints progress lines and sums the planner's warning counts. It never
// prints Event.Call.
type Sink struct {
	mu     sync.Mutex
	w      io.Writer
	counts Counts
}

// NewSink returns a Sink writing to w.
func NewSink(w io.Writer) *Sink { return &Sink{w: w} }

// Counts returns the warning counts summed so far. DuplicatesIgnored is filled
// by Run from planner.IgnoredDuplicates, not here.
func (s *Sink) Counts() Counts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts
}

// Emit implements events.Sink.
func (s *Sink) Emit(e events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Kind == events.KindWarning {
		s.count(e.Message)
	}
	msg := oneLine(e.Message)
	var line string
	switch e.Kind {
	case events.KindStageStarted:
		line = oneLine(e.Stage) + ": started"
	case events.KindStageFinished:
		line = oneLine(e.Stage) + ": done"
	case events.KindWarning, events.KindLedgerError:
		line = "warning: " + msg
	case events.KindCoverageGap:
		line = "coverage gap, " + msg
	default:
		if msg == "" {
			return
		}
		who := e.NodeID
		if who == "" {
			who = e.Stage
		}
		line = oneLine(who) + ": " + oneLine(e.Kind) + " " + msg
	}
	if s.w != nil {
		fmt.Fprintln(s.w, line)
	}
}

// count adds the integer after a planner warning prefix to its counter.
func (s *Sink) count(msg string) {
	for _, p := range []struct {
		prefix string
		to     *int
	}{
		{"leaf_defaulted: ", &s.counts.LeafDefaulted},
		{"doc_defaulted: ", &s.counts.DocDefaulted},
		{"leaf_normalized: ", &s.counts.LeafNormalized},
		{"outline_id_normalized: ", &s.counts.OutlineIDNormalized},
	} {
		rest, ok := strings.CutPrefix(msg, p.prefix)
		if !ok {
			continue
		}
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if n, err := strconv.Atoi(rest[:end]); err == nil {
			*p.to += n
		}
		return
	}
}

// Counts are the planner warning counts of a run.
type Counts struct {
	LeafDefaulted       int `json:"leaf_defaulted"`
	DocDefaulted        int `json:"doc_defaulted"`
	LeafNormalized      int `json:"leaf_normalized"`
	OutlineIDNormalized int `json:"outline_id_normalized"`
	DuplicatesIgnored   int `json:"duplicates_ignored"`
}
