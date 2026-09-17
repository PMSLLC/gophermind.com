package ui

import (
	"strings"
	"testing"
)

func TestWindowState_SaveLoadRoundTrips(t *testing.T) {
	s := WindowState{Width: 1200, Height: 800, X: 50, Y: 75}
	var buf strings.Builder
	if err := s.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadWindowState(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadWindowState: %v", err)
	}
	if loaded != s {
		t.Errorf("loaded = %+v, want %+v", loaded, s)
	}
}

func TestLoadWindowState_EmptyReaderReturnsDefault(t *testing.T) {
	loaded, err := LoadWindowState(strings.NewReader(""))
	if err != nil {
		t.Fatalf("LoadWindowState: %v", err)
	}
	if loaded.Width <= 0 || loaded.Height <= 0 {
		t.Errorf("LoadWindowState on empty input = %+v, want a positive default size", loaded)
	}
}

// TestDefaultWindowState_StartsMaximized covers the actual bug report: a
// fresh install (no window-state.json yet) launched into a small,
// easy-to-miss window instead of filling the screen. The default must
// say "maximized" so main.go's caller knows to zoom the window on first
// launch, not just fall back to a fixed pixel size.
func TestDefaultWindowState_StartsMaximized(t *testing.T) {
	if !DefaultWindowState().Maximized {
		t.Error("DefaultWindowState().Maximized = false, want true (first launch should fill the screen)")
	}
}

// TestWindowState_SaveLoadRoundTrips_PreservesMaximized covers Maximized
// surviving a real save/load cycle, same as every other field.
func TestWindowState_SaveLoadRoundTrips_PreservesMaximized(t *testing.T) {
	s := WindowState{Width: 1200, Height: 800, X: 50, Y: 75, Maximized: true}
	var buf strings.Builder
	if err := s.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadWindowState(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadWindowState: %v", err)
	}
	if loaded != s {
		t.Errorf("loaded = %+v, want %+v", loaded, s)
	}
}

// TestLoadWindowState_LegacyFileWithNoMaximizedKeyDefaultsToFalse covers
// reading a window-state.json written before this field existed: it must
// parse cleanly and NOT retroactively claim the user wants to be
// maximized (that would ignore a window size they deliberately saved).
func TestLoadWindowState_LegacyFileWithNoMaximizedKeyDefaultsToFalse(t *testing.T) {
	loaded, err := LoadWindowState(strings.NewReader(`{"Width":602,"Height":690,"X":274,"Y":96}`))
	if err != nil {
		t.Fatalf("LoadWindowState: %v", err)
	}
	if loaded.Maximized {
		t.Error("a legacy file with no \"Maximized\" key should load as Maximized = false")
	}
	if loaded.Width != 602 || loaded.Height != 690 {
		t.Errorf("loaded = %+v, want the legacy width/height preserved", loaded)
	}
}

func TestLoadWindowState_InvalidJSONErrors(t *testing.T) {
	if _, err := LoadWindowState(strings.NewReader("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
