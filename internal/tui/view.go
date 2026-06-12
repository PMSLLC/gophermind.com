package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ tea.Model = model{}

var (
	statusStyle = lipgloss.NewStyle().Faint(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}

	body := ""
	if m.st == stateWorking && m.stream != "" {
		body = m.stream + "\n"
	}

	var status string
	switch m.st {
	case stateWorking:
		status = fmt.Sprintf("%s %s · %s mode · %d tokens · working", m.spin.View(), m.model, m.mode, m.tokens)
	case stateApproval:
		status = fmt.Sprintf("approve %s %s ? (y)es (n)o (a)lways", m.pending.tool, oneLine(m.pending.args))
	default:
		status = fmt.Sprintf("%s · %s mode · ready", m.model, m.mode)
	}

	width := m.width - 2
	if width < 1 {
		width = 1
	}
	return body + "\n" +
		boxStyle.Width(width).Render(m.input.View()) + "\n" +
		statusStyle.Render(status)
}
