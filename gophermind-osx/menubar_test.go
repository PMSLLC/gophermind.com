package main

// Like the other widget-assembly test files, this only proves the menu
// bar's items build and dispatch to their wired actions without
// panicking, routed through runOnUIThread for the same AppKit-
// single-thread reason -- whether it actually LOOKS right (or whether a
// real click on a real menu bar reaches these callbacks) needs a human at
// a real window.

import "testing"

func TestNewApp_MenuItemsAreNonNil(t *testing.T) {
	var err error
	var newProjectNil, openBriefNil, settingsNil, togglePanelNil, toggleDarkModeNil bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		newProjectNil = app.NewProjectItem == nil
		openBriefNil = app.OpenBriefItem == nil
		settingsNil = app.SettingsItem == nil
		togglePanelNil = app.TogglePanelItem == nil
		toggleDarkModeNil = app.ToggleDarkModeItem == nil
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	for name, isNil := range map[string]bool{
		"NewProjectItem": newProjectNil, "OpenBriefItem": openBriefNil, "SettingsItem": settingsNil,
		"TogglePanelItem": togglePanelNil, "ToggleDarkModeItem": toggleDarkModeNil,
	} {
		if isNil {
			t.Errorf("App.%s is nil", name)
		}
	}
}

func TestWireMenuActions_ClickDispatchesToAction(t *testing.T) {
	var err error
	var calls int
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()

		action := func() { calls++ }
		app.WireMenuActions(action, action, action, action, action)

		// Drives the exact callback goMenuItemClicked receives from
		// uiMenuItemOnClicked, without needing a real menu bar click (not
		// reachable in a headless test run) -- same "call the exported
		// trampoline directly" approach chatview_test.go and
		// rightpanel_test.go already use for their own C callbacks.
		goMenuItemClicked(nil, nil, menuActionHandleForTest(app.NewProjectItem))
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWireMenuActions_NilActionDoesNotPanic(t *testing.T) {
	var err error
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		app.WireMenuActions(nil, nil, nil, nil, nil) // must not panic
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}
