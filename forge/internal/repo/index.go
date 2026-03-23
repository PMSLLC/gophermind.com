package repo

import (
	"path/filepath"
	"strings"
)

func LikelyImportantFiles(tree string) []string {
	var out []string
	for _, line := range strings.Split(tree, "\n") {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		base := filepath.Base(clean)
		switch {
		case base == "go.mod":
			out = append(out, clean)
		case strings.HasPrefix(clean, "cmd/"):
			out = append(out, clean)
		case strings.HasPrefix(clean, "internal/"):
			out = append(out, clean)
		case strings.HasSuffix(clean, ".go"):
			out = append(out, clean)
		case strings.EqualFold(base, "README.md"):
			out = append(out, clean)
		}
		if len(out) >= 12 {
			break
		}
	}
	return out
}
