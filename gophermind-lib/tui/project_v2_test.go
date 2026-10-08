package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/gophermind-lib/briefv2/projectrun"
)

// fakeProject swaps projectRunFn for fn and records the Options it saw.
func fakeProject(t *testing.T, fn func(ctx context.Context, o projectrun.Options) projectrun.Result) *[]projectrun.Options {
	t.Helper()
	oldRun, oldEnv := projectRunFn, projectEnvFn
	var mu sync.Mutex
	var seen []projectrun.Options
	projectRunFn = func(ctx context.Context, o projectrun.Options, _ projectrun.Env) projectrun.Result {
		mu.Lock()
		seen = append(seen, o)
		mu.Unlock()
		return fn(ctx, o)
	}
	projectEnvFn = func() projectrun.Env { return projectrun.Env{} }
	t.Cleanup(func() { projectRunFn, projectEnvFn = oldRun, oldEnv })
	return &seen
}

// pumpProject feeds messages from m.sub through Update until the done message.
func pumpProject(t *testing.T, m model) model {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-m.sub:
			nm, _ := m.Update(msg)
			m = nm.(model)
			if _, ok := msg.(projectV2DoneMsg); ok {
				return m
			}
		case <-deadline:
			t.Fatal("run never finished")
		}
	}
}

func TestSlashProjectRunsProjectrun(t *testing.T) {
	seen := fakeProject(t, func(_ context.Context, o projectrun.Options) projectrun.Result {
		o.Err.Write([]byte("preflight ok\nplanning\tstarted\n"))
		o.Out.Write([]byte("partial tail"))
		return projectrun.Result{ExitCode: 0, Status: "verified"}
	})
	m := testModel(t)
	m.input.SetValue("/project brief.md --repo /tmp/r")
	m, _ = m.handleSubmit()
	if !m.projV2 || m.st != stateWorking || m.cancel == nil {
		t.Fatalf("not working: projV2=%v st=%v", m.projV2, m.st)
	}
	m = pumpProject(t, m)
	for _, want := range []string{"preflight ok", "planning started", "partial tail", "project: exit 0 (verified)"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript missing %q: %q", want, m.content)
		}
	}
	if len(*seen) != 1 || (*seen)[0].BriefPath != "brief.md" || (*seen)[0].Repo != "/tmp/r" || (*seen)[0].In != nil || (*seen)[0].Attended {
		t.Errorf("options = %+v", *seen)
	}
	if m.projV2 || m.st != stateIdle || m.cancel != nil {
		t.Errorf("not cleared: projV2=%v st=%v", m.projV2, m.st)
	}
}

func TestSlashProjectRefusesAttended(t *testing.T) {
	seen := fakeProject(t, func(context.Context, projectrun.Options) projectrun.Result { return projectrun.Result{} })
	m := testModel(t)
	m.input.SetValue("/project brief.md --attended")
	m, _ = m.handleSubmit()
	if m.projV2 || len(*seen) != 0 || !strings.Contains(m.content, "attended") {
		t.Errorf("projV2=%v calls=%d content=%q", m.projV2, len(*seen), m.content)
	}
}

func TestSlashProjectRejectsV1Form(t *testing.T) {
	seen := fakeProject(t, func(context.Context, projectrun.Options) projectrun.Result { return projectrun.Result{} })
	m := testModel(t)
	m.input.SetValue("/project name brief.md")
	m, _ = m.handleSubmit()
	if m.projV2 || len(*seen) != 0 || !strings.Contains(m.content, "the v1 form") {
		t.Errorf("projV2=%v calls=%d content=%q", m.projV2, len(*seen), m.content)
	}
}

func TestSlashProjectEscCancels(t *testing.T) {
	started := make(chan struct{})
	fakeProject(t, func(ctx context.Context, o projectrun.Options) projectrun.Result {
		close(started)
		<-ctx.Done()
		o.Err.Write([]byte("report written\n"))
		return projectrun.Result{ExitCode: projectrun.ExitInterrupted, Status: "interrupted"}
	})
	m := testModel(t)
	m.input.SetValue("/project brief.md")
	m, _ = m.handleSubmit()
	<-started
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = pumpProject(t, nm.(model))
	for _, want := range []string{"report written", "interrupted; continue with /project <brief> --resume"} {
		if !strings.Contains(m.content, want) {
			t.Errorf("transcript missing %q: %q", want, m.content)
		}
	}
	if m.projV2 || m.st != stateIdle {
		t.Errorf("not cleared")
	}
}

func TestSlashProjectSecondRunRefused(t *testing.T) {
	release := make(chan struct{})
	seen := fakeProject(t, func(ctx context.Context, _ projectrun.Options) projectrun.Result {
		<-release
		return projectrun.Result{Status: "verified"}
	})
	m := testModel(t)
	m.input.SetValue("/project brief.md")
	m, _ = m.handleSubmit()
	m, _ = m.handleProjectV2Command("/project other.md")
	if !strings.Contains(m.content, "a project run is already working; wait for it or press Esc") {
		t.Errorf("content = %q", m.content)
	}
	close(release)
	m = pumpProject(t, m)
	if len(*seen) != 1 {
		t.Errorf("calls = %d, want 1", len(*seen))
	}
}

func TestSlashProjectLinesAreBounded(t *testing.T) {
	fakeProject(t, func(_ context.Context, o projectrun.Options) projectrun.Result {
		o.Err.Write([]byte(strings.Repeat("x", 5000) + "\n"))
		return projectrun.Result{Status: "verified"}
	})
	m := testModel(t)
	m, _ = m.handleProjectV2Command("/project brief.md")
	m = pumpProject(t, m)
	if strings.Contains(m.content, strings.Repeat("x", projectV2LineMax+1)) || !strings.Contains(m.content, "...") {
		t.Errorf("line not bounded (len %d)", len(m.content))
	}
}

func TestSlashProjectRegistry(t *testing.T) {
	found := false
	for _, c := range slashCommands {
		if c.Name == "/project" {
			found = true
			if c.Arg != "<brief> [flags]" || c.Desc != "plan and build a v2 brief in one run, no second command" {
				t.Errorf("entry = %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("/project not in registry")
	}
	if !strings.Contains(helpLine(), "/project <brief> [flags]") {
		t.Errorf("help lacks /project: %q", helpLine())
	}
	m := testModel(t)
	m.input.SetValue("/help")
	m2, _ := m.handleSubmit()
	if !strings.Contains(m2.content, "/project <brief> [flags]") {
		t.Errorf("/help lacks /project")
	}
}
