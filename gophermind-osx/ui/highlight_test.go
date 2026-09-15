package ui

import (
	"strings"
	"testing"
)

// spanText reassembles spans' text, so tests can check the tokenizer
// didn't drop or duplicate any source text.
func spanText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// colorAt finds the color of the span containing substr (the first
// occurrence), for asserting a specific token got a specific color
// without hardcoding exact span boundaries.
func colorAt(t *testing.T, spans []Span, substr string) Color {
	t.Helper()
	for _, s := range spans {
		if strings.Contains(s.Text, substr) {
			return s.Color
		}
	}
	t.Fatalf("no span contains %q in %+v", substr, spans)
	return Color{}
}

// TestHighlightCode_PreservesAllSourceText covers a basic soundness
// property: whatever the tokenizer's classification, no source text is
// dropped, added, or reordered.
func TestHighlightCode_PreservesAllSourceText(t *testing.T) {
	code := `func main() {
	// say hi
	fmt.Println("hello, world")
	x := 42
}`
	spans := HighlightCode(code, "go")
	if got := spanText(spans); got != code {
		t.Errorf("reassembled text does not match source:\ngot:  %q\nwant: %q", got, code)
	}
}

func TestHighlightCode_KeywordsColored(t *testing.T) {
	spans := HighlightCode("func main() { return }", "go")
	if c := colorAt(t, spans, "func"); c != colorKeyword {
		t.Errorf("'func' color = %v, want keyword color", c)
	}
	if c := colorAt(t, spans, "return"); c != colorKeyword {
		t.Errorf("'return' color = %v, want keyword color", c)
	}
}

func TestHighlightCode_StringsColored(t *testing.T) {
	spans := HighlightCode(`x = "hello world"`, "python")
	if c := colorAt(t, spans, `"hello world"`); c != colorString {
		t.Errorf("string literal color = %v, want string color", c)
	}
}

func TestHighlightCode_CommentsColored(t *testing.T) {
	for _, tc := range []struct {
		lang, code, comment string
	}{
		{"go", "x := 1 // a comment", "// a comment"},
		{"python", "x = 1  # a comment", "# a comment"},
	} {
		spans := HighlightCode(tc.code, tc.lang)
		if c := colorAt(t, spans, tc.comment); c != colorComment {
			t.Errorf("%s comment color = %v, want comment color", tc.lang, c)
		}
	}
}

func TestHighlightCode_NumbersColored(t *testing.T) {
	spans := HighlightCode("x = 42", "python")
	if c := colorAt(t, spans, "42"); c != colorNumber {
		t.Errorf("number color = %v, want number color", c)
	}
}

// TestHighlightCode_UnknownLanguageFallsBackToGeneric covers "code blocks
// displayed with syntax highlighting" for a language not in keywordSets:
// it must not error or panic, and strings/comments still highlight.
func TestHighlightCode_UnknownLanguageFallsBackToGeneric(t *testing.T) {
	spans := HighlightCode(`if x { print("hi") } // note`, "some-made-up-language")
	if got := spanText(spans); got != `if x { print("hi") } // note` {
		t.Errorf("text mismatch: %q", got)
	}
	if c := colorAt(t, spans, "// note"); c != colorComment {
		t.Errorf("comment not highlighted in unknown language: %v", c)
	}
}

func TestHighlightCode_EmptyLanguageUsesGeneric(t *testing.T) {
	spans := HighlightCode(`return true`, "")
	if c := colorAt(t, spans, "return"); c != colorKeyword {
		t.Errorf("'return' color = %v, want keyword color even with empty lang", c)
	}
}

// TestExtractBlocks_PlainTextOnly covers a message with no code fences.
func TestExtractBlocks_PlainTextOnly(t *testing.T) {
	blocks := ExtractBlocks("just some prose, no code here")
	if len(blocks) != 1 || blocks[0].IsCode {
		t.Errorf("blocks = %+v, want one non-code block", blocks)
	}
}

// TestExtractBlocks_MixedProseAndCode covers the real case this exists
// for: an assistant message with prose before/after a fenced code block.
func TestExtractBlocks_MixedProseAndCode(t *testing.T) {
	text := "Here's the fix:\n```go\nfunc f() {}\n```\nThat should work."
	blocks := ExtractBlocks(text)
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3: %+v", len(blocks), blocks)
	}
	if blocks[0].IsCode || !strings.Contains(blocks[0].Text, "Here's the fix") {
		t.Errorf("blocks[0] = %+v", blocks[0])
	}
	if !blocks[1].IsCode || blocks[1].Lang != "go" || strings.TrimSpace(blocks[1].Text) != "func f() {}" {
		t.Errorf("blocks[1] = %+v", blocks[1])
	}
	if blocks[2].IsCode || !strings.Contains(blocks[2].Text, "should work") {
		t.Errorf("blocks[2] = %+v", blocks[2])
	}
}

func TestExtractBlocks_CodeBlockWithNoLanguage(t *testing.T) {
	blocks := ExtractBlocks("```\nplain code\n```")
	if len(blocks) != 1 || !blocks[0].IsCode || blocks[0].Lang != "" {
		t.Errorf("blocks = %+v", blocks)
	}
}

func TestExtractBlocks_MultipleCodeBlocks(t *testing.T) {
	text := "```go\na()\n```\nmiddle\n```python\nb()\n```"
	blocks := ExtractBlocks(text)
	var codeCount int
	for _, b := range blocks {
		if b.IsCode {
			codeCount++
		}
	}
	if codeCount != 2 {
		t.Errorf("found %d code blocks, want 2: %+v", codeCount, blocks)
	}
}
