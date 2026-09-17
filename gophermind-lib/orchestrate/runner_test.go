package orchestrate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/agent"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/phaseflow"
	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/tools"
)

// toolCallResp and finalResp build minimal SSE bodies for a scripted chat
// endpoint, mirroring agent/loop_test.go's own helpers of the same name
// (unexported, so duplicated rather than shared across packages).
func toolCallResp(id, name, args string) string {
	b, _ := json.Marshal(args)
	frame := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id +
		`","type":"function","function":{"name":"` + name + `","arguments":` + string(b) + `}}]}}]}`
	return "data: " + frame + "\n\ndata: [DONE]\n\n"
}

func finalResp(text string) string {
	b, _ := json.Marshal(text)
	frame := `{"choices":[{"delta":{"content":` + string(b) + `},"finish_reason":"stop"}]}`
	return "data: " + frame + "\n\ndata: [DONE]\n\n"
}

// TestTaskAgentHonorsConfiguredApproval is the regression guard for the defect
// where /project-execute ran completely ungated: Run built its agent with a
// hardcoded safety.Auto and then called SetApprovalMode("auto"), which assigns
// a.approve = safety.Auto and would discard any policy even once one was
// passed. A task agent must deny what the configured policy denies.
func TestTaskAgentHonorsConfiguredApproval(t *testing.T) {
	denyAll := func(tool, argsJSON string) bool { return false }
	r := NewRunner(&llm.Client{}, nil, t.TempDir(), "speed-x", "strong-x", 1, WithApproval(denyAll))

	ag := r.newTaskAgent("01-01", "strong-x", "system prompt", 1)
	if ag.ApprovalAllows("run_shell", `{"cmd":"rm -rf /"}`) {
		t.Error("task agent allowed a tool call the configured policy denies — the policy stack is not reaching /project-execute")
	}
}

// TestTaskAgentDefaultsToAutoWhenNoApprovalConfigured pins the unattended
// contract: with no policy supplied the runner must not block on a prompt.
func TestTaskAgentDefaultsToAutoWhenNoApprovalConfigured(t *testing.T) {
	r := NewRunner(&llm.Client{}, nil, t.TempDir(), "speed-x", "strong-x", 1)

	ag := r.newTaskAgent("01-01", "strong-x", "system prompt", 1)
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

	ag := r.newTaskAgent("01-01", "strong-x", "system prompt", 1)
	if ag.AuditLog() == nil {
		t.Error("task agent has no audit log attached — unattended runs would leave no verifiable chain")
	}
}

// TestTaskEventForwarderFiltersToToolEvents is the deferred follow-up from
// feat/project-execute (#3): the spec asked for the agent's events, prefixed
// per task, so the TUI can show live tool activity, but the runner passed a
// hardcoded nil onEvent and no task's activity was ever seen until it
// finished. taskEventForwarder is the piece that tags each event with its
// task and drops the high-frequency ones ("token" fires per streamed
// character; a wave of concurrent tasks would flood the transcript).
func TestTaskEventForwarderFiltersToToolEvents(t *testing.T) {
	var got []TaskEvent
	fwd := taskEventForwarder("02-01", func(e TaskEvent) { got = append(got, e) })

	fwd(agent.Event{Type: "token", Text: "partial"})
	fwd(agent.Event{Type: "usage"})
	fwd(agent.Event{Type: "assistant", Text: "final answer"})
	fwd(agent.Event{Type: "tool_call", Name: "read_file", Text: `{"path":"x.txt"}`})
	fwd(agent.Event{Type: "tool_result", Name: "read_file", Text: "file contents"})

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (tool_call, tool_result only): %+v", len(got), got)
	}
	if got[0].TaskID != "02-01" || got[0].Type != "tool_call" || got[0].Name != "read_file" {
		t.Errorf("first event = %+v, want tagged tool_call for read_file", got[0])
	}
	if got[1].TaskID != "02-01" || got[1].Type != "tool_result" {
		t.Errorf("second event = %+v, want tagged tool_result", got[1])
	}
}

// TestTaskEventForwarderNilSinkNoop guards the default (no WithEvents
// configured) path: forwarding must be a safe no-op, not a nil-pointer panic.
func TestTaskEventForwarderNilSinkNoop(t *testing.T) {
	fwd := taskEventForwarder("02-01", nil)
	fwd(agent.Event{Type: "tool_call", Name: "read_file"})
}

// TestNewTaskAgentStreamsToolEventsTaggedWithTaskID is the end-to-end version
// of the two tests above: a real agent.Send with a tool-call round trip must
// reach WithEvents' sink, tagged with the task ID newTaskAgent was built for.
func TestNewTaskAgentStreamsToolEventsTaggedWithTaskID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	var i int
	bodies := []string{
		toolCallResp("call_1", "read_file", `{"path":"x.txt"}`),
		finalResp("done"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := bodies[len(bodies)-1]
		if i < len(bodies) {
			body = bodies[i]
			i++
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "m", 5*time.Second, false)
	reg := tools.NewRegistry(tools.ReadFile(dir))
	var got []TaskEvent
	r := NewRunner(client, reg, dir, "speed-x", "strong-x", 25, WithEvents(func(e TaskEvent) { got = append(got, e) }))

	ag := r.newTaskAgent("02-01", "m", "system", 25)
	if _, err := ag.Send(context.Background(), "read x.txt"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (tool_call, tool_result): %+v", len(got), got)
	}
	for _, e := range got {
		if e.TaskID != "02-01" {
			t.Errorf("event %+v not tagged with task 02-01", e)
		}
	}
	if got[0].Type != "tool_call" || got[0].Name != "read_file" {
		t.Errorf("first event = %+v, want tool_call for read_file", got[0])
	}
	if got[1].Type != "tool_result" {
		t.Errorf("second event = %+v, want tool_result", got[1])
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
