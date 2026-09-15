package ui

import (
	"encoding/json"
	"io"
)

// WindowState is the main window's persisted size and position
// (.planning/tasks/04-08.json's "window state: size, position ...
// persisted across app restarts"). Plain data, no OnChange: unlike
// PanelState, nothing in this app re-renders when the window moves or
// resizes, so there's no redraw to trigger -- the widget layer just reads
// this once at startup and writes it once at shutdown.
type WindowState struct {
	Width, Height int
	X, Y          int
}

// DefaultWindowState matches App's own DefaultWidth/DefaultHeight
// (app.go), positioned wherever the OS places a window with no saved
// position (X, Y left 0 -- the widget layer only calls SetPosition when a
// saved position exists, see main.go's restore step).
func DefaultWindowState() WindowState {
	return WindowState{Width: 1200, Height: 800}
}

type windowStateJSON struct {
	Width, Height int
	X, Y          int
}

// Save writes s as JSON to w.
func (s WindowState) Save(w io.Writer) error {
	return json.NewEncoder(w).Encode(windowStateJSON{s.Width, s.Height, s.X, s.Y})
}

// LoadWindowState reads a WindowState previously written by Save. An
// empty r (no saved state yet -- first run) returns DefaultWindowState,
// same first-run contract as LoadPanelState.
func LoadWindowState(r io.Reader) (WindowState, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return WindowState{}, err
	}
	if len(data) == 0 {
		return DefaultWindowState(), nil
	}
	var decoded windowStateJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return WindowState{}, err
	}
	return WindowState{decoded.Width, decoded.Height, decoded.X, decoded.Y}, nil
}
