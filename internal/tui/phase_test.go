package tui

import (
	"os"
	"strings"
	"testing"

	"gophermind/internal/phaseflow"
)

// withWorkdir runs fn with the process working directory temporarily set to dir.
func withWorkdir(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prev) }()
	fn()
}

func TestHandlePhaseInitAndStatus(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		var m model
		reply, task := m.handlePhaseCommand("/phase init Demo")
		if task != "" {
			t.Fatalf("init should be a local command, got agent task %q", task)
		}
		if !strings.Contains(reply, "initialized") {
			t.Errorf("unexpected init reply: %q", reply)
		}
		if !phaseflow.New(dir).Initialized() {
			t.Error("project should be initialized on disk")
		}

		reply, task = m.handlePhaseCommand("/phase status")
		if task != "" || !strings.Contains(reply, "Demo") {
			t.Errorf("status reply = %q task = %q", reply, task)
		}
	})
}

func TestHandlePhaseAgenticStepYieldsPrompt(t *testing.T) {
	dir := t.TempDir()
	withWorkdir(t, dir, func() {
		var m model
		if _, task := m.handlePhaseCommand("/phase init Demo"); task != "" {
			t.Fatal("init should be local")
		}
		reply, task := m.handlePhaseCommand("/phase execute 1")
		if reply != "" {
			t.Errorf("agentic step should not produce a local reply, got %q", reply)
		}
		if !strings.Contains(task, "<phaseflow-context>") {
			t.Errorf("agentic task should be a seeded prompt, got %q", task[:min(80, len(task))])
		}
	})
}

func TestHandlePhaseHelp(t *testing.T) {
	withWorkdir(t, t.TempDir(), func() {
		var m model
		reply, task := m.handlePhaseCommand("/phase")
		if task != "" || !strings.Contains(reply, "PhaseFlow") {
			t.Errorf("help reply = %q", reply)
		}
	})
}
