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

func TestNewApp_CreatesWindowWithCorrectTitleAndSize(t *testing.T) {
	app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer app.Close()

	if got := app.Title(); got != DefaultTitle {
		t.Errorf("Title() = %q, want %q", got, DefaultTitle)
	}
	w, h := app.ContentSize()
	if w != DefaultWidth || h != DefaultHeight {
		t.Errorf("ContentSize() = (%d, %d), want (%d, %d)", w, h, DefaultWidth, DefaultHeight)
	}
}

func TestNewApp_ShowDoesNotPanic(t *testing.T) {
	app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer app.Close()
	app.Show() // must not panic; libui-ng has no visible side effect to assert on without uiMain().
}

func TestNewApp_CustomTitleAndSize(t *testing.T) {
	app, err := NewApp("custom title", 640, 480)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer app.Close()

	if got := app.Title(); got != "custom title" {
		t.Errorf("Title() = %q, want %q", got, "custom title")
	}
	w, h := app.ContentSize()
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
	app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.Close()
}

func TestNewApp_SequentialLifecyclesDoNotPanic(t *testing.T) {
	for i := 0; i < 3; i++ {
		app, err := NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			t.Fatalf("NewApp iteration %d: %v", i, err)
		}
		app.Show()
		app.Close()
	}
}
