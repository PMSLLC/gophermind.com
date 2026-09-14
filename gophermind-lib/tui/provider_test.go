package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderCardShowsAttributionAndOdometer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odo.json")
	card := providerCard("free-groq", "openai/gpt-oss-120b", path, time.Now())
	for _, want := range []string{"Groq", "https://groq.com", "openai/gpt-oss-120b", "odometer"} {
		if !strings.Contains(strings.ToLower(card), strings.ToLower(want)) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
}

func TestProviderCardForPaidProfile(t *testing.T) {
	card := providerCard("openai", "gpt-4o-mini", filepath.Join(t.TempDir(), "odo.json"), time.Now())
	if !strings.Contains(strings.ToLower(card), "not a free provider") {
		t.Errorf("paid profile should say so:\n%s", card)
	}
}

func TestOSC8WrapsWhenEnabled(t *testing.T) {
	got := osc8("https://groq.com", "model-x")
	if !strings.Contains(got, "\x1b]8;;https://groq.com") || !strings.Contains(got, "model-x") {
		t.Errorf("osc8 did not produce a hyperlink: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b]8;;\x1b\\") {
		t.Errorf("osc8 did not terminate the hyperlink: %q", got)
	}
}

// TestHyperlinkTerminal covers the three branches of hyperlinkTerminal: the
// env override forcing it on, a known terminal, and an unknown one.
func TestHyperlinkTerminal(t *testing.T) {
	t.Run("env override forces it on", func(t *testing.T) {
		t.Setenv("GOPHERMIND_HYPERLINKS", "1")
		t.Setenv("TERM_PROGRAM", "")
		if !hyperlinkTerminal() {
			t.Error("GOPHERMIND_HYPERLINKS set should force hyperlinkTerminal true")
		}
	})

	t.Run("known terminal", func(t *testing.T) {
		t.Setenv("GOPHERMIND_HYPERLINKS", "")
		t.Setenv("TERM_PROGRAM", "iTerm.app")
		if !hyperlinkTerminal() {
			t.Error("iTerm.app should report hyperlinkTerminal true")
		}
	})

	t.Run("unknown terminal", func(t *testing.T) {
		t.Setenv("GOPHERMIND_HYPERLINKS", "")
		t.Setenv("TERM_PROGRAM", "some-unknown-terminal")
		if hyperlinkTerminal() {
			t.Error("unknown terminal should report hyperlinkTerminal false")
		}
	})
}
