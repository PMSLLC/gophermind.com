package ui

import (
	"strings"
	"testing"
)

func TestHighlightMarkdown(t *testing.T) {
	md := "# Title\n\nsome prose\n\n```go\nfunc main() {}\n```\n> a quote\n"
	out := HighlightMarkdown(md)

	if !strings.Contains(out, ansiBold+ansiCyan+"# Title") {
		t.Errorf("heading should be bold cyan:\n%q", out)
	}
	if !strings.Contains(out, ansiYellow+"func main() {}") {
		t.Errorf("code block content should be yellow:\n%q", out)
	}
	if !strings.Contains(out, ansiDim+"> a quote") {
		t.Errorf("blockquote should be dimmed:\n%q", out)
	}
	if !strings.Contains(out, "\nsome prose\n") {
		t.Errorf("prose should be unstyled:\n%q", out)
	}
	if HighlightMarkdown("") != "" {
		t.Error("empty input should stay empty")
	}
}
