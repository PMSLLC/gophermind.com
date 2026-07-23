package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestAltEnterDoesNotInsertNewlineByDefault is the reported bug: a key that
// arrives as ESC+CR (alt+enter) must not silently become a newline, because a
// remapped key is indistinguishable from a real Alt+Enter by the time it is
// decoded.
func TestAltEnterDoesNotInsertNewlineByDefault(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("abc")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m2 := updated.(model)

	if strings.Contains(m2.input.Value(), "\n") {
		t.Errorf("alt+enter inserted a newline: %q", m2.input.Value())
	}
}

// TestCtrlJStillInsertsNewline keeps a working way to write a multi-line
// prompt.
func TestCtrlJStillInsertsNewline(t *testing.T) {
	m := testModel(t)
	m.input.SetValue("abc")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m2 := updated.(model)

	if !strings.Contains(m2.input.Value(), "\n") {
		t.Errorf("ctrl+j did not insert a newline: %q", m2.input.Value())
	}
}

// TestAltEnterNewlineCanBeRestored: users whose terminal sends a genuine
// Alt+Enter can opt back in.
func TestAltEnterNewlineCanBeRestored(t *testing.T) {
	t.Setenv(altEnterEnv, "1")
	m := testModel(t)
	m.input.SetValue("abc")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m2 := updated.(model)

	if !strings.Contains(m2.input.Value(), "\n") {
		t.Errorf("alt+enter did not insert a newline with %s=1: %q", altEnterEnv, m2.input.Value())
	}
}

// TestDashIsAlwaysLiteral: whatever the terminal does upstream, a decoded "-"
// rune must only ever type a dash.
func TestDashIsAlwaysLiteral(t *testing.T) {
	m := testModel(t)

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m2 := updated.(model)

	if m2.input.Value() != "-" {
		t.Errorf("input = %q, want %q", m2.input.Value(), "-")
	}
	if strings.Contains(m2.input.Value(), "\n") {
		t.Error("typing a dash produced a newline")
	}
}
