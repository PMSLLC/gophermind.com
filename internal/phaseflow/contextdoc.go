package phaseflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ContextDocName is the running-state file refreshed after every task. Each
// task runs in a cleared agent context (see orchestrate.NewRunner), so this
// file — together with PROJECT.md — is how the next task learns where the run
// got to. Generated content lives in a delimited block: CONTEXT.md is also a
// hand-written handoff file and the rest of it is never touched.
const ContextDocName = "CONTEXT.md"

const (
	contextBeginMarker = "<!-- gophermind:state:begin -->"
	contextEndMarker   = "<!-- gophermind:state:end -->"
)

// ContextDocPath is the absolute path to the context doc under root.
func ContextDocPath(root string) string { return filepath.Join(root, ContextDocName) }

// UpsertContextDoc writes body into the managed block of root's CONTEXT.md,
// with the same rules as UpsertProjectDoc: replace between markers, append when
// they are absent, create the file when it does not exist.
func UpsertContextDoc(root, body string) error {
	path := ContextDocPath(root)
	block := contextBeginMarker + "\n" + strings.TrimRight(body, "\n") + "\n" + contextEndMarker

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", ContextDocName, err)
		}
		return writeFileAtomic(path, block+"\n")
	}

	text := string(existing)
	start := strings.Index(text, contextBeginMarker)
	end := strings.Index(text, contextEndMarker)
	if start >= 0 && end > start {
		text = text[:start] + block + text[end+len(contextEndMarker):]
	} else {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n" + block + "\n"
	}
	return writeFileAtomic(path, text)
}

// RenderContextDocBody renders the run's current state: what just finished,
// what is left, and what runs next. It is written for a reader with no memory
// of the run, because that is exactly what the next task is.
func RenderContextDocBody(name string, a *Assignments, last TaskOutcome) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Run state — %s\n\n", name)
	b.WriteString("Refreshed after every task. Each task runs in a cleared context and reads\n")
	b.WriteString("this block plus PROJECT.md to pick up where the previous one stopped.\n\n")

	if a == nil || len(a.Tasks) == 0 {
		b.WriteString("No tasks planned.\n")
		return b.String()
	}

	var done, failed, pending int
	var next *Task
	tasks := append([]Task(nil), a.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	for i := range tasks {
		switch tasks[i].Status {
		case StatusDone, StatusCorrected:
			done++
		case StatusFailed:
			failed++
		case StatusPending:
			pending++
			if next == nil {
				next = &tasks[i]
			}
		}
	}

	fmt.Fprintf(&b, "%d done, %d failed, %d pending.\n\n", done, failed, pending)

	if last.ID != "" {
		fmt.Fprintf(&b, "Last finished: %s — %s", last.ID, last.Status)
		if strings.TrimSpace(last.Detail) != "" {
			fmt.Fprintf(&b, " (%s)", oneLine(last.Detail))
		}
		b.WriteString("\n\n")
	}

	if next != nil {
		fmt.Fprintf(&b, "Next: %s (phase %s) — %s\n\n", next.ID, next.Phase, next.Title)
	} else {
		b.WriteString("Next: nothing pending.\n\n")
	}

	b.WriteString("| Task | Phase | Title | Status |\n|---|---|---|---|\n")
	for _, t := range tasks {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", t.ID, t.Phase, t.Title, t.Status)
	}
	return b.String()
}

// oneLine flattens a multi-line detail so it cannot break the summary line.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len([]rune(s)) > max {
		return string([]rune(s)[:max]) + "…"
	}
	return s
}
