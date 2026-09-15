package main

// Like rightpanel_test.go, this only proves the model picker's widgets
// build and respond to state changes without panicking, routed through
// runOnUIThread for the same AppKit-single-thread reason. Driven through
// NewChatWindow (not a bare newModelPicker call) so the picker's widgets
// are actually attached to the window -- an unattached widget tree makes
// libui-ng's leak detector abort the process at uiUninit(), same as
// chatview_test.go's buildAttributedString note already documents for a
// different widget.

import (
	"testing"

	"gophermind/gophermind-lib/modelcat"
)

func TestNewChatWindow_BuildsModelPickerWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var err error
	var modelNil bool
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		modelNil = cw.Model == nil || cw.modelUI == nil
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if modelNil {
		t.Fatal("ChatWindow.Model/modelUI is nil")
	}
}

// TestChatWindow_ModelStateMutationsRefreshWithoutPanic exercises refresh
// (the OnChange -> rebuild-combos path) for every state mutation 04-04's
// widgets can trigger.
func TestChatWindow_ModelStateMutationsRefreshWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var err error
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})

		cw.Model.SetEntries([]modelcat.Entry{{ID: "a", Provider: "P", Profile: "p", Reachable: true}})
		cw.Model.AddToOrder("p/a")
		cw.Model.SetCurrentKey("p/a")
		cw.Model.SetFilterReachable(true)
		cw.Model.SetFilterHasCapacity(true)
		cw.Model.SetProviderFilter("P")
		cw.Model.SetModalityFilter("text")
		cw.Model.MoveUp("p/a")
		cw.Model.MoveDown("p/a")
		cw.Model.RemoveFromOrder("p/a")
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}

// TestChatWindow_PinSelectedNoOpWithoutPanic exercises modelUI.pinSelected
// with no PinModelFunc wired (NewChatWindow's current default, since no
// backend connection exists yet -- see chatinput.go's doc comment): it
// must be a safe no-op, not a panic.
func TestChatWindow_PinSelectedNoOpWithoutPanic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var err error
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()
		cw := NewChatWindow(app, func(string) {})
		cw.Model.SetEntries([]modelcat.Entry{{ID: "a", Provider: "P", Profile: "p", Reachable: true}})
		cw.modelUI.pinSelected()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}
