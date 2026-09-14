package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gophermind/gophermind-lib/freellm"
)

// osc8 wraps text in an OSC 8 terminal hyperlink. Terminals without OSC 8
// support render the text unchanged, so this degrades rather than corrupts.
func osc8(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// hyperlinkTerminal reports whether the terminal is known to render OSC 8
// hyperlinks. GOPHERMIND_HYPERLINKS forces it on for a terminal not on the
// list. Unknown terminals render the plain model name, exactly as before.
func hyperlinkTerminal() bool {
	if os.Getenv("GOPHERMIND_HYPERLINKS") != "" {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "ghostty", "vscode":
		return true
	}
	return false
}

// providerCard renders the /provider readout: who is serving the current
// model, the free-tier terms, the lifetime odometer, and the trip meters.
func providerCard(profile, model, odoPath string, now time.Time) string {
	c, ok := freellm.CompatFor(profile)
	if !ok {
		return fmt.Sprintf("Model %s: profile %q is not a free provider.\nRun `gophermind free list` to see the free options.", model, profile)
	}
	a, _ := freellm.AttributionFor(profile, model)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", a.Line())
	fmt.Fprintf(&b, "Endpoint:  %s\n", c.BaseURL)
	if t := strings.Join(freellm.TermsFlags(c.Terms), "; "); t != "" {
		fmt.Fprintf(&b, "Terms:     %s\n", t)
	}
	if c.Note != "" {
		fmt.Fprintf(&b, "Note:      %s\n", c.Note)
	}

	odo, err := freellm.LoadOdometer(odoPath)
	if err == nil {
		tokens, requests := odo.Reading()
		fmt.Fprintf(&b, "\nFree odometer: %s tokens over %s requests (all free providers)\n", freellm.Commas(tokens), freellm.Commas(requests))
		var parts []string
		for _, m := range freellm.TripMeters(odo, c, now) {
			s := m.String()
			if m.Warn() {
				s += " <- near the limit"
			}
			parts = append(parts, s)
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, "This window:   %s\n", strings.Join(parts, "   "))
		}
	}
	return b.String()
}
