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
	"testing"

	"gophermind/gophermind-lib/runview"
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

func TestPipelinePanel_RefreshRebuildsTheNodeListOnStateChange(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		pp := newPipelinePanel(state, app.window, nil, nil, nil)
		attachControlForTest(app.window, pp.Control())

		// state.OnChange dispatches pp.refresh via queueMain, which only runs
		// once uiMain is pumping (see chatview_test.go), so this calls
		// refresh() directly, the same thing queueMain would eventually run.
		comp := runview.NodeSummary{ID: "greeting", Kind: "component", Title: "Greeting"}
		state.SetState(runview.StateBody{
			Run:   &runview.RunSummary{RunID: "gm-2026-10-08-001", Status: "building"},
			Nodes: []runview.NodeSummary{comp, {ID: "fn-a", Kind: "function", Parent: "greeting", Status: "in_progress"}},
		})
		pp.refresh()
		if len(pp.nodeLabels) != 2 { // the component row and one node row
			t.Errorf("nodeLabels after one node = %d, want 2", len(pp.nodeLabels))
		}

		state.SetState(runview.StateBody{
			Run: &runview.RunSummary{RunID: "gm-2026-10-08-001", Status: "building"},
			Nodes: []runview.NodeSummary{comp,
				{ID: "fn-a", Kind: "function", Parent: "greeting"}, {ID: "fn-b", Kind: "function", Parent: "greeting"}},
		})
		pp.refresh()
		if len(pp.nodeLabels) != 3 {
			t.Errorf("nodeLabels after two nodes = %d, want 3", len(pp.nodeLabels))
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

func TestPipelinePanel_ShowNodeWithoutALiveConnectionDoesNotPanic(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		var said string
		pp := newPipelinePanel(appui.NewPipelineState(), app.window, func(s string) { said = s }, nil, nil)
		attachControlForTest(app.window, pp.Control())
		pp.doShow() // no run, no node source: say so, do not panic
		if said == "" {
			t.Error("doShow with nothing to show must tell the user why")
		}
	})
}
