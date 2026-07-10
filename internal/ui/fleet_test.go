package ui

import (
	"strings"
	"testing"
)

func TestRenderFleetStatus(t *testing.T) {
	workers := []WorkerStatus{
		{ID: 2, Task: "refactor db", State: "running", Tokens: 500, CostUSD: 0.02},
		{ID: 1, Task: "write tests", State: "done", Tokens: 300, CostUSD: 0.01},
	}
	out := RenderFleetStatus(workers)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Sorted by id: worker 1 before worker 2 (after the header line).
	if !strings.HasPrefix(lines[1], "1 ") && !strings.HasPrefix(strings.TrimSpace(lines[1]), "1") {
		t.Errorf("workers should be sorted by id:\n%s", out)
	}
	// Totals footer: 800 tokens, $0.03.
	if !strings.Contains(out, "800 tokens") || !strings.Contains(out, "0.0300") {
		t.Errorf("totals footer wrong:\n%s", out)
	}
	if !strings.Contains(out, "write tests") || !strings.Contains(out, "running") {
		t.Errorf("worker rows missing content:\n%s", out)
	}
}
