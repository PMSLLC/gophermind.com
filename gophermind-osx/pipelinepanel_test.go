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
	"context"
	"errors"
	"testing"
	"time"

	"gophermind/gophermind-osx/client"

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

func newTestPipelinePanel(t *testing.T, app *App, state *appui.PipelineState) *pipelinePanel {
	t.Helper()
	pp := newPipelinePanel(state, app.window, nil, nil, nil)
	attachControlForTest(app.window, pp.Control())
	return pp
}

func TestPipelinePanel_StopCancelsTheFollower(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		state.SetState(runview.StateBody{Run: &runview.RunSummary{RunID: "r1", Status: "building"}})
		pp := newTestPipelinePanel(t, app, state)
		ctxs := make(chan context.Context, 8)
		pp.openEvents = func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
			select {
			case ctxs <- ctx:
			default:
			}
			return nil, errors.New("down")
		}

		pp.follow()
		var first context.Context
		select {
		case first = <-ctxs:
		case <-time.After(5 * time.Second):
			t.Fatal("follower never opened the stream")
		}
		pp.stop()
		select {
		case <-first.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("stop did not cancel the follower context")
		}
		pp.followMu.Lock()
		cancel := pp.followCancel
		pp.followMu.Unlock()
		if cancel != nil {
			t.Error("followCancel not nil after stop")
		}

		// A second follow replaces the first.
		pp.follow()
		var second context.Context
		select {
		case second = <-ctxs:
		case <-time.After(5 * time.Second):
			t.Fatal("second follower never opened the stream")
		}
		pp.follow()
		select {
		case <-second.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("second follow did not cancel the previous follower")
		}
		pp.stop()
	})
}

func TestPipelinePanel_ReloadWithEmptyBodyKeepsTheShownRun(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		state.SetState(runview.StateBody{
			Run:   &runview.RunSummary{RunID: "r1", Status: "building"},
			Nodes: []runview.NodeSummary{{ID: "fn-a", Kind: "function"}},
		})
		pp := newTestPipelinePanel(t, app, state)
		pp.fetchPipeline = func(context.Context) (runview.StateBody, error) { return runview.StateBody{}, nil }
		if !pp.reload(context.Background()) {
			t.Fatal("reload reported failure")
		}
		if state.RunID() != "r1" || len(state.Nodes()) != 1 {
			t.Errorf("run %q nodes %d after empty reload, want r1 and 1", state.RunID(), len(state.Nodes()))
		}
	})
}

func TestPipelinePanel_ReportClearsWhenTheRunChanges(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		pp := newTestPipelinePanel(t, app, state)
		state.SetState(runview.StateBody{Run: &runview.RunSummary{RunID: "r1", Status: "done"}})
		var rb runview.ReportBody
		rb.Status = "verified"
		state.SetReport(rb)
		pp.refresh()
		if pp.reportTextForTest() == "" {
			t.Fatal("report box empty after SetReport")
		}
		state.SetState(runview.StateBody{Run: &runview.RunSummary{RunID: "r2", Status: "building"}})
		pp.refresh()
		if got := pp.reportTextForTest(); got != "" {
			t.Errorf("report box = %q after the run changed, want empty", got)
		}
	})
}

// After stop (a disconnect) nothing used to start the follower again, so a
// later connect left the panel dead. restart refetches the state and starts a
// new follower.
func TestPipelinePanel_RestartAfterStopRefetchesAndFollows(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewPipelineState()
		pp := newTestPipelinePanel(t, app, state)
		fetched := make(chan struct{}, 8)
		pp.fetchPipeline = func(context.Context) (runview.StateBody, error) {
			select {
			case fetched <- struct{}{}:
			default:
			}
			return runview.StateBody{Run: &runview.RunSummary{RunID: "r1", Status: "building"}}, nil
		}
		ctxs := make(chan context.Context, 8)
		pp.openEvents = func(ctx context.Context, run string, since int64) (*client.EventStream, error) {
			select {
			case ctxs <- ctx:
			default:
			}
			return nil, errors.New("down")
		}
		wait := func(what string) {
			select {
			case <-fetched:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: state never fetched", what)
			}
		}
		follower := func(what string) context.Context {
			select {
			case c := <-ctxs:
				return c
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: follower never opened the stream", what)
			}
			return nil
		}

		pp.seed()
		wait("seed")
		first := follower("seed")
		pp.stop()
		select {
		case <-first.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("stop did not cancel the follower")
		}

		pp.restart()
		wait("restart")
		second := follower("restart")
		if second.Err() != nil {
			t.Error("the restarted follower is already cancelled")
		}
		pp.followMu.Lock()
		running := pp.followCancel != nil
		pp.followMu.Unlock()
		if !running {
			t.Error("no follower registered after restart")
		}
		pp.stop()
	})
}
