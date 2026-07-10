package trace

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTracerEmitsSpan(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)
	end := tr.Start("turn")
	time.Sleep(2 * time.Millisecond)
	end(map[string]string{"model": "m1"})

	var s Span
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &s); err != nil {
		t.Fatalf("span not valid JSON: %v\n%s", err, buf.String())
	}
	if s.Name != "turn" {
		t.Errorf("name = %q", s.Name)
	}
	if s.DurationMs < 1 {
		t.Errorf("duration should be >= 1ms, got %d", s.DurationMs)
	}
	if s.Attrs["model"] != "m1" {
		t.Errorf("attrs = %v", s.Attrs)
	}
}

func TestNilTracerNoop(t *testing.T) {
	var tr *Tracer // nil
	end := tr.Start("x")
	end(nil) // must not panic
	if New(nil) != nil {
		t.Error("New(nil) should return a nil tracer")
	}
}
