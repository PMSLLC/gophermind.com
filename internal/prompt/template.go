// Package prompt provides a structured, composable system-prompt system: YAML-ish
// frontmatter plus XML-style sections parsed from .md templates, and a builder
// that assembles them (with context injection) into a final system prompt.
package prompt

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Template is a parsed prompt template: frontmatter metadata plus named sections.
type Template struct {
	Name        string
	Description string
	Tools       []string
	Color       string
	Sections    map[string]string // section name -> trimmed content
}

// sectionOrder is the canonical order sections are emitted in. Any section not
// listed here is appended afterward in sorted order.
var sectionOrder = []string{"role", "workflow", "tools", "safety", "rules", "project_context", "skills", "verification"}

var (
	openTagRe = regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_]*)>`)
	ctxVarRe  = regexp.MustCompile(`\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
)

// Load reads and parses a template file.
func Load(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	return ParseTemplate(string(data))
}

// ParseTemplate parses frontmatter (--- delimited key: value lines) and XML-like
// sections from src. Frontmatter is optional; an opened but unclosed frontmatter
// block or an unclosed section tag is an error.
func ParseTemplate(src string) (*Template, error) {
	t := &Template{Sections: map[string]string{}}
	body := src

	if strings.HasPrefix(src, "---\n") || src == "---" {
		rest := strings.TrimPrefix(src, "---\n")
		end := strings.Index(rest, "\n---")
		if end < 0 {
			return nil, fmt.Errorf("frontmatter opened with --- but never closed")
		}
		fm := rest[:end]
		body = strings.TrimPrefix(rest[end+len("\n---"):], "\n")
		if err := parseFrontmatter(fm, t); err != nil {
			return nil, err
		}
	}

	if err := parseSections(body, t); err != nil {
		return nil, err
	}
	return t, nil
}

func parseFrontmatter(fm string, t *Template) error {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("malformed frontmatter line: %q", line)
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch key {
		case "name":
			t.Name = val
		case "description":
			t.Description = val
		case "color":
			t.Color = val
		case "tools":
			for _, tool := range strings.Split(val, ",") {
				if tool = strings.TrimSpace(tool); tool != "" {
					t.Tools = append(t.Tools, tool)
				}
			}
		}
	}
	return nil
}

func parseSections(body string, t *Template) error {
	for {
		m := openTagRe.FindStringSubmatchIndex(body)
		if m == nil {
			break
		}
		name := body[m[2]:m[3]]
		close := "</" + name + ">"
		rest := body[m[1]:]
		ci := strings.Index(rest, close)
		if ci < 0 {
			return fmt.Errorf("section <%s> is not closed", name)
		}
		t.Sections[name] = strings.TrimSpace(rest[:ci])
		body = rest[ci+len(close):]
	}
	return nil
}

// Build assembles a template's sections into a final prompt string, in canonical
// order (extras appended sorted), each wrapped in its tags, with {{.Var}}
// placeholders replaced from context (unknown vars left as-is).
func Build(t *Template, context map[string]string) string {
	seen := map[string]bool{}
	var order []string
	for _, name := range sectionOrder {
		if _, ok := t.Sections[name]; ok {
			order = append(order, name)
			seen[name] = true
		}
	}
	var extras []string
	for name := range t.Sections {
		if !seen[name] {
			extras = append(extras, name)
		}
	}
	sort.Strings(extras)
	order = append(order, extras...)

	var b strings.Builder
	first := true
	for _, name := range order {
		content := strings.TrimSpace(injectContext(t.Sections[name], context))
		if content == "" {
			continue // drop sections that resolve to nothing (e.g. empty context)
		}
		if !first {
			b.WriteString("\n\n")
		}
		first = false
		fmt.Fprintf(&b, "<%s>\n%s\n</%s>", name, content, name)
	}
	return b.String()
}

// injectContext replaces {{.Key}} placeholders with values from context. An
// unset key renders as empty, so optional sections (e.g. project_context with no
// CLAUDE.md) collapse and are dropped by Build.
func injectContext(s string, context map[string]string) string {
	return ctxVarRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := ctxVarRe.FindStringSubmatch(m)
		return context[sub[1]] // "" when unset
	})
}
