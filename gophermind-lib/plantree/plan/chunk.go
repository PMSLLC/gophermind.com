// Package plan turns a brief into the plantree skeleton, one bounded slice at
// a time. Every pass runs in a fresh context and everything durable lives in
// the tree, so a run can stop and resume without conversation history.
package plan

import (
	"strings"
	"unicode/utf8"
)

// DefaultChunkBytes bounds one chunk of the brief (about 3,000 tokens).
const DefaultChunkBytes = 12000

// Chunk is one bounded slice of a brief.
type Chunk struct {
	Index int
	Title string // the nearest heading at the chunk's start, "" before the first
	Text  string
}

type section struct {
	title string
	text  string
}

// SplitBrief splits brief into chunks of at most maxBytes without dropping or
// changing any text: joining the chunks' Text values reproduces brief exactly.
// Headings ("# " to "###### ") start new sections, sections are packed
// together while they fit, and a section larger than maxBytes is split at
// blank lines, then line ends, then rune boundaries. A "#" line inside a code
// fence is treated as a heading; that only changes where chunks break.
func SplitBrief(brief string, maxBytes int) []Chunk {
	if strings.TrimSpace(brief) == "" {
		return nil
	}
	if maxBytes < 1 {
		maxBytes = DefaultChunkBytes
	}

	var secs []section
	for _, line := range strings.SplitAfter(brief, "\n") {
		if line == "" {
			continue
		}
		title, isHeading := headingTitle(line)
		if isHeading || len(secs) == 0 {
			secs = append(secs, section{title: title})
		}
		secs[len(secs)-1].text += line
	}

	var chunks []Chunk
	var buf strings.Builder
	title := ""
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		chunks = append(chunks, Chunk{Index: len(chunks), Title: title, Text: buf.String()})
		buf.Reset()
	}
	for _, s := range secs {
		if len(s.text) > maxBytes {
			flush()
			for _, piece := range splitLarge(s.text, maxBytes) {
				chunks = append(chunks, Chunk{Index: len(chunks), Title: s.title, Text: piece})
			}
			continue
		}
		if buf.Len() > 0 && buf.Len()+len(s.text) > maxBytes {
			flush()
		}
		if buf.Len() == 0 {
			title = s.title
		}
		buf.WriteString(s.text)
	}
	flush()
	return chunks
}

// headingTitle reports whether line is a markdown heading and returns its text.
func headingTitle(line string) (string, bool) {
	t := strings.TrimRight(line, "\r\n")
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(t) || t[n] != ' ' {
		return "", false
	}
	return strings.TrimSpace(t[n+1:]), true
}

// splitLarge cuts text into pieces of at most max bytes.
func splitLarge(text string, max int) []string {
	var out []string
	rest := text
	for len(rest) > max {
		cut := cutPoint(rest, max)
		out = append(out, rest[:cut])
		rest = rest[cut:]
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

// cutPoint picks where to cut the first max bytes of rest, which must be
// longer than max: after the last blank line, else after the last newline,
// else at a rune boundary.
func cutPoint(rest string, max int) int {
	w := rest[:max]
	if i := strings.LastIndex(w, "\n\n"); i > 0 {
		return i + 2
	}
	if i := strings.LastIndex(w, "\n"); i > 0 {
		return i + 1
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(rest[cut]) {
		cut--
	}
	if cut == 0 {
		cut = max
	}
	return cut
}
