package main

// TestNewStatusItem_BuildsAndRefreshesWithoutPanic is a smoke test, same
// spirit and same limits as chatview_test.go's and rightpanel_test.go's:
// it proves the status item and its menu build via real AppKit calls and
// survive Add/SetStatus/Remove without panicking, routed through
// runOnUIThread for the same AppKit-single-thread reason. Whether the
// menu bar actually LOOKS right needs a human looking at a real menu bar.

import (
	"os"
	"path/filepath"
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

// TestFindMenubarIconEnvOverride verifies GOPHERMIND_MENUBAR_ICON, when it
// names a real file, wins over every other search location.
func TestFindMenubarIconEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-icon.png")
	if err := os.WriteFile(path, []byte("fake png"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPHERMIND_MENUBAR_ICON", path)

	if got := findMenubarIcon(); got != path {
		t.Errorf("findMenubarIcon() = %q, want the env override %q", got, path)
	}
}

// TestFindMenubarIconEnvOverrideMissingFileFallsThrough verifies an override
// naming a file that doesn't exist is ignored rather than returned as-is
// (which would hand statusItemCreate an unopenable path).
func TestFindMenubarIconEnvOverrideMissingFileFallsThrough(t *testing.T) {
	t.Setenv("GOPHERMIND_MENUBAR_ICON", filepath.Join(t.TempDir(), "does-not-exist.png"))
	t.Chdir(t.TempDir()) // no menubar-icon.png here either

	if got := findMenubarIcon(); got != "" {
		t.Errorf("findMenubarIcon() = %q, want empty when the override doesn't exist", got)
	}
}

// TestFindMenubarIconDevCwdFallback covers the dev-checkout layout: running
// from within gophermind-osx/ with no env override and no .app bundle.
func TestFindMenubarIconDevCwdFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "menubar-icon.png"), []byte("fake png"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if got := findMenubarIcon(); got != "menubar-icon.png" {
		t.Errorf("findMenubarIcon() = %q, want the bare dev-relative path", got)
	}
}

// TestFindMenubarIconNotFound verifies the "give up gracefully" path: no
// env, no bundle, no dev file -- statusItemCreate falls back to a text
// title rather than crashing on an empty path.
func TestFindMenubarIconNotFound(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := findMenubarIcon(); got != "" {
		t.Errorf("findMenubarIcon() = %q, want empty", got)
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
