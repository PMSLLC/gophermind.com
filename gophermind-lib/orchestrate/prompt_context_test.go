package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/phaseflow"
)

func task() phaseflow.Task {
	return phaseflow.Task{ID: "02-03", Phase: "2", Title: "Wire the handler",
		Description: "Do the thing.", AcceptanceCriteria: []string{"it works"}}
}

// TestTaskPromptCarriesRunState: each task starts from a cleared context, so
// the prompt itself must carry where the run is and what the project is.
func TestTaskPromptCarriesRunState(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, phaseflow.ContextDocName), []byte("STATE-MARKER: 01-01 done"), 0o644)
	os.WriteFile(filepath.Join(root, phaseflow.ProjectDocName), []byte("PROJECT-MARKER: build with make"), 0o644)

	_, user := buildTaskPromptsWithContext(task(), "catalog body", root)

	if !strings.Contains(user, "STATE-MARKER") {
		t.Errorf("prompt missing CONTEXT.md state:\n%s", user)
	}
	if !strings.Contains(user, "PROJECT-MARKER") {
		t.Errorf("prompt missing PROJECT.md:\n%s", user)
	}
	// The task itself must still be the instruction.
	if !strings.Contains(user, "Wire the handler") || !strings.Contains(user, "it works") {
		t.Errorf("task detail lost:\n%s", user)
	}
}

// TestTaskPromptIncludesPhaseflowContextPreamble is the deferred follow-up
// from feat/project-execute (#5): a task's prompt never carried the
// <phaseflow-context> preamble /phase's interactive steps get (config flags,
// roadmap progress), so an executor task never knew e.g. whether a verifier
// gate was even on, or where it sat in the plan. It must appear ahead of the
// task instruction, the same "preamble" position it has everywhere else.
func TestTaskPromptIncludesPhaseflowContextPreamble(t *testing.T) {
	root := t.TempDir()
	e := phaseflow.New(root)
	if err := e.Init("Demo"); err != nil {
		t.Fatal(err)
	}

	_, user := buildTaskPromptsWithContext(task(), "catalog body", root)

	if !strings.Contains(user, "<phaseflow-context>") {
		t.Errorf("prompt missing phaseflow-context preamble:\n%s", user)
	}
	if !strings.Contains(user, `"02-03"`) {
		t.Errorf("preamble does not name the task:\n%s", user)
	}
	if strings.Index(user, "<phaseflow-context>") > strings.Index(user, "Wire the handler") {
		t.Errorf("preamble must precede the task instruction:\n%s", user)
	}
}

// TestTaskPromptWithoutContextFiles keeps a bare project working.
func TestTaskPromptWithoutContextFiles(t *testing.T) {
	_, user := buildTaskPromptsWithContext(task(), "catalog body", t.TempDir())
	if !strings.Contains(user, "Wire the handler") {
		t.Errorf("task detail missing:\n%s", user)
	}
	if strings.Contains(user, "Run state so far") {
		t.Errorf("empty context heading emitted:\n%s", user)
	}
}

// TestTaskPromptContextIsBounded: these files grow, and a small local context
// window must not be consumed by them.
func TestTaskPromptContextIsBounded(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", 40_000)
	os.WriteFile(filepath.Join(root, phaseflow.ContextDocName), []byte(big), 0o644)
	os.WriteFile(filepath.Join(root, phaseflow.ProjectDocName), []byte(big), 0o644)

	_, user := buildTaskPromptsWithContext(task(), "catalog body", root)
	if len([]rune(user)) > maxContextRunes*2+2000 {
		t.Errorf("prompt is %d runes; context was not bounded", len([]rune(user)))
	}
}
