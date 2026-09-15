package ui

import (
	"strings"
	"testing"
)

func TestPanelState_DefaultsExpanded(t *testing.T) {
	p := NewPanelState()
	if p.Collapsed {
		t.Error("NewPanelState should default to expanded")
	}
}

func TestPanelState_ToggleFlipsCollapsed(t *testing.T) {
	p := NewPanelState()
	p.Toggle()
	if !p.Collapsed {
		t.Error("Toggle() from expanded should collapse")
	}
	p.Toggle()
	if p.Collapsed {
		t.Error("Toggle() from collapsed should expand")
	}
}

func TestPanelState_OnChangeFiresOnToggle(t *testing.T) {
	p := NewPanelState()
	calls := 0
	p.OnChange(func() { calls++ })
	p.Toggle()
	p.Toggle()
	if calls != 2 {
		t.Errorf("OnChange called %d times, want 2", calls)
	}
}

// TestPanelState_SaveLoadRoundTrips covers "state persisted across app
// restarts": Save/Load work against a plain io.Writer/io.Reader (no file
// I/O in this package -- see panel.go's doc comment), so the widget layer
// decides where the bytes actually live.
func TestPanelState_SaveLoadRoundTrips(t *testing.T) {
	p := NewPanelState()
	p.Toggle() // collapsed = true

	var buf strings.Builder
	if err := p.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadPanelState(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadPanelState: %v", err)
	}
	if !loaded.Collapsed {
		t.Error("LoadPanelState did not restore Collapsed = true")
	}
}

// TestLoadPanelState_EmptyReaderReturnsDefault covers first-run: no saved
// state file yet, so Load should hand back a plain default rather than an
// error the caller has to special-case.
func TestLoadPanelState_EmptyReaderReturnsDefault(t *testing.T) {
	loaded, err := LoadPanelState(strings.NewReader(""))
	if err != nil {
		t.Fatalf("LoadPanelState: %v", err)
	}
	if loaded.Collapsed {
		t.Error("LoadPanelState on empty input should default to expanded")
	}
}

func TestLoadPanelState_InvalidJSONErrors(t *testing.T) {
	if _, err := LoadPanelState(strings.NewReader("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
