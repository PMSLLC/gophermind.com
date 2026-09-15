package main

// Like chatview_test.go/rightpanel_test.go/sessionlist_test.go, this only
// proves the pipeline panel's widgets build and respond to state changes
// without panicking, routed through runOnUIThread for the same
// AppKit-single-thread reason -- whether it actually LOOKS right needs a
// human at a real window.
//
// goPipelinePickBriefClicked is never invoked here, same as
// sessionlist_test.go never invokes goSessionChooseRootClicked: both call
// a real native dialog (uiOpenFile/uiOpenFolder) that would block waiting
// for a human, not something a test can safely drive.

import (
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/phaseflow"
	appui "gophermind/gophermind-osx/ui"
)

func TestNewChatWindow_BuildsPipelinePanelWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var err error
	var pipelineNil bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		pipelineNil = cw.Pipeline == nil
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if pipelineNil {
		t.Fatal("ChatWindow.Pipeline is nil")
	}
}

func TestPipelinePanel_RefreshRebuildsTaskListOnStateChange(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		pp := newPipelinePanel(state, app.window, nil, nil, nil)
		attachControlForTest(app.window, pp.Control())

		// state.OnChange dispatches pp.refresh via queueMain, which only
		// actually runs once uiMain is pumping (see chatview_test.go's
		// doc comment on the same limit) -- so this calls refresh()
		// directly, the same thing queueMain would eventually run, rather
		// than relying on the queue draining here.
		state.SetTasks([]phaseflow.Task{{ID: "01-01", Status: phaseflow.StatusRunning}}, time.Time{})
		pp.refresh()
		if len(pp.taskLabels) != 1 {
			t.Errorf("taskLabels after one task = %d, want 1", len(pp.taskLabels))
		}

		state.SetTasks([]phaseflow.Task{{ID: "01-01"}, {ID: "01-02"}}, time.Time{})
		pp.refresh()
		if len(pp.taskLabels) != 2 {
			t.Errorf("taskLabels after two tasks = %d, want 2", len(pp.taskLabels))
		}
	})
}

func TestPipelinePanel_StartClickedWithNilFuncDoesNotPanic(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		pp := newPipelinePanel(state, app.window, nil, nil, nil)
		attachControlForTest(app.window, pp.Control())
		pp.briefContent = "a brief" // simulate a picked brief without opening the dialog
		pp.doStart()                // startBreakdown == nil: must not panic
	})
}

func TestFormatTaskLine_IncludesStatusAndLatestAttempt(t *testing.T) {
	task := phaseflow.Task{
		ID: "01-01", Status: phaseflow.StatusFailed, Wave: 2,
		Attempts: []phaseflow.Attempt{{Model: "strong", Duration: "2m", Verdict: "fail"}},
	}
	line := formatTaskLine(task)
	for _, want := range []string{"failed", "01-01", "strong", "2m", "fail"} {
		if !strings.Contains(line, want) {
			t.Errorf("formatTaskLine(%+v) = %q, missing %q", task, line, want)
		}
	}
}

func TestFormatRunReport_IncludesModelStats(t *testing.T) {
	report := phaseflow.RunReport{
		Models: []phaseflow.ModelStat{{Model: "strong", Passes: 3, Attempts: 4}},
	}
	text := formatRunReport(report)
	if !strings.Contains(text, "strong") || !strings.Contains(text, "3/4") {
		t.Errorf("formatRunReport = %q, missing model stats", text)
	}
}
