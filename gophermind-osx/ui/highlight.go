package ui

import (
	"regexp"
	"strings"
)

// Color is an RGB color for a highlighted span. The zero value means "use
// the transcript's default text color" -- chatview.go's rendering never
// treats {0,0,0} as "black" specially, only as "unset."
type Color struct{ R, G, B uint8 }

// Span is one contiguously-colored run of code text.
type Span struct {
	Text  string
	Color Color
	Bold  bool
}

var (
	colorKeyword = Color{R: 0xC5, G: 0x86, B: 0xC0} // purple
	colorString  = Color{R: 0x6A, G: 0x99, B: 0x55} // green
	colorComment = Color{R: 0x80, G: 0x80, B: 0x80} // gray
	colorNumber  = Color{R: 0xD1, G: 0x9A, B: 0x66} // orange
)

// keywordSets covers a handful of common languages well enough for a chat
// transcript's code blocks to read clearly -- this is a lightweight
// tokenizer for legible highlighting, not a real language parser (it
// doesn't understand nested strings-in-templates, raw strings' exact
// escaping rules per language, or anything requiring a real grammar).
// Unrecognized languages (and blocks with no language tag) fall back to
// genericKeywords, which still highlights strings/comments/numbers
// reasonably since those are similar enough across most C-like and
// scripting languages.
var keywordSets = map[string]map[string]bool{
	"go":         setOf("func", "return", "if", "else", "for", "range", "var", "const", "type", "struct", "interface", "package", "import", "go", "chan", "select", "case", "switch", "default", "defer", "map", "nil", "true", "false", "err", "error"),
	"python":     setOf("def", "return", "if", "elif", "else", "for", "in", "while", "import", "from", "class", "try", "except", "finally", "with", "as", "lambda", "None", "True", "False", "self"),
	"javascript": setOf("function", "return", "if", "else", "for", "while", "const", "let", "var", "class", "import", "export", "async", "await", "try", "catch", "finally", "new", "this", "null", "undefined", "true", "false"),
	"typescript": setOf("function", "return", "if", "else", "for", "while", "const", "let", "var", "class", "interface", "type", "import", "export", "async", "await", "try", "catch", "finally", "new", "this", "null", "undefined", "true", "false"),
}

var genericKeywords = setOf("if", "else", "for", "while", "return", "function", "def", "class", "import", "true", "false", "null", "nil", "none")

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// tokenRe splits code into candidate tokens: quoted strings (single,
// double, or backtick), line comments (// or #), block comments (/* */),
// numbers, identifiers/keywords, and everything else char-by-char.
var tokenRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|` + "`[^`]*`" +
	`|//[^\n]*|#[^\n]*|/\*[\s\S]*?\*/|\b\d+(?:\.\d+)?\b|[A-Za-z_][A-Za-z0-9_]*|\s+|.`)

// HighlightCode tokenizes code (best-effort; see keywordSets' doc comment)
// for language lang, returning colored spans in order. lang is matched
// case-insensitively against keywordSets; an unrecognized or empty lang
// uses genericKeywords, which still colors strings/comments/numbers.
func HighlightCode(code, lang string) []Span {
	keywords, ok := keywordSets[strings.ToLower(lang)]
	if !ok {
		keywords = genericKeywords
	}

	var spans []Span
	for _, tok := range tokenRe.FindAllString(code, -1) {
		switch {
		case isQuoted(tok):
			spans = append(spans, Span{Text: tok, Color: colorString})
		case strings.HasPrefix(tok, "//") || strings.HasPrefix(tok, "#") || strings.HasPrefix(tok, "/*"):
			spans = append(spans, Span{Text: tok, Color: colorComment})
		case isNumber(tok):
			spans = append(spans, Span{Text: tok, Color: colorNumber})
		case keywords[tok]:
			spans = append(spans, Span{Text: tok, Color: colorKeyword, Bold: true})
		default:
			spans = append(spans, Span{Text: tok})
		}
	}
	return mergeAdjacent(spans)
}

func isQuoted(s string) bool {
	if len(s) < 2 {
		return false
	}
	q := s[0]
	return (q == '"' || q == '\'' || q == '`') && s[len(s)-1] == q
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// mergeAdjacent coalesces consecutive spans with identical styling (mostly
// runs of unstyled whitespace/punctuation from the token regex's
// char-by-char fallback), reducing the span count a renderer has to walk.
func mergeAdjacent(spans []Span) []Span {
	if len(spans) == 0 {
		return spans
	}
	out := spans[:1]
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if last.Color == s.Color && last.Bold == s.Bold {
			last.Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

// Block is one piece of a message's text: either plain prose or a fenced
// code block (```lang\n...\n```).
type Block struct {
	IsCode bool
	Lang   string // only meaningful when IsCode
	Text   string
}

var fenceRe = regexp.MustCompile("(?s)```([A-Za-z0-9_+-]*)\\n?(.*?)```")

// ExtractBlocks splits a message's text into alternating prose/code
// blocks, covering "code blocks displayed with syntax highlighting":
// chatview.go renders IsCode blocks through HighlightCode and everything
// else as plain text.
func ExtractBlocks(text string) []Block {
	var blocks []Block
	last := 0
	for _, loc := range fenceRe.FindAllStringSubmatchIndex(text, -1) {
		start, end := loc[0], loc[1]
		langStart, langEnd := loc[2], loc[3]
		codeStart, codeEnd := loc[4], loc[5]

		if start > last {
			blocks = append(blocks, Block{Text: text[last:start]})
		}
		blocks = append(blocks, Block{IsCode: true, Lang: text[langStart:langEnd], Text: text[codeStart:codeEnd]})
		last = end
	}
	if last < len(text) {
		blocks = append(blocks, Block{Text: text[last:]})
	}
	return blocks
}
