package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"gophermind/internal/agent"
	"gophermind/internal/prompt"
)

// TestBannerSurvivesFirstWindowSize reproduces the real startup sequence: a
// fresh (not-yet-ready) model receives its first WindowSizeMsg, which flips
// m.ready. The banner must remain visible in the ready view, not vanish.
func TestBannerSurvivesFirstWindowSize(t *testing.T) {
	m := newModel(func(sub chan tea.Msg, allowed *allowSet) *agent.Agent { return nil }, "m", "auto", "dark")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rm := updated.(model)

	if !rm.ready {
		t.Fatal("model not ready after first WindowSizeMsg")
	}

	// A short, distinctive slice of the art (the gopher's buck teeth) that sits
	// well within the 80-column viewport, so it survives truncation.
	const needle = "|==|"
	if !strings.Contains(prompt.GopherArt, needle) {
		t.Fatalf("needle %q not in art; pick a new one", needle)
	}
	if !strings.Contains(rm.View(), needle) {
		t.Errorf("banner not present in ready view; want substring %q", needle)
	}
}
