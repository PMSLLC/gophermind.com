package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbrahy/bubblecomplete"
)

// TestProviderModes is a headless sanity check that the toy word list
// actually exercises both presentation modes, since main.go can't be driven
// without a real TTY.
func TestProviderModes(t *testing.T) {
	p := &wordProvider{words: words}

	if got := p.Suggest("ap", 2); len(got) != 2 {
		t.Fatalf(`Suggest("ap") = %d candidates, want 2 (menu)`, len(got))
	}
	if got := p.Suggest("app", 3); len(got) != 1 || got[0].Text != "le" {
		t.Fatalf(`Suggest("app") = %+v, want single candidate Text="le" (ghost)`, got)
	}
	if got := p.Suggest("ban", 3); len(got) != 3 {
		t.Fatalf(`Suggest("ban") = %d candidates, want 3 (menu)`, len(got))
	}
	if got := p.Suggest("bandan", 6); len(got) != 1 || got[0].Text != "a" {
		t.Fatalf(`Suggest("bandan") = %+v, want single candidate Text="a" (ghost)`, got)
	}
	if got := p.Suggest("apple", 5); len(got) != 0 {
		t.Fatalf(`Suggest("apple") = %+v, want no candidates for a fully-typed word`, got)
	}
}

func keyRunes(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// TestModelWiresUpWithoutPanic drives model.Update headlessly through
// typing, ghost-accept (Tab), and menu-accept (Enter) to prove the
// Update-first / Consumed / Accepted / re-Query host-integration pattern in
// main.go actually works end-to-end.
func TestModelWiresUpWithoutPanic(t *testing.T) {
	m := initialModel()

	// Type "app" -> single candidate -> ghost "le".
	for _, r := range "app" {
		next, _ := m.Update(keyRunes(r))
		m = next.(model)
	}
	if m.cmpl.Mode() != bubblecomplete.ModeGhost || m.cmpl.Ghost() != "le" {
		t.Fatalf("after typing %q: mode=%v ghost=%q, want ModeGhost/%q", m.value, m.cmpl.Mode(), m.cmpl.Ghost(), "le")
	}

	// Tab accepts the ghost.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.value != "apple" {
		t.Fatalf("after Tab accept, value = %q, want %q", m.value, "apple")
	}
	if m.cmpl.Active() {
		t.Fatalf("completion still active after accept: mode=%v", m.cmpl.Mode())
	}

	// Reset and type "ban" -> menu with 3 rows.
	m = initialModel()
	for _, r := range "ban" {
		next, _ := m.Update(keyRunes(r))
		m = next.(model)
	}
	if m.cmpl.Mode() != bubblecomplete.ModeMenu {
		t.Fatalf("after typing %q: mode=%v, want ModeMenu", m.value, m.cmpl.Mode())
	}

	// Down selects the second row (band), Enter accepts it.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("Enter on an active menu should not quit, got a non-nil Cmd")
	}
	if m.value != "band" {
		t.Fatalf("after menu Enter accept, value = %q, want %q", m.value, "band")
	}

	// Enter with no completion active quits.
	m = initialModel()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("Enter with no completion active should return tea.Quit, got nil Cmd")
	}

	// Ctrl+C always quits.
	m = initialModel()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("Ctrl+C should return tea.Quit, got nil Cmd")
	}
}
