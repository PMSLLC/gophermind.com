package main

import "testing"

// This file deliberately never calls App.Run(): it starts libui-ng's real
// event loop (uiMain()), which blocks until the app quits and requires an
// actual macOS display/window-server session -- neither exists in an
// automated test run. What IS tested here -- uiInit, window creation with
// the right title/size, and clean teardown via Close() (uiControlDestroy +
// uiUninit) -- covers 03-01's "no panics or crashes on launch/quit"
// acceptance criterion for everything except the blocking event loop
// itself, which needs a human at an actual window to verify.
//
// libui-ng is process-global single-instance state (uiInit/uiUninit), so
// these tests run sequentially (no t.Parallel()) and each calls Close()
// before returning, leaving the library uninitialized for the next test.
// Every libui-ng-touching call runs through runOnUIThread (see
// uithread_test.go) -- required because go test spawns each test on its
// own goroutine, and AppKit (which libui-ng's darwin backend sits on)
// requires every call, across the whole process, to land on one
// consistent OS thread. t.Fatalf itself must stay on the test's own
// goroutine (the testing package requires this), so each test captures
// results/errors inside the UI-thread closure and asserts on them after
// runOnUIThread returns, not inside it.

func TestNewApp_CreatesWindowWithCorrectTitleAndSize(t *testing.T) {
	var app *App
	var err error
	var title string
	var w, h int
	runOnUIThread(t, func() {
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		title = app.Title()
		w, h = app.ContentSize()
		app.Close()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if title != DefaultTitle {
		t.Errorf("Title() = %q, want %q", title, DefaultTitle)
	}
	if w != DefaultWidth || h != DefaultHeight {
		t.Errorf("ContentSize() = (%d, %d), want (%d, %d)", w, h, DefaultWidth, DefaultHeight)
	}
}

func TestNewApp_ShowDoesNotPanic(t *testing.T) {
	var err error
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		app.Show() // must not panic; libui-ng has no visible side effect to assert on without uiMain().
		app.Close()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}

func TestNewApp_CustomTitleAndSize(t *testing.T) {
	var err error
	var title string
	var w, h int
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp("custom title", 640, 480)
		if err != nil {
			return
		}
		title = app.Title()
		w, h = app.ContentSize()
		app.Close()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	if title != "custom title" {
		t.Errorf("Title() = %q, want %q", title, "custom title")
	}
	if w != 640 || h != 480 {
		t.Errorf("ContentSize() = (%d, %d), want (640, 480)", w, h)
	}
}

// TestApp_QuitCallbacksAttachWithoutPanic covers the "no panics" half of
// the window-close/Cmd+Q acceptance criterion: attaching the OnClosing and
// ShouldQuit callbacks (done inside NewApp) must not panic or crash. The
// callbacks calling uiQuit() correctly when actually triggered by a close
// event or Cmd+Q needs a real window/display session to verify -- see this
// file's top-level doc comment.
func TestApp_QuitCallbacksAttachWithoutPanic(t *testing.T) {
	var err error
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		app.Close()
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}

func TestNewApp_SequentialLifecyclesDoNotPanic(t *testing.T) {
	var err error
	runOnUIThread(t, func() {
		for i := 0; i < 3; i++ {
			var app *App
			app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
			if err != nil {
				return
			}
			app.Show()
			app.Close()
		}
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}
