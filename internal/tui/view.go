package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ tea.Model = model{}

var boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

func (m model) View() string {
	if !m.ready {
		return m.banner
	}

	var status string
	switch m.st {
	case stateWorking:
		status = statusWorkingStyle.Render(fmt.Sprintf("%s %s · %s mode · %s · working", m.spin.View(), m.model, m.mode, m.usage.String()))
	case stateApproval:
		status = statusApprovalStyle.Render(fmt.Sprintf("⏸ approve  %s %s  ? (y)es (n)o (a)lways", m.pending.tool, oneLine(m.pending.args)))
	default:
		status = statusReadyStyle.Render(fmt.Sprintf("%s · %s mode · %s · ready · /help", m.model, m.mode, m.usage.String()))
	}

	width := m.width - 2
	if width < 1 {
		width = 1
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		boxStyle.Width(width).Render(m.input.View()),
		status,
	)
}
