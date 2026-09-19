package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jbrahy/bubblecomplete"
	"gophermind/gophermind-lib/freellm"
)

var _ tea.Model = model{}

var boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

func (m model) View() string {
	return m.attentionOverlay(m.frame())
}

// attentionOverlay renders the composed frame inverse while the attention
// signal is showing (see attention.go).
func (m model) attentionOverlay(frame string) string {
	if !m.attentionVisible() {
		return frame
	}
	return invertFrame(frame)
}

func (m model) frame() string {
	if !m.ready {
		return m.banner
	}

	name := m.model
	free := ""
	if a, ok := freellm.AttributionFor(m.profile, m.model); ok {
		if m.hyperlinks && a.Link() != "" {
			name = osc8(a.Link(), m.model)
		}
		free = " - " + a.Short()
	}

	var status string
	switch m.st {
	case stateWorking:
		status = statusWorkingStyle.Render(fmt.Sprintf("%s %s%s · %s mode · %s · working", m.spin.View(), name, free, m.mode, m.usage.String()))
	case stateApproval:
		status = statusApprovalStyle.Render(fmt.Sprintf("⏸ approve  %s %s  ? (y)es (n)o (a)lways", m.pending.tool, oneLine(m.pending.args)))
	default:
		status = statusReadyStyle.Render(fmt.Sprintf("%s%s · %s mode · %s · ready · /help", name, free, m.mode, m.usage.String()))
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
	if m.st == stateIdle && m.proj == projNone && m.qphase == qNone && m.complete.Mode() == bubblecomplete.ModeMenu {
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
	if m.st == stateIdle && m.proj == projNone && m.qphase == qNone && m.complete.Mode() == bubblecomplete.ModeGhost {
		if ghost := m.complete.Ghost(); ghost != "" && singleVisualLine(m) && cursorAtEnd(m) {
			inputContent = m.input.Prompt + m.input.Value() + m.complete.GhostStyle().Render(ghost)
		}
	}

	// The question round takes the panel above the input while it is showing,
	// and keeps a one-line panel while its pass runs.
	if m.qphase != qNone {
		panel := questionsDialogText(m.qphase)
		if m.qphase == qAsking {
			panel = m.round.View()
		}
		// The round's height is unbounded (see questionRound.View), so it is
		// capped here to what the terminal has left after the input and the
		// status line, keeping the bottom rows (the current question, its
		// note and the key help) and dropping the list above them. The
		// viewport gives up the rows the panel takes, as the menu does.
		//
		// The cap has a floor: the panel keeps at least one row, so on a
		// terminal shorter than about 8 rows (viewport 1, panel border 2 and
		// row 1, input box 3, status 1) the frame is taller than the screen.
		// Nothing useful fits there; the frame is still well formed.
		box := boxStyle.Width(width).Render(inputContent)
		const panelBorder, minViewport = 2, 1
		avail := m.height - lipgloss.Height(box) - statusHeight - panelBorder - minViewport
		panel = keepBottomRows(panel, avail, width-2)
		panelView := roundDialogStyle.Width(width).Render(panel)
		if h := m.height - lipgloss.Height(box) - statusHeight - lipgloss.Height(panelView); h < vp.Height {
			vp.Height = h
			if vp.Height < 1 {
				vp.Height = 1
			}
		}
		return lipgloss.JoinVertical(
			lipgloss.Left,
			vp.View(),
			panelView,
			box,
			status,
		)
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

// keepBottomRows keeps the last n rows of s as they will show at the given
// content width, or all of it when it is shorter: a line wider than width
// takes several rows, so the rows are counted after wrapping. A limit below 1
// keeps one row; a width below 1 counts each line as one row.
func keepBottomRows(s string, n, width int) string {
	if n < 1 {
		n = 1
	}
	if width >= 1 {
		s = ansi.Wrap(s, width, "")
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
