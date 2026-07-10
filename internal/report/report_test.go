package report

import (
	"strings"
	"testing"
	"time"
)

func TestHTMLContainsContent(t *testing.T) {
	html := HTML(Data{
		Task:             "Refactor the parser",
		Answer:           "Done: split parser.go into two files.",
		PromptTokens:     1200,
		CompletionTokens: 340,
		TotalTokens:      1540,
		CostUSD:          0.0123,
		GeneratedAt:      time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
	})
	for _, want := range []string{
		"<!doctype html>", "Refactor the parser", "split parser.go",
		"1540", "0.0123", "2026",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report HTML missing %q", want)
		}
	}
}

func TestHTMLEscapesContent(t *testing.T) {
	html := HTML(Data{
		Task:   "handle <script>alert(1)</script>",
		Answer: "a & b < c",
	})
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("task content must be HTML-escaped:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag:\n%s", html)
	}
	if !strings.Contains(html, "a &amp; b &lt; c") {
		t.Errorf("answer must be escaped:\n%s", html)
	}
}

func TestHTMLIncludesDiff(t *testing.T) {
	html := HTML(Data{Task: "t", Answer: "a", Diff: "diff --git a/x b/x\n+added"})
	if !strings.Contains(html, "added") {
		t.Errorf("diff section should be rendered:\n%s", html)
	}
}
