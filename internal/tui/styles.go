package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The TUI's visual theme. Colors are lipgloss.AdaptiveColor so each renders a
// tuned shade in light and dark terminals; the palette is intentionally small
// and consistent so the transcript reads as one system rather than a pile of
// ad-hoc colors.
var (
	// userPromptStyle styles the echo of what the user typed (the "› …" line).
	userPromptStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C4B5FD"})

	// Tool invocation: a bright bullet, a bold name, and dimmed argument JSON.
	toolBulletStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#22D3EE"})
	toolNameStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"})
	toolArgsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#94A3B8"})

	// Tool output: muted text behind a colored gutter so results are visually
	// subordinate to the call that produced them.
	resultGutterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#94A3B8", Dark: "#475569"})
	resultTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#475569", Dark: "#94A3B8"})

	// Intermediate narration ("planning…" prose the model emits alongside a tool
	// call) is de-emphasized relative to the final answer.
	narrationStyle = lipgloss.NewStyle().Italic(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#475569", Dark: "#9CA3AF"})

	// errorStyle marks failures and the cancellation notice.
	errorStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"})

	// Status-bar variants. The approval prompt is deliberately loud (inverse
	// video) so a paused, input-awaiting turn never reads as a frozen one.
	statusReadyStyle   = lipgloss.NewStyle().Faint(true)
	statusWorkingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"})
	statusApprovalStyle = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#111827"}).
				Background(lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FBBF24"}).
				Padding(0, 1)
)

// resultMaxLines caps how many lines of a tool result are shown inline so a
// large file read or long command output cannot flood the transcript; the
// remainder is summarized as a count.
const resultMaxLines = 12

// renderUserPrompt styles the echoed user input line.
func renderUserPrompt(text string) string {
	return userPromptStyle.Render("› " + text)
}

// renderToolCall styles a tool invocation: bullet, name, and (unless empty)
// its arguments collapsed to a single dimmed line.
func renderToolCall(name, args string) string {
	line := toolBulletStyle.Render("●") + " " + toolNameStyle.Render(name)
	if a := oneLine(args); a != "" && a != "{}" {
		line += "  " + toolArgsStyle.Render(a)
	}
	return line
}

// renderToolResult styles a tool's output as muted, multi-line text behind a
// gutter, preserving line structure (unlike a single truncated line) and
// capping the height at resultMaxLines.
func renderToolResult(text string) string {
	gutter := resultGutterStyle.Render("  │ ")
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return gutter + resultTextStyle.Render("(no output)")
	}
	lines := strings.Split(text, "\n")
	extra := 0
	if len(lines) > resultMaxLines {
		extra = len(lines) - resultMaxLines
		lines = lines[:resultMaxLines]
	}
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(gutter + resultTextStyle.Render(ln))
	}
	if extra > 0 {
		b.WriteByte('\n')
		b.WriteString(gutter + resultTextStyle.Italic(true).Render(fmt.Sprintf("… %d more line%s", extra, plural(extra))))
	}
	return b.String()
}

// renderNarration styles intermediate model prose emitted alongside a tool call.
func renderNarration(text string) string {
	return narrationStyle.Render(strings.TrimRight(text, "\n"))
}

// renderError styles a failure line.
func renderError(text string) string {
	return errorStyle.Render(text)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
