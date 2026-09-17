// This file persists the right panel's PanelState (.planning/tasks/
// 04-03.json's "state persisted across app restarts") as a small JSON file
// under gophermind-osx's local state directory (see localstate.go). No
// cgo/libui-ng here -- unlike rightpanel.go, this is plain file I/O, kept
// in the main package (rather than gophermind-osx/ui) because deciding
// *where* state lives on disk is an OS/app-packaging concern, not
// something appui.PanelState itself needs to know (see its own doc
// comment: it only knows how to Save/Load against an io.Writer/io.Reader).
package main

import (
	"path/filepath"

	appui "gophermind/gophermind-osx/ui"
)

// panelStateFile returns the on-disk path for the panel's persisted state.
func panelStateFile() (string, error) {
	dir, err := osxStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "panel-state.json"), nil
}

// loadPanelStateFrom reads a PanelState from path, returning a plain
// default (expanded) for a missing file or any read/parse error -- a
// corrupt or absent state file must never block the app from starting.
func loadPanelStateFrom(path string) *appui.PanelState {
	return loadState(path, appui.LoadPanelState, appui.NewPanelState())
}

// savePanelStateTo writes p to path, creating parent directories as
// needed. Best-effort: a failure here (e.g. a read-only config dir) is not
// fatal to the app, and this package has no logger to report it to --
// same tolerance ApprovalTracker.CheckTimeouts documents for its own
// per-item failures.
func savePanelStateTo(path string, p *appui.PanelState) {
	saveState(path, p.Save)
}

// loadPanelState and savePanelState are loadPanelStateFrom/savePanelStateTo
// against the real default path (panelStateFile), for NewChatWindow to use.
// A failure to even determine the path (panelStateFile's osxStateDir call)
// degrades the same way: default state, no-op save.

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
