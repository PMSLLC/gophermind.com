//go:build windows

package tui

import tea "github.com/charmbracelet/bubbletea"

// The project runner needs process groups and a sandbox that only exist on unix
// (see build/runner), so on Windows /project only explains itself.
const projectV2Unavailable = "/project is not available on Windows: the build runner needs a unix host."

type projectV2LineMsg string

type projectV2DoneMsg struct{}

func projectV2DoneLine(projectV2DoneMsg) string { return projectV2Unavailable }

func (m model) handleProjectV2Command(string) (model, tea.Cmd) {
	m.appendLine(projectV2Unavailable)
	m.sync()
	return m, nil
}
