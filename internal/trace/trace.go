// Package trace provides minimal, dependency-free tracing: spans (name, start,
// duration, attributes) emitted as JSON lines, so turns/tool-calls/LLM requests
// can be inspected for latency without pulling in the full OpenTelemetry SDK.
package trace

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Span is a timed operation with optional attributes.
type Span struct {
	Name       string            `json:"name"`
	Start      time.Time         `json:"start"`
	DurationMs int64             `json:"duration_ms"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

// Tracer writes completed spans as JSON lines to a writer (thread-safe).
type Tracer struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a Tracer writing to w, or nil when w is nil (tracing disabled).
func New(w io.Writer) *Tracer {
	if w == nil {
		return nil
	}
	return &Tracer{w: w}
}

// Start begins a span; call the returned func (optionally with attributes) to
// end it and emit it. Nil-safe: a nil Tracer returns a no-op ender.
func (t *Tracer) Start(name string) func(attrs map[string]string) {
	if t == nil {
		return func(map[string]string) {}
	}
	start := time.Now()
	return func(attrs map[string]string) {
		s := Span{Name: name, Start: start, DurationMs: time.Since(start).Milliseconds(), Attrs: attrs}
		b, err := json.Marshal(s)
		if err != nil {
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		t.w.Write(append(b, '\n'))
	}
}
