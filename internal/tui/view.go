package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jbrahy/bubblecomplete"
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

	// vp is a local copy: the popup menu (below) shrinks its Height so the
	// spliced-in menu doesn't push the input/status off screen, without
	// mutating m itself (View has a value receiver and must stay pure).
	vp := m.viewport

	// Predictive-text suggestions are only ever live while stateIdle (see
	// handleKey), so mode is inert here otherwise.
	var menuView string
	if m.st == stateIdle && m.proj == projNone && m.complete.Mode() == bubblecomplete.ModeMenu {
		menuView = m.complete.View()
		if h := lipgloss.Height(menuView); h > 0 {
			vp.Height -= h
			if vp.Height < 1 {
				vp.Height = 1
			}
		}
	}

	// Ghost text is rendered TRULY INLINE — the input's own text followed by
	// the styled ghost continuation on the same line — rather than as a
	// separate line. v1 simplification: this only composes cleanly when the
	// box is showing a single visual row (see singleVisualLine); a
	// fully-correct multi-line inline compose would need to reproduce the
	// textarea's own per-row wrapping/padding, which is out of scope here.
	// Outside that case (or when the cursor isn't at the true end of the
	// input, or a menu/no suggestion is active) the input renders unchanged.
	inputContent := m.input.View()
	if m.st == stateIdle && m.proj == projNone && m.complete.Mode() == bubblecomplete.ModeGhost {
		if ghost := m.complete.Ghost(); ghost != "" && singleVisualLine(m) && cursorAtEnd(m) {
			inputContent = m.input.Prompt + m.input.Value() + m.complete.GhostStyle().Render(ghost)
		}
	}

	// During a guided /project flow, show a dialog panel above the input.
	if m.proj != projNone {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			vp.View(),
			projectDialogStyle.Width(width).Render(projectDialogText(m.proj, m.projName)),
			boxStyle.Width(width).Render(inputContent),
			status,
		)
	}

	if menuView != "" {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			vp.View(),
			menuView,
			boxStyle.Width(width).Render(inputContent),
			status,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		vp.View(),
		boxStyle.Width(width).Render(inputContent),
		status,
	)
}
