package patch

import (
	"fmt"
	"strings"
)

func UnifiedPreview(path, before, after string) string {
	if before == after {
		return fmt.Sprintf("No changes for %s", path)
	}
	var b strings.Builder
	b.WriteString("--- ")
	b.WriteString(path)
	b.WriteString("\n+++")
	b.WriteString(" ")
	b.WriteString(path)
	b.WriteString("\n")
	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	max := len(beforeLines)
	if len(afterLines) > max {
		max = len(afterLines)
	}
	for i := 0; i < max; i++ {
		var oldLine, newLine string
		if i < len(beforeLines) {
			oldLine = beforeLines[i]
		}
		if i < len(afterLines) {
			newLine = afterLines[i]
		}
		if oldLine == newLine {
			continue
		}
		if oldLine != "" {
			b.WriteString("-")
			b.WriteString(oldLine)
			b.WriteString("\n")
		}
		if newLine != "" {
			b.WriteString("+")
			b.WriteString(newLine)
			b.WriteString("\n")
		}
	}
	return b.String()
}
