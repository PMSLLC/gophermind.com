package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/internal/phaseflow"
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
