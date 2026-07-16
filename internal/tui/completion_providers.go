package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jbrahy/bubblecomplete"
	"github.com/jbrahy/bubblecomplete/ngram"
)

// This file implements the four bubblecomplete.Provider instances gophermind
// wires into the completion controller (Task 10). They are consulted in
// priority order — command, file, recall, markov — and the controller uses
// the first one to return a non-empty slice.
//
// Candidate contract (system-wide): Text is the TAIL to insert beyond what
// is already typed (Replace is always 0 here, since every provider below
// only ever appends). Display is the full label shown in a menu; Desc is
// the menu's right-column description. The library renders a single
// candidate as an inline ghost (using Text) and multiple candidates as a
// menu (using Display/Desc).

// tokenAtCursor returns the whitespace-delimited token ending at cursor,
// along with its rune-index start position in input. It is the shared
// "what is the user currently typing" helper for providers (fileProvider)
// that complete a single in-progress token rather than the whole line.
func tokenAtCursor(input string, cursor int) (token string, start int) {
	runes := []rune(input)
	if cursor < 0 || cursor > len(runes) {
		cursor = len(runes)
	}
	start = cursor
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	return string(runes[start:cursor]), start
}

// commandProvider completes slash-command names from the package registry
// (commands_registry.go).
type commandProvider struct{}

func newCommandProvider() *commandProvider { return &commandProvider{} }

func (p *commandProvider) Name() string { return "command" }

// Suggest is active only while the user is still typing the command name
// itself: input must start with "/" and contain no space yet (a space
// means we've moved on to arguments, which this provider doesn't handle).
func (p *commandProvider) Suggest(input string, cursor int) []bubblecomplete.Candidate {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		return nil
	}

	var out []bubblecomplete.Candidate
	for _, c := range slashCommands {
		if !strings.HasPrefix(c.Name, input) {
			continue
		}
		display := c.Name
		if c.Arg != "" {
			display += " " + c.Arg
		}
		out = append(out, bubblecomplete.Candidate{
			Text:    c.Name[len(input):],
			Display: display,
			Desc:    c.Desc,
			Replace: 0,
		})
	}

	// A single exact match with nothing left to add has nothing useful to
	// suggest.
	if len(out) == 1 && out[0].Text == "" {
		return nil
	}
	return out
}

// fileProvider completes filesystem paths under the current working
// directory.
type fileProvider struct{}

func newFileProvider() *fileProvider { return &fileProvider{} }

func (p *fileProvider) Name() string { return "file" }

// Suggest is active only when the in-progress token is path-shaped, i.e.
// contains a "/" (which covers "./", "../", "~/", and "/" prefixes, plus
// any deeper "dir/frag" token). A BARE word with no separator does NOT
// trigger file completion — this is a deliberate narrowing versus a naive
// "any filename fragment" rule, so that normal prose typing doesn't
// constantly surface filename suggestions. To complete a path, the user
// must include a separator, e.g. "internal/" or "./int".
func (p *fileProvider) Suggest(input string, cursor int) []bubblecomplete.Candidate {
	token, _ := tokenAtCursor(input, cursor)
	if !strings.Contains(token, "/") {
		return nil
	}

	idx := strings.LastIndex(token, "/")
	dirPart := token[:idx+1] // includes the trailing "/"
	fragment := token[idx+1:]

	lookupDir := dirPart
	if strings.HasPrefix(dirPart, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		lookupDir = filepath.Join(home, strings.TrimPrefix(dirPart, "~/"))
	}

	entries, err := os.ReadDir(lookupDir)
	if err != nil {
		return nil
	}

	var out []bubblecomplete.Candidate
	for _, e := range entries {
		name := e.Name()
		// Skip dotfiles unless the fragment itself starts with ".".
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(fragment, ".") {
			continue
		}
		if !strings.HasPrefix(name, fragment) {
			continue
		}

		tail := name[len(fragment):]
		display := name
		desc := "file"
		if e.IsDir() {
			tail += "/"
			display += "/"
			desc = "dir"
		}
		out = append(out, bubblecomplete.Candidate{
			Text:    tail,
			Display: display,
			Desc:    desc,
			Replace: 0,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// recallProvider suggests the most-recent prompt-history entry that has the
// current input as a strict prefix.
type recallProvider struct {
	history func() []string // oldest -> newest
}

func newRecallProvider(history func() []string) *recallProvider {
	return &recallProvider{history: history}
}

func (p *recallProvider) Name() string { return "recall" }

func (p *recallProvider) Suggest(input string, cursor int) []bubblecomplete.Candidate {
	if strings.HasPrefix(input, "/") || strings.TrimSpace(input) == "" {
		return nil
	}

	entries := p.history()
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if len(e) > len(input) && strings.HasPrefix(e, input) {
			return []bubblecomplete.Candidate{{
				Text:    e[len(input):],
				Display: e,
				Replace: 0,
			}}
		}
	}
	return nil
}

// markovProvider predicts the next word from an n-gram model trained on
// prompt history.
type markovProvider struct {
	model *ngram.Model
}

func newMarkovProvider(model *ngram.Model) *markovProvider {
	return &markovProvider{model: model}
}

func (p *markovProvider) Name() string { return "markov" }

func (p *markovProvider) Suggest(input string, cursor int) []bubblecomplete.Candidate {
	if input == "" || strings.HasPrefix(input, "/") {
		return nil
	}

	words := strings.Fields(input)
	if len(words) == 0 {
		return nil
	}
	if len(words) > 2 {
		words = words[len(words)-2:]
	}

	word, ok := p.model.Predict(words)
	if !ok {
		return nil
	}

	text := word
	if !endsWithSpace(input) {
		text = " " + word
	}
	return []bubblecomplete.Candidate{{
		Text:    text,
		Display: text,
		Replace: 0,
	}}
}

// endsWithSpace reports whether s's last rune is whitespace.
func endsWithSpace(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	return unicode.IsSpace(r[len(r)-1])
}

var (
	_ bubblecomplete.Provider = (*commandProvider)(nil)
	_ bubblecomplete.Provider = (*fileProvider)(nil)
	_ bubblecomplete.Provider = (*recallProvider)(nil)
	_ bubblecomplete.Provider = (*markovProvider)(nil)
)
