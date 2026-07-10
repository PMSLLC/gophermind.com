// Package banner composes the startup splash shown under the gopher: the ASCII
// art, the build version, the most recent changelog entries, and a random
// fortune. Render is called once per session so the fortune stays put.
package banner

import (
	"strings"

	root "gophermind"
	"gophermind/internal/fortune"
	"gophermind/internal/prompt"
	"gophermind/internal/tips"
	"gophermind/internal/version"
)

// Options controls optional banner sections.
type Options struct {
	Fortune bool // include a random fortune under the banner
	Tip     bool // include a rotating tip-of-the-day line
}

// Render builds the full startup banner string, including a fortune and a tip.
func Render() string {
	return RenderWith(Options{Fortune: true, Tip: true})
}

// RenderWith builds the startup banner, honoring the given options (e.g.
// --fortune off suppresses the fortune while keeping art/version/changes).
func RenderWith(o Options) string {
	var b strings.Builder
	b.WriteString(prompt.GopherArt)
	b.WriteString("\n")
	b.WriteString(version.String())
	b.WriteByte('\n')

	if changes := LatestChanges(root.Changelog, 3); len(changes) > 0 {
		b.WriteString("\nRecent changes:\n")
		for _, c := range changes {
			b.WriteString("  • " + c + "\n")
		}
	}

	if o.Tip {
		b.WriteString("\n💡 " + tips.Random() + "\n")
	}

	if o.Fortune {
		if f := fortune.Random(); f != "" {
			b.WriteString("\n" + f + "\n")
		}
	}
	return b.String()
}

// LatestChanges returns up to n bullet entries from the most recent non-empty
// version section of a Keep a Changelog document. "###" subsection headers
// (Added/Changed/Fixed) and an empty "[Unreleased]" section are skipped.
func LatestChanges(md string, n int) []string {
	var out []string
	started := false
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			if len(out) > 0 {
				return out // reached the next version; the first non-empty one is done
			}
			started = true
		case !started:
			// preamble before the first version heading
		case strings.HasPrefix(t, "### "):
			// subsection header (Added/Changed/Fixed) — skip
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
			out = append(out, strings.TrimSpace(t[2:]))
			if len(out) >= n {
				return out
			}
		}
	}
	return out
}
