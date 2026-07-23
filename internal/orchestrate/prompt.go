package orchestrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/internal/phaseflow"
)

// buildTaskPrompts assembles the system and user prompts for one task's fresh
// agent: system is the catalog agent's body plus the task's AgentAddendum (if
// any); user is a rendered instruction from the task's title/phase/description
// plus its acceptance criteria as a bullet list.
// maxContextRunes bounds each carried file. A task's own instructions must not
// be crowded out by run state, especially on a small local context window.
const maxContextRunes = 1500

// buildTaskPromptsWithContext is buildTaskPrompts plus the run state a cleared
// context cannot otherwise know: CONTEXT.md (what the previous tasks did) and
// PROJECT.md (the spec and conventions). Both are read fresh per task, so each
// task sees the state its predecessor left behind.
func buildTaskPromptsWithContext(t phaseflow.Task, catalogBody, root string) (system, user string) {
	system, user = buildTaskPrompts(t, catalogBody)

	var b strings.Builder
	if s := readCapped(filepath.Join(root, phaseflow.ContextDocName)); s != "" {
		b.WriteString("\n\nRun state so far (CONTEXT.md):\n\n")
		b.WriteString(s)
	}
	if s := readCapped(filepath.Join(root, phaseflow.ProjectDocName)); s != "" {
		b.WriteString("\n\nProject spec and conventions (PROJECT.md):\n\n")
		b.WriteString(s)
	}
	if b.Len() > 0 {
		// Context goes after the task so the instruction stays the first thing
		// the model reads, and survives a trim from the front.
		user += b.String()
	}
	return system, user
}

// readCapped reads a file and trims it to maxContextRunes, marking the cut so a
// partial file is not read as the whole one. A missing file yields "".
func readCapped(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	r := []rune(text)
	if len(r) > maxContextRunes {
		return string(r[:maxContextRunes]) + "\n…(truncated)…"
	}
	return text
}

func buildTaskPrompts(t phaseflow.Task, catalogBody string) (system, user string) {
	system = catalogBody
	if strings.TrimSpace(t.AgentAddendum) != "" {
		system = system + "\n\n" + t.AgentAddendum
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (phase %s): %s\n\n%s\n", t.ID, t.Phase, t.Title, t.Description)
	b.WriteString("\nAcceptance criteria:\n")
	for _, c := range t.AcceptanceCriteria {
		fmt.Fprintf(&b, "- %s\n", c)
	}
	user = b.String()
	return system, user
}
