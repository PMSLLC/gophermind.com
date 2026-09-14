package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skills discovers .gophermind/skills/*.md capability packs under root and
// returns them concatenated as tagged sections for injection into the system
// prompt, or "" when none exist. Includes expand within each skill, and the
// caller's token-budget guardrail bounds the total.
func Skills(root string) string {
	dir := filepath.Join(root, ".gophermind", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var parts []string
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(expandIncludes(root, string(b), 0))
		if content == "" {
			continue
		}
		skill := strings.TrimSuffix(name, ".md")
		parts = append(parts, "<skill name=\""+skill+"\">\n"+content+"\n</skill>")
	}
	return strings.Join(parts, "\n\n")
}
