package orchestrate

import (
	"context"
	"strings"
	"testing"

	"gophermind/internal/phaseflow"
)

// TestRunFailsWhenAgentNotAssigned verifies a task with no agent assigned
// fails fast (status=failed, err=nil) without needing an LLM.
func TestRunFailsWhenAgentNotAssigned(t *testing.T) {
	root := t.TempDir()
	r := NewRunner(nil, nil, root, "speed-x", "strong-x", 1)

	status, detail, err := r.Run(context.Background(), phaseflow.Task{ID: "01-01", Agent: ""})
	if err != nil {
		t.Fatalf("Run returned err=%v, want nil (failed status only)", err)
	}
	if status != phaseflow.StatusFailed {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusFailed)
	}
	if !strings.Contains(detail, "no agent assigned") {
		t.Errorf("detail = %q, want it to mention %q", detail, "no agent assigned")
	}
}

// TestRunFailsWhenCatalogAgentNotFound verifies a task whose agent isn't in
// the catalog (or the catalog dir is absent) fails fast rather than running
// under a generic default system prompt.
func TestRunFailsWhenCatalogAgentNotFound(t *testing.T) {
	root := t.TempDir() // no .planning/agents/ at all
	r := NewRunner(nil, nil, root, "speed-x", "strong-x", 1)

	status, detail, err := r.Run(context.Background(), phaseflow.Task{ID: "01-01", Agent: "ghost-agent"})
	if err != nil {
		t.Fatalf("Run returned err=%v, want nil (failed status only)", err)
	}
	if status != phaseflow.StatusFailed {
		t.Errorf("status = %q, want %q", status, phaseflow.StatusFailed)
	}
	if !strings.Contains(detail, "not found") {
		t.Errorf("detail = %q, want it to mention %q", detail, "not found")
	}
}
