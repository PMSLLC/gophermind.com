package ui

import "strings"

const (
	ansiBold   = "\x1b[1m"
	ansiYellow = "\x1b[33m"
)

// HighlightMarkdown applies lightweight ANSI styling to markdown for terminal
// display: headings bold-cyan, fenced ``` code blocks yellow, and blockquotes
// dimmed. Other text is left as-is. It never styles an empty string.
func HighlightMarkdown(md string) string {
	if md == "" {
		return ""
	}
	lines := strings.Split(md, "\n")
	var b strings.Builder
	inCode := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inCode = !inCode
			b.WriteString(ansiYellow + ln + ansiReset)
		case inCode:
			b.WriteString(ansiYellow + ln + ansiReset)
		case strings.HasPrefix(trimmed, "#"):
			b.WriteString(ansiBold + ansiCyan + ln + ansiReset)
		case strings.HasPrefix(trimmed, ">"):
			b.WriteString(ansiDim + ln + ansiReset)
		default:
			b.WriteString(ln)
		}
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
