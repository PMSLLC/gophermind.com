package main

// This file covers 05-07's "UI edge cases handled (empty states, long
// text, rapid interactions)" for the pieces of chatview.go's layout math
// that are plain Go (no cgo, no libui-ng widgets) and therefore fully
// unit-testable without runOnUIThread -- unlike the rest of this package's
// tests, which can only prove "doesn't panic," these can prove the actual
// numbers are right.

import (
	"strings"
	"testing"

	appui "gophermind/gophermind-osx/ui"
)

func TestWrappedLineCount_EmptyStringIsOneLine(t *testing.T) {
	if got := wrappedLineCount("", 80); got != 1 {
		t.Errorf("wrappedLineCount(\"\", 80) = %d, want 1", got)
	}
}

func TestWrappedLineCount_ShortLineIsOneLine(t *testing.T) {
	if got := wrappedLineCount("hello", 80); got != 1 {
		t.Errorf("wrappedLineCount(%q, 80) = %d, want 1", "hello", got)
	}
}

func TestWrappedLineCount_ExactMultipleOfColsIsExact(t *testing.T) {
	s := strings.Repeat("x", 160) // exactly 2*80
	if got := wrappedLineCount(s, 80); got != 2 {
		t.Errorf("wrappedLineCount(160 chars, 80) = %d, want 2", got)
	}
}

func TestWrappedLineCount_OneOverAMultipleRoundsUp(t *testing.T) {
	s := strings.Repeat("x", 161) // 2*80 + 1
	if got := wrappedLineCount(s, 80); got != 3 {
		t.Errorf("wrappedLineCount(161 chars, 80) = %d, want 3", got)
	}
}

// TestWrappedLineCount_VeryLongSingleLine covers the "long text" edge case
// directly: a single unbroken line (no newlines a user could have typed,
// e.g. a very long URL or base64 blob) must still produce a finite,
// correctly rounded line count rather than degrading in some other way
// (an infinite loop, an off-by-one that compounds, etc.) as the input
// grows to sizes well past anything in this package's other tests.
func TestWrappedLineCount_VeryLongSingleLine(t *testing.T) {
	s := strings.Repeat("x", 100_000)
	want := (100_000 + 79) / 80
	if got := wrappedLineCount(s, 80); got != want {
		t.Errorf("wrappedLineCount(100000 chars, 80) = %d, want %d", got, want)
	}
}

// TestMessageLineCount_EmptyMessageStillCountsPrefixAndSpacer covers the
// "empty state" edge case: a Message with no text at all (the zero value,
// or an assistant message that streamed zero tokens before "done") must
// still produce a sane, non-zero line count -- the role-prefix line plus
// the spacer -- not zero or a negative number that would confuse
// recomputeSize's content-height math.
func TestMessageLineCount_EmptyMessageStillCountsPrefixAndSpacer(t *testing.T) {
	m := appui.Message{Role: appui.RoleAssistant}
	got := messageLineCount(m)
	want := 1 /* role prefix */ + 1 /* wrappedLineCount("") */ + 1 /* spacer */
	if got != want {
		t.Errorf("messageLineCount(empty assistant message) = %d, want %d", got, want)
	}
}

// TestMessageLineCount_VeryLongTextWithManyNewlines covers "long text"
// combined with the multi-line accumulation path recomputeSize's caller
// relies on for a real streamed answer.
func TestMessageLineCount_VeryLongTextWithManyNewlines(t *testing.T) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = strings.Repeat("y", 40) // under 80 cols: exactly 1 wrapped line each
	}
	m := appui.Message{Role: appui.RoleAssistant, Text: strings.Join(lines, "\n")}
	got := messageLineCount(m)
	want := 1 /* role prefix */ + 500 /* one wrapped line per source line */ + 1 /* spacer */
	if got != want {
		t.Errorf("messageLineCount(500 lines) = %d, want %d", got, want)
	}
}

// TestMessageLineCount_ToolCallUsesToolArgsNotText covers the branch that
// picks ToolArgs over Text for a RoleToolCall message -- an empty Text
// alongside a real ToolArgs must not be mistaken for an empty message.
func TestMessageLineCount_ToolCallUsesToolArgsNotText(t *testing.T) {
	m := appui.Message{Role: appui.RoleToolCall, ToolArgs: "{\n  \"a\": 1\n}"}
	got := messageLineCount(m)
	// 3 lines of ToolArgs ("{", `  "a": 1`, "}") + role prefix + spacer.
	want := 1 + 3 + 1
	if got != want {
		t.Errorf("messageLineCount(tool call) = %d, want %d", got, want)
	}
}
