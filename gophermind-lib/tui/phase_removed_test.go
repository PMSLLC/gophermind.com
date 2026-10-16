package tui

import (
	"strings"
	"testing"
)

func TestPhaseRemovedNoticeText(t *testing.T) {
	n := PhaseRemovedNotice()
	for _, want := range []string{"gophermind phase was removed in " + PhaseRemovedIn, `"gophermind brief plan"`, `"gophermind brief run"`, "/project", ".planning/ folders are not read"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice lacks %q: %s", want, n)
		}
	}
	if strings.ContainsRune(n, '—') {
		t.Error("the notice must not contain an em dash")
	}
}

func TestPhaseTypedInASessionPrintsTheNoticeAndSendsNothing(t *testing.T) {
	m := sizedModel(t, 80, 24)
	next := submit(t, m, "/phase status")
	if !strings.Contains(next.content, PhaseRemovedNotice()) {
		t.Errorf("the transcript lacks the notice:\n%s", next.content)
	}
	if next.st != stateIdle {
		t.Errorf("state = %v: a retired command must not start an agent turn", next.st)
	}
}

func TestRetiredPlannerCommandsPrintOneLineAndSendNothing(t *testing.T) {
	for _, name := range []string{"/plan-v1", "/plan-v1-execute", "/questions", "/project-execute"} {
		m := sizedModel(t, 80, 24)
		next := submit(t, m, name+" demo something")
		want := name + " was removed in " + PhaseRemovedIn + "; use /project <brief>"
		if RetiredCommandNotice(name) != want {
			t.Errorf("notice for %s = %q, want %q", name, RetiredCommandNotice(name), want)
		}
		if !strings.Contains(next.content, want) {
			t.Errorf("%s: the transcript lacks the notice:\n%s", name, next.content)
		}
		if next.st != stateIdle {
			t.Errorf("%s: state = %v: a retired command must not start an agent turn", name, next.st)
		}
	}
}

func TestRegistryHasNoRetiredCommands(t *testing.T) {
	for _, name := range commandNames() {
		switch name {
		case "/phase", "/project-execute", "/plan-v1", "/plan-v1-execute", "/questions":
			t.Errorf("the registry still lists %s", name)
		}
	}
	if !strings.Contains(helpLine(), "/project") {
		t.Error("/project must stay in the help line")
	}
}

// submit runs one line through handleSubmit.
func submit(t *testing.T, m model, line string) model {
	t.Helper()
	m.input.SetValue(line)
	next, _ := m.handleSubmit()
	return next
}
