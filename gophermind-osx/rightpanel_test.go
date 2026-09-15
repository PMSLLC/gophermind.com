package main

// Like chatview_test.go, this only proves the panel's widgets build and
// respond to a toggle without panicking, routed through runOnUIThread for
// the same AppKit-single-thread reason -- whether it actually LOOKS right
// needs a human at a real window.

import "testing"

func TestNewChatWindow_BuildsPanelWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // loadPanelState must not touch the real user's config dir

	var err error
	var panelNil bool
	var visibleAtStart bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		panelNil = cw.Panel == nil
		visibleAtStart = cw.panel.Visible()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if panelNil {
		t.Fatal("ChatWindow.Panel is nil")
	}
	if !visibleAtStart {
		t.Error("panel should be visible by default (04-03's \"visible by default\")")
	}
}

// TestChatWindow_ToggleCollapsesAndExpandsPanel exercises the same path
// goPanelToggleClicked takes (state.Toggle then syncVisibility), driven
// directly since a real button click needs a live libui-ng event loop this
// test doesn't run.
func TestChatWindow_ToggleCollapsesAndExpandsPanel(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // loadPanelState must not touch the real user's config dir

	var err error
	var visibleAfterFirstToggle, visibleAfterSecondToggle bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})

		cw.Panel.Toggle()
		cw.panel.syncVisibility()
		visibleAfterFirstToggle = cw.panel.Visible()

		cw.Panel.Toggle()
		cw.panel.syncVisibility()
		visibleAfterSecondToggle = cw.panel.Visible()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if visibleAfterFirstToggle {
		t.Error("panel should be hidden after toggling from expanded")
	}
	if !visibleAfterSecondToggle {
		t.Error("panel should be visible again after toggling back")
	}
}
