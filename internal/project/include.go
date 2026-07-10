package project

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gophermind/internal/safety"
)

// includeRe matches a {{include: path}} directive so reusable prompt fragments
// can be composed into instruction files.
var includeRe = regexp.MustCompile(`\{\{\s*include:\s*([^}]+?)\s*\}\}`)

// maxIncludeDepth bounds recursive includes so a cycle can't loop forever.
const maxIncludeDepth = 5

// expandIncludes replaces {{include: path}} directives with the referenced
// file's contents (contained to root via SafeJoin), recursively up to
// maxIncludeDepth. A missing or out-of-root target is replaced with a short
// marker rather than failing the whole prompt.
func expandIncludes(root, content string, depth int) string {
	if depth >= maxIncludeDepth || !strings.Contains(content, "{{") {
		return content
	}
	return includeRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := includeRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		rel := strings.TrimSpace(sub[1])
		full, err := safety.SafeJoin(root, rel)
		if err != nil {
			return fmt.Sprintf("[include blocked: %s]", rel)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fmt.Sprintf("[include missing: %s]", rel)
		}
		return expandIncludes(root, strings.TrimRight(string(data), "\n"), depth+1)
	})
}
