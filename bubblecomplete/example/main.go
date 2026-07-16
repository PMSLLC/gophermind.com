// Command example is a minimal, standalone Bubble Tea program demonstrating
// github.com/jbrahy/bubblecomplete outside of gophermind. It hand-rolls its
// own single-line text buffer instead of pulling in bubbles/textinput, to
// mirror exactly what bubblecomplete depends on (bubbletea + lipgloss only)
// and to keep the host-integration pattern fully visible in one file.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jbrahy/bubblecomplete"
)

const prompt = "> "

// words is deliberately picked so typing exercises both presentation modes:
// "ap" -> apple, apricot (menu); "app" -> apple only (ghost); "ban" ->
// banana, band, bandana (menu); "bandan" -> bandana only (ghost).
var words = []string{"apple", "apricot", "banana", "band", "bandana"}

var helpStyle = lipgloss.NewStyle().Faint(true)

type model struct {
	value  string
	cursor int // rune index into value
	cmpl   bubblecomplete.Model
}

func initialModel() model {
	cmpl := bubblecomplete.New()
	cmpl.SetProviders(&wordProvider{words: words})
	return model{cmpl: cmpl}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// Route the key to the completion model first; the host only handles
	// it itself when the completion model declines (Consumed == false).
	cmpl, res := m.cmpl.Update(keyMsg)
	m.cmpl = cmpl

	if res.Accepted != nil {
		m.insert(res.Accepted)
		m.cmpl = m.cmpl.Query(m.value, m.cursor)
		return m, nil
	}
	if res.Consumed {
		return m, nil
	}

	switch keyMsg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEnter:
		if m.cmpl.Mode() == bubblecomplete.ModeMenu {
			nm, cand := m.cmpl.Accept()
			m.cmpl = nm
			if cand != nil {
				m.insert(cand)
				m.cmpl = m.cmpl.Query(m.value, m.cursor)
			}
			return m, nil
		}
		return m, tea.Quit

	default:
		m.applyKey(keyMsg)
		m.cmpl = m.cmpl.Query(m.value, m.cursor)
		return m, nil
	}
}

func (m model) View() string {
	var line string
	if ghost := m.cmpl.Ghost(); ghost != "" {
		line = prompt + m.value + m.cmpl.GhostStyle().Render(ghost)
	} else {
		line = prompt + m.value
	}

	var b strings.Builder
	if menu := m.cmpl.View(); menu != "" {
		b.WriteString(lipgloss.JoinVertical(lipgloss.Left, menu, line))
	} else {
		b.WriteString(line)
	}
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("type to complete · Tab/→ accept · ↑↓ menu · Esc dismiss · Enter quit"))
	b.WriteString("\n")
	return b.String()
}

// insert splices cand.Text into the buffer at the cursor, first deleting
// cand.Replace runes immediately before the cursor.
func (m *model) insert(cand *bubblecomplete.Candidate) {
	runes := []rune(m.value)

	del := cand.Replace
	if del > m.cursor {
		del = m.cursor
	}
	start := m.cursor - del

	ins := []rune(cand.Text)
	out := make([]rune, 0, len(runes)-del+len(ins))
	out = append(out, runes[:start]...)
	out = append(out, ins...)
	out = append(out, runes[m.cursor:]...)

	m.value = string(out)
	m.cursor = start + len(ins)
}

// applyKey handles the plain-text-editing keys the completion model didn't
// consume: printable input, backspace, and cursor movement.
func (m *model) applyKey(msg tea.KeyMsg) {
	runes := []rune(m.value)

	switch {
	case msg.Type == tea.KeyBackspace:
		if m.cursor > 0 {
			runes = append(runes[:m.cursor-1], runes[m.cursor:]...)
			m.cursor--
		}

	case msg.Type == tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
		return

	case msg.Type == tea.KeyRight:
		if m.cursor < len(runes) {
			m.cursor++
		}
		return

	case len(msg.Runes) > 0: // KeyRunes and KeySpace both carry printable runes
		out := make([]rune, 0, len(runes)+len(msg.Runes))
		out = append(out, runes[:m.cursor]...)
		out = append(out, msg.Runes...)
		out = append(out, runes[m.cursor:]...)
		runes = out
		m.cursor += len(msg.Runes)

	default:
		return
	}

	m.value = string(runes)
}

func main() {
	if _, err := tea.NewProgram(initialModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
