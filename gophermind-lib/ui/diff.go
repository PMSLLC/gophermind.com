package ui

import "strings"

// ANSI color codes for diff rendering.
const (
	ansiReset = "\x1b[0m"
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiCyan  = "\x1b[36m"
	ansiDim   = "\x1b[2m"
)

// ColorizeDiff renders a unified diff with ANSI colors: added lines green,
// removed lines red, hunk headers cyan, and file/index headers dimmed. Context
// lines are left unstyled. It never colors an empty string.
func ColorizeDiff(diff string) string {
	if diff == "" {
		return ""
	}
	lines := strings.Split(diff, "\n")
	var b strings.Builder
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
			b.WriteString(ansiDim + ln + ansiReset)
		case strings.HasPrefix(ln, "@@"):
			b.WriteString(ansiCyan + ln + ansiReset)
		case strings.HasPrefix(ln, "diff ") || strings.HasPrefix(ln, "index "):
			b.WriteString(ansiDim + ln + ansiReset)
		case strings.HasPrefix(ln, "+"):
			b.WriteString(ansiGreen + ln + ansiReset)
		case strings.HasPrefix(ln, "-"):
			b.WriteString(ansiRed + ln + ansiReset)
		default:
			b.WriteString(ln)
		}
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
