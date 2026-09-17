package main

// TestNewStatusItem_BuildsAndRefreshesWithoutPanic is a smoke test, same
// spirit and same limits as chatview_test.go's and rightpanel_test.go's:
// it proves the status item and its menu build via real AppKit calls and
// survive Add/SetStatus/Remove without panicking, routed through
// runOnUIThread for the same AppKit-single-thread reason. Whether the
// menu bar actually LOOKS right needs a human looking at a real menu bar.

import (
	"testing"

	appui "gophermind/gophermind-osx/ui"
)

func TestNewStatusItem_BuildsAndRefreshesWithoutPanic(t *testing.T) {
	var err error
	runOnUIThread(t, func() {
		var app *App
		app, err = NewApp(DefaultTitle, DefaultWidth, DefaultHeight)
		if err != nil {
			return
		}
		defer app.Close()

		backends := appui.NewBackendListState()
		newStatusItem(backends, app.window)

		// Exercise every mutation that fires OnChange (and so refresh):
		// add, change status, add a second, remove one.
		backends.Add(appui.BackendProfile{Name: "local", Mode: "local"})
		backends.SetStatus("local", "connected")
		backends.Add(appui.BackendProfile{Name: "office", Mode: "remote"})
		backends.Remove("office")
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
}

func TestFormatBackendMenuLabel(t *testing.T) {
	cases := []struct {
		name, status, want string
	}{
		{"local", "connected", "local -- connected"},
		{"office", "disconnected", "office -- disconnected"},
		{"", "connected", " -- connected"},
	}
	for _, c := range cases {
		if got := formatBackendMenuLabel(c.name, c.status); got != c.want {
			t.Errorf("formatBackendMenuLabel(%q, %q) = %q, want %q", c.name, c.status, got, c.want)
		}
	}
}
