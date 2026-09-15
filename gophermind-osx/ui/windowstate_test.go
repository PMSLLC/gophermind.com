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

func TestLoadWindowState_InvalidJSONErrors(t *testing.T) {
	if _, err := LoadWindowState(strings.NewReader("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
