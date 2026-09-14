package orchestrate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/safety"
)

// TestTaskAgentHonorsConfiguredApproval is the regression guard for the defect
// where /project-execute ran completely ungated: Run built its agent with a
// hardcoded safety.Auto and then called SetApprovalMode("auto"), which assigns
// a.approve = safety.Auto and would discard any policy even once one was
// passed. A task agent must deny what the configured policy denies.
func TestTaskAgentHonorsConfiguredApproval(t *testing.T) {
	denyAll := func(tool, argsJSON string) bool { return false }
	r := NewRunner(&llm.Client{}, nil, t.TempDir(), "speed-x", "strong-x", 1, WithApproval(denyAll))

	ag := r.newTaskAgent("strong-x", "system prompt", 1)
	if ag.ApprovalAllows("run_shell", `{"cmd":"rm -rf /"}`) {
		t.Error("task agent allowed a tool call the configured policy denies — the policy stack is not reaching /project-execute")
	}
}

// TestTaskAgentDefaultsToAutoWhenNoApprovalConfigured pins the unattended
// contract: with no policy supplied the runner must not block on a prompt.
func TestTaskAgentDefaultsToAutoWhenNoApprovalConfigured(t *testing.T) {
	r := NewRunner(&llm.Client{}, nil, t.TempDir(), "speed-x", "strong-x", 1)

	ag := r.newTaskAgent("strong-x", "system prompt", 1)
	if !ag.ApprovalAllows("read_file", `{"path":"go.mod"}`) {
		t.Error("task agent denied a tool call with no policy configured; unattended runs must default to auto")
	}
}

// TestTaskAgentAttachesAuditLog guards the other half of the defect: the
// tamper-evident chain covered interactive sessions and was absent from the
// unattended run, which is precisely where provenance matters most.
func TestTaskAgentAttachesAuditLog(t *testing.T) {
	al := safety.NewAuditLog(filepath.Join(t.TempDir(), "audit.log"))
	r := NewRunner(&llm.Client{}, nil, t.TempDir(), "speed-x", "strong-x", 1, WithAuditLog(al))

	ag := r.newTaskAgent("strong-x", "system prompt", 1)
	if ag.AuditLog() == nil {
		t.Error("task agent has no audit log attached — unattended runs would leave no verifiable chain")
	}
}

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
