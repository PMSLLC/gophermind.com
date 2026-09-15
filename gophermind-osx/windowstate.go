// This file persists WindowState (.planning/tasks/04-08.json's "window
// state: size, position, panel state persisted across app restarts") as a
// small JSON file under the user's config dir, same pattern as
// panelstate.go and cachehistorystate.go.
package main

import (
	"os"
	"path/filepath"

	appui "gophermind/gophermind-osx/ui"
)

// windowStateFile returns the on-disk path for the persisted window state.
func windowStateFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind-osx", "window-state.json"), nil
}

// loadWindowStateFrom reads a WindowState from path, returning
// appui.DefaultWindowState() for a missing file or any read/parse error --
// same first-run/corrupt-file tolerance as loadPanelStateFrom.
func loadWindowStateFrom(path string) appui.WindowState {
	f, err := os.Open(path)
	if err != nil {
		return appui.DefaultWindowState()
	}
	defer f.Close()

	s, err := appui.LoadWindowState(f)
	if err != nil {
		return appui.DefaultWindowState()
	}
	return s
}

// saveWindowStateTo writes s to path, creating parent directories as
// needed. Best-effort, same tolerance as savePanelStateTo.
func saveWindowStateTo(path string, s appui.WindowState) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	s.Save(f)
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
