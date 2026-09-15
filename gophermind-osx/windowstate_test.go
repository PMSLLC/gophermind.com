package main

import (
	"path/filepath"
	"testing"

	appui "gophermind/gophermind-osx/ui"
)

func TestLoadWindowStateFrom_MissingFileReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got := loadWindowStateFrom(path)
	if got != appui.DefaultWindowState() {
		t.Errorf("loadWindowStateFrom on a missing file = %+v, want defaults", got)
	}
}

func TestSaveLoadWindowStateFrom_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "window-state.json")

	s := appui.WindowState{Width: 1000, Height: 700, X: 20, Y: 30}
	saveWindowStateTo(path, s)

	got := loadWindowStateFrom(path)
	if got != s {
		t.Errorf("loadWindowStateFrom = %+v, want %+v", got, s)
	}
}
