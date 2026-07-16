package bubblecomplete

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain forces a color profile on lipgloss's default renderer. Test runs
// aren't attached to a tty, so lipgloss would otherwise auto-detect
// no-color/ascii and silently drop styling (Bold/Reverse become no-ops),
// making View()'s highlighting untestable. This only affects the test
// binary's rendering, not library behavior in a real terminal.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI)
	os.Exit(m.Run())
}

func TestQueryModeSelection(t *testing.T) {
	tests := []struct {
		name       string
		candidates []Candidate
		wantMode   Mode
		wantGhost  string
	}{
		{
			name:       "zero candidates -> ModeNone",
			candidates: []Candidate{},
			wantMode:   ModeNone,
			wantGhost:  "",
		},
		{
			name:       "one candidate -> ModeGhost",
			candidates: []Candidate{{Text: "world"}},
			wantMode:   ModeGhost,
			wantGhost:  "world",
		},
		{
			name: "multiple candidates -> ModeMenu",
			candidates: []Candidate{
				{Text: "foo", Display: "foo"},
				{Text: "bar", Display: "bar"},
			},
			wantMode:  ModeMenu,
			wantGhost: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetProviders(&staticProvider{name: "p", candidates: tt.candidates})
			m = m.Query("hello ", 6)

			if m.Mode() != tt.wantMode {
				t.Errorf("Mode() = %v, want %v", m.Mode(), tt.wantMode)
			}
			if got := m.Ghost(); got != tt.wantGhost {
				t.Errorf("Ghost() = %q, want %q", got, tt.wantGhost)
			}
			wantActive := tt.wantMode != ModeNone
			if m.Active() != wantActive {
				t.Errorf("Active() = %v, want %v", m.Active(), wantActive)
			}
			if tt.wantMode != ModeMenu && m.View() != "" {
				t.Errorf("View() = %q, want empty for mode %v", m.View(), tt.wantMode)
			}
			if tt.wantMode == ModeMenu {
				view := m.View()
				for _, c := range tt.candidates {
					if !strings.Contains(view, c.Display) {
						t.Errorf("View() = %q, want it to contain %q", view, c.Display)
					}
				}
			}
		})
	}
}

func TestQueryFirstNonEmptyProviderWins(t *testing.T) {
	empty := &staticProvider{name: "empty", candidates: []Candidate{}}
	first := &staticProvider{name: "first", candidates: []Candidate{{Text: "alpha"}}}
	second := &staticProvider{name: "second", candidates: []Candidate{{Text: "beta"}, {Text: "gamma"}}}

	m := New()
	m.SetProviders(empty, first, second)
	m = m.Query("a", 1)

	if m.Mode() != ModeGhost {
		t.Fatalf("Mode() = %v, want ModeGhost (first provider should win)", m.Mode())
	}
	if got := m.Ghost(); got != "alpha" {
		t.Errorf("Ghost() = %q, want %q (second provider's candidates should be ignored)", got, "alpha")
	}
}

func TestQueryFirstProviderReturningEmptyIsSkipped(t *testing.T) {
	empty1 := &staticProvider{name: "e1", candidates: nil}
	empty2 := &staticProvider{name: "e2", candidates: []Candidate{}}
	winner := &staticProvider{name: "w", candidates: []Candidate{{Text: "only"}}}

	m := New()
	m.SetProviders(empty1, empty2, winner)
	m = m.Query("o", 1)

	if m.Mode() != ModeGhost || m.Ghost() != "only" {
		t.Errorf("expected ModeGhost with ghost %q, got mode=%v ghost=%q", "only", m.Mode(), m.Ghost())
	}
}

func TestUpdateTabAcceptsGhost(t *testing.T) {
	m := New()
	m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
	m = m.Query("hello ", 6)

	nm, res := m.Update(tea.KeyMsg{Type: tea.KeyTab})

	if !res.Consumed {
		t.Fatalf("Consumed = false, want true")
	}
	if res.Accepted == nil || res.Accepted.Text != "world" {
		t.Fatalf("Accepted = %+v, want Text=world", res.Accepted)
	}
	if nm.Mode() != ModeNone {
		t.Errorf("Mode() after accept = %v, want ModeNone", nm.Mode())
	}
	if nm.Active() {
		t.Errorf("Active() after accept = true, want false")
	}
}

func TestUpdateTabAcceptsSelectedMenuItem(t *testing.T) {
	candidates := []Candidate{
		{Text: "one", Display: "one"},
		{Text: "two", Display: "two"},
		{Text: "three", Display: "three"},
	}
	m := New()
	m.SetProviders(&staticProvider{name: "p", candidates: candidates})
	m = m.Query("t", 1)

	// Move selection down twice: 0 -> 1 -> 2.
	m, res := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !res.Consumed || res.Accepted != nil {
		t.Fatalf("Down: Consumed=%v Accepted=%v, want Consumed=true Accepted=nil", res.Consumed, res.Accepted)
	}
	m, res = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !res.Consumed || res.Accepted != nil {
		t.Fatalf("Down: Consumed=%v Accepted=%v, want Consumed=true Accepted=nil", res.Consumed, res.Accepted)
	}

	m, res = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !res.Consumed {
		t.Fatalf("Consumed = false, want true")
	}
	if res.Accepted == nil || res.Accepted.Text != "three" {
		t.Fatalf("Accepted = %+v, want Text=three", res.Accepted)
	}
	if m.Mode() != ModeNone {
		t.Errorf("Mode() after accept = %v, want ModeNone", m.Mode())
	}
}

func TestUpdateRightAcceptsGhostOnlyAtEndOfInput(t *testing.T) {
	t.Run("cursor at end accepts", func(t *testing.T) {
		m := New()
		m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
		m = m.Query("hello ", 6) // cursor == rune length of input

		nm, res := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		if !res.Consumed {
			t.Fatalf("Consumed = false, want true")
		}
		if res.Accepted == nil || res.Accepted.Text != "world" {
			t.Fatalf("Accepted = %+v, want Text=world", res.Accepted)
		}
		if nm.Mode() != ModeNone {
			t.Errorf("Mode() after accept = %v, want ModeNone", nm.Mode())
		}
	})

	t.Run("cursor not at end does not consume", func(t *testing.T) {
		m := New()
		m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
		m = m.Query("hello ", 3) // cursor mid-string

		nm, res := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		if res.Consumed {
			t.Fatalf("Consumed = true, want false")
		}
		if res.Accepted != nil {
			t.Fatalf("Accepted = %+v, want nil", res.Accepted)
		}
		if nm.Mode() != ModeGhost {
			t.Errorf("Mode() = %v, want ModeGhost (should remain active)", nm.Mode())
		}
	})

	t.Run("menu mode right is never consumed", func(t *testing.T) {
		m := New()
		m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "a"}, {Text: "b"}}})
		m = m.Query("x", 1)

		_, res := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		if res.Consumed {
			t.Fatalf("Consumed = true, want false in ModeMenu")
		}
	})
}

func TestUpdateUpDownMovesSelectionAndClamps(t *testing.T) {
	candidates := []Candidate{
		{Text: "one", Display: "one"},
		{Text: "two", Display: "two"},
		{Text: "three", Display: "three"},
	}
	m := New()
	m.SetProviders(&staticProvider{name: "p", candidates: candidates})
	m = m.Query("t", 1)

	// Up at index 0 should clamp at 0.
	m, res := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !res.Consumed {
		t.Fatalf("Up: Consumed = false, want true")
	}
	if _, cand := m.Accept(); cand == nil || cand.Text != "one" {
		t.Fatalf("after clamped Up, Accept() = %+v, want one", cand)
	}

	// Re-query to reset selection, then move down past the end.
	m = m.Query("t", 1)
	for i := 0; i < 5; i++ {
		var r Result
		m, r = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if !r.Consumed {
			t.Fatalf("Down iteration %d: Consumed = false, want true", i)
		}
	}
	if _, cand := m.Accept(); cand == nil || cand.Text != "three" {
		t.Fatalf("after clamped Down, Accept() = %+v, want three", cand)
	}
}

func TestUpdateUpDownNotConsumedOutsideMenu(t *testing.T) {
	m := New()
	m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
	m = m.Query("hello ", 6)

	_, res := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if res.Consumed {
		t.Errorf("Up in ModeGhost: Consumed = true, want false")
	}
	_, res = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if res.Consumed {
		t.Errorf("Down in ModeGhost: Consumed = true, want false")
	}
}

func TestUpdateEscDismisses(t *testing.T) {
	t.Run("active dismisses and consumes", func(t *testing.T) {
		m := New()
		m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
		m = m.Query("hello ", 6)

		nm, res := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if !res.Consumed {
			t.Fatalf("Consumed = false, want true")
		}
		if res.Accepted != nil {
			t.Fatalf("Accepted = %+v, want nil", res.Accepted)
		}
		if nm.Active() {
			t.Errorf("Active() after Esc = true, want false")
		}
	})

	t.Run("inactive does not consume", func(t *testing.T) {
		m := New() // no providers, nothing queried: ModeNone
		_, res := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if res.Consumed {
			t.Errorf("Consumed = true, want false when inactive")
		}
	})
}

func TestUpdateNeverConsumesEnterVariants(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyEnter, Alt: true},
		{Type: tea.KeyCtrlJ},
	}

	setups := map[string]func() Model{
		"ModeNone": func() Model {
			return New()
		},
		"ModeGhost": func() Model {
			m := New()
			m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
			return m.Query("hello ", 6)
		},
		"ModeMenu": func() Model {
			m := New()
			m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "a"}, {Text: "b"}}})
			return m.Query("x", 1)
		},
	}

	for modeName, setup := range setups {
		for _, k := range keys {
			t.Run(modeName+"/"+k.String(), func(t *testing.T) {
				m := setup()
				wantMode := m.Mode()
				nm, res := m.Update(k)
				if res.Consumed {
					t.Errorf("Consumed = true, want false for key %v in %s", k, modeName)
				}
				if res.Accepted != nil {
					t.Errorf("Accepted = %+v, want nil for key %v in %s", res.Accepted, k, modeName)
				}
				if nm.Mode() != wantMode {
					t.Errorf("Mode() changed from %v to %v for key %v in %s, want unchanged", wantMode, nm.Mode(), k, modeName)
				}
			})
		}
	}
}

func TestAccept(t *testing.T) {
	t.Run("inactive returns nil", func(t *testing.T) {
		m := New()
		nm, cand := m.Accept()
		if cand != nil {
			t.Fatalf("Accept() = %+v, want nil", cand)
		}
		if nm.Mode() != ModeNone {
			t.Errorf("Mode() = %v, want ModeNone", nm.Mode())
		}
	})

	t.Run("ghost returns the ghost candidate", func(t *testing.T) {
		m := New()
		m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
		m = m.Query("hello ", 6)

		nm, cand := m.Accept()
		if cand == nil || cand.Text != "world" {
			t.Fatalf("Accept() = %+v, want Text=world", cand)
		}
		if nm.Active() {
			t.Errorf("Active() after Accept = true, want false")
		}
	})

	t.Run("menu returns the selected candidate", func(t *testing.T) {
		m := New()
		m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{
			{Text: "one"}, {Text: "two"}, {Text: "three"},
		}})
		m = m.Query("t", 1)
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})

		nm, cand := m.Accept()
		if cand == nil || cand.Text != "two" {
			t.Fatalf("Accept() = %+v, want Text=two", cand)
		}
		if nm.Active() {
			t.Errorf("Active() after Accept = true, want false")
		}
	})
}

func TestViewHighlightsSelectedRow(t *testing.T) {
	m := New()
	m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{
		{Text: "one", Display: "one"},
		{Text: "two", Display: "two"},
	}})
	m = m.Query("t", 1)

	viewSelectedFirst := m.View()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	viewSelectedSecond := m.View()

	if viewSelectedFirst == viewSelectedSecond {
		t.Errorf("View() did not change when selection moved; got identical output for both selections")
	}
	if !strings.Contains(viewSelectedFirst, "one") || !strings.Contains(viewSelectedFirst, "two") {
		t.Errorf("View() = %q, want it to contain both candidate labels", viewSelectedFirst)
	}
}

func TestViewEmptyOutsideMenuMode(t *testing.T) {
	m := New() // ModeNone
	if v := m.View(); v != "" {
		t.Errorf("View() in ModeNone = %q, want empty", v)
	}

	m.SetProviders(&staticProvider{name: "p", candidates: []Candidate{{Text: "world"}}})
	m = m.Query("hello ", 6) // ModeGhost
	if v := m.View(); v != "" {
		t.Errorf("View() in ModeGhost = %q, want empty", v)
	}
}

func TestGhostStyleReturnsInjectedStyle(t *testing.T) {
	m := New()
	if s := m.GhostStyle(); s.GetFaint() != true {
		t.Errorf("default GhostStyle() faint = %v, want true", s.GetFaint())
	}
}
