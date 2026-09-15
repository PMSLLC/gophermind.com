package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPanelStateFrom_MissingFileReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	p := loadPanelStateFrom(path)
	if p.Collapsed {
		t.Error("loadPanelStateFrom on a missing file should default to expanded")
	}
}

func TestSaveLoadPanelStateFrom_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "panel-state.json")

	p := loadPanelStateFrom(path) // default, expanded
	p.Toggle()                    // now collapsed
	savePanelStateTo(path, p)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("savePanelStateTo did not create the file: %v", err)
	}

	reloaded := loadPanelStateFrom(path)
	if !reloaded.Collapsed {
		t.Error("reloaded state did not persist Collapsed = true")
	}
}
