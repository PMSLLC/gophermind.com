// This file persists WindowState (.planning/tasks/04-08.json's "window
// state: size, position, panel state persisted across app restarts") as a
// small JSON file under gophermind-osx's local state directory (see
// localstate.go), same pattern as panelstate.go and cachehistorystate.go.
package main

import (
	"path/filepath"

	appui "gophermind/gophermind-osx/ui"
)

// windowStateFile returns the on-disk path for the persisted window state.
func windowStateFile() (string, error) {
	dir, err := osxStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "window-state.json"), nil
}

// loadWindowStateFrom reads a WindowState from path, returning
// appui.DefaultWindowState() for a missing file or any read/parse error --
// same first-run/corrupt-file tolerance as loadPanelStateFrom.
func loadWindowStateFrom(path string) appui.WindowState {
	return loadState(path, appui.LoadWindowState, appui.DefaultWindowState())
}

// saveWindowStateTo writes s to path, creating parent directories as
// needed. Best-effort, same tolerance as savePanelStateTo.
func saveWindowStateTo(path string, s appui.WindowState) {
	saveState(path, s.Save)
}

// loadWindowState and saveWindowState are
// loadWindowStateFrom/saveWindowStateTo against the real default path.

func loadWindowState() appui.WindowState {
	path, err := windowStateFile()
	if err != nil {
		return appui.DefaultWindowState()
	}
	return loadWindowStateFrom(path)
}

func saveWindowState(s appui.WindowState) {
	path, err := windowStateFile()
	if err != nil {
		return
	}
	saveWindowStateTo(path, s)
}
