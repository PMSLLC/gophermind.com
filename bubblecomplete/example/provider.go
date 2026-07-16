package main

import (
	"strings"

	"github.com/jbrahy/bubblecomplete"
)

// wordProvider is a toy Provider backed by a static word list. It completes
// the "word" (the run of non-space runes) immediately before the cursor.
type wordProvider struct {
	words []string
}

func (p *wordProvider) Name() string { return "words" }

// Suggest returns one Candidate per word in the list that starts with the
// in-progress word and is strictly longer than it (so a fully-typed word
// never suggests itself). Candidate.Text is the remaining tail beyond what
// was typed, and Replace is always 0 since we only ever append.
func (p *wordProvider) Suggest(input string, cursor int) []bubblecomplete.Candidate {
	runes := []rune(input)
	if cursor < 0 || cursor > len(runes) {
		cursor = len(runes)
	}

	start := cursor
	for start > 0 && runes[start-1] != ' ' {
		start--
	}
	prefix := string(runes[start:cursor])
	if prefix == "" {
		return nil
	}

	var out []bubblecomplete.Candidate
	for _, w := range p.words {
		if len(w) <= len(prefix) || !strings.HasPrefix(w, prefix) {
			continue
		}
		out = append(out, bubblecomplete.Candidate{
			Text:    w[len(prefix):],
			Display: w,
			Replace: 0,
		})
	}
	return out
}

var _ bubblecomplete.Provider = (*wordProvider)(nil)
