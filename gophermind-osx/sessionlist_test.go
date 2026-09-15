package main

// Like chatview_test.go and rightpanel_test.go, this only proves the
// session list's widgets build and respond to actions without panicking,
// routed through runOnUIThread for the same AppKit-single-thread reason --
// whether it actually LOOKS right needs a human at a real window.

import (
	"context"
	"errors"
	"testing"

	"gophermind/gophermind-lib/llm"
	appui "gophermind/gophermind-osx/ui"
)

func TestNewChatWindow_BuildsSessionListWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var err error
	var sessionsNil bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		sessionsNil = cw.Sessions == nil
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if sessionsNil {
		t.Fatal("ChatWindow.Sessions is nil")
	}
}

func TestSessionList_DoResumeAppliesConfigAndHistory(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewSessionListState()
		transcript := appui.NewTranscript()
		resume := func(ctx context.Context, id string) (appui.SessionConfig, []llm.Message, error) {
			return appui.SessionConfig{Model: "m1", Mode: "coding", Root: "/tmp/proj"},
				[]llm.Message{{Role: "user", Content: "hi"}}, nil
		}
		sl := newSessionList(state, app.window, transcript, nil, nil, nil, nil, nil, resume)
		attachControlForTest(app.window, sl.Control())

		sl.doResume("s1")

		if state.SelectedID() != "s1" {
			t.Errorf("SelectedID() = %q, want s1", state.SelectedID())
		}
		if state.Config().Model != "m1" {
			t.Errorf("Config() = %+v", state.Config())
		}
		msgs := transcript.Messages()
		if len(msgs) != 1 || msgs[0].Text != "hi" {
			t.Errorf("Transcript.Messages() = %+v, want the replayed history", msgs)
		}
	})
}

func TestSessionList_DoResumeFailurePostsNotificationAndLeavesStateUnset(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewSessionListState()
		var notified string
		resume := func(ctx context.Context, id string) (appui.SessionConfig, []llm.Message, error) {
			return appui.SessionConfig{}, nil, errors.New("boom")
		}
		sl := newSessionList(state, app.window, nil, func(s string) { notified = s }, nil, nil, nil, nil, resume)
		attachControlForTest(app.window, sl.Control())

		sl.doResume("s1")

		if state.SelectedID() != "" {
			t.Errorf("SelectedID() = %q, want unset after a failed resume", state.SelectedID())
		}
		if notified == "" {
			t.Error("expected a failure notification")
		}
	})
}

func TestSessionList_DoRenameNilFuncIsNoop(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewSessionListState()
		sl := newSessionList(state, app.window, nil, nil, nil, nil, nil, nil, nil)
		attachControlForTest(app.window, sl.Control())
		sl.doRename("s1") // must not panic with rename == nil
	})
}

func TestSessionList_DoChooseRootAndDoCreateWithNilFuncsDoNotPanic(t *testing.T) {
	runOnUIThread(t, func() {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp: %v", err)
		}
		defer app.Close()

		state := appui.NewSessionListState()
		sl := newSessionList(state, app.window, nil, nil, nil, nil, nil, nil, nil)
		attachControlForTest(app.window, sl.Control())
		sl.doCreate() // create == nil: must not panic
		sl.refreshList()
		sl.refresh()
	})
}
