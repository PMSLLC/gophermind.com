// This file persists the right panel's PanelState (.planning/tasks/
// 04-03.json's "state persisted across app restarts") as a small JSON file
// under the OS's per-user config directory. No cgo/libui-ng here -- unlike
// rightpanel.go, this is plain file I/O, kept in the main package (rather
// than gophermind-osx/ui) because deciding *where* state lives on disk is
// an OS/app-packaging concern, not something appui.PanelState itself
// needs to know (see its own doc comment: it only knows how to
// Save/Load against an io.Writer/io.Reader).
package main

import (
	"os"
	"path/filepath"

	appui "gophermind/gophermind-osx/ui"
)

// panelStateFile returns the on-disk path for the panel's persisted state.
func panelStateFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind-osx", "panel-state.json"), nil
}

// loadPanelStateFrom reads a PanelState from path, returning a plain
// default (expanded) for a missing file or any read/parse error -- a
// corrupt or absent state file must never block the app from starting.
func loadPanelStateFrom(path string) *appui.PanelState {
	f, err := os.Open(path)
	if err != nil {
		return appui.NewPanelState()
	}
	defer f.Close()

	state, err := appui.LoadPanelState(f)
	if err != nil {
		return appui.NewPanelState()
	}
	return state
}

// savePanelStateTo writes p to path, creating parent directories as
// needed. Best-effort: a failure here (e.g. a read-only config dir) is not
// fatal to the app, and this package has no logger to report it to --
// same tolerance ApprovalTracker.CheckTimeouts documents for its own
// per-item failures.
func savePanelStateTo(path string, p *appui.PanelState) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	p.Save(f)
}

// loadPanelState and savePanelState are loadPanelStateFrom/savePanelStateTo
// against the real default path (panelStateFile), for NewChatWindow to use.
// A failure to even determine the path (panelStateFile's os.UserConfigDir
// call) degrades the same way: default state, no-op save.

func loadPanelState() *appui.PanelState {
	path, err := panelStateFile()
	if err != nil {
		return appui.NewPanelState()
	}
	return loadPanelStateFrom(path)
}

func savePanelState(p *appui.PanelState) {
	path, err := panelStateFile()
	if err != nil {
		return
	}
	savePanelStateTo(path, p)
}
