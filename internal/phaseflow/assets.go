package phaseflow

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// assetFS holds the PhaseFlow prompt artifacts vendored from upstream
// (github.com/jbrahy/metaphaseflow, MIT, Copyright 2025 Lex Christopherson —
// see assets/LICENSE.upstream). They are the markdown that drives each step of
// the loop: slash-command prompts, subagent definitions, and templates. The
// engine loads them by name to seed gophermind's agent, so the workflow runs
// without an external npx install.
//
//go:embed assets/commands/*.md assets/agents/*.md assets/templates/*.md assets/templates/config.json assets/LICENSE.upstream
var assetFS embed.FS

// Asset is one embedded prompt artifact with its parsed frontmatter.
type Asset struct {
	Name        string            // basename without extension, e.g. "execute-phase"
	Kind        string            // "command", "agent", or "template"
	Description string            // from frontmatter "description", if present
	Frontmatter map[string]string // scalar frontmatter fields
	Body        string            // markdown after the frontmatter block
}

// Command returns the embedded phase command with the given name (e.g.
// "execute-phase" or "phase:execute-phase" — the "phase:" prefix is optional).
func Command(name string) (Asset, bool) { return lookup("commands", "command", name) }

// AgentPrompt returns the embedded phase agent definition with the given name
// (e.g. "phase-executor").
func AgentPrompt(name string) (Asset, bool) { return lookup("agents", "agent", name) }

// Template returns the embedded workflow template with the given name (e.g.
// "roadmap", "state", "project").
func Template(name string) (Asset, bool) { return lookup("templates", "template", name) }

// CommandNames returns the sorted names of all embedded phase commands.
func CommandNames() []string { return names("commands") }

// AgentNames returns the sorted names of all embedded phase agents.
func AgentNames() []string { return names("agents") }

func lookup(dir, kind, name string) (Asset, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "phase:")
	name = strings.TrimPrefix(name, "phase-")
	// Agents are stored with a "phase-" prefix; restore it for the file lookup.
	base := name
	if kind == "agent" && !strings.HasPrefix(name, "phase-") {
		base = "phase-" + name
	}
	data, err := assetFS.ReadFile("assets/" + dir + "/" + base + ".md")
	if err != nil {
		return Asset{}, false
	}
	return parseAsset(base, kind, string(data)), true
}

func names(dir string) []string {
	entries, err := fs.ReadDir(assetFS, "assets/"+dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			out = append(out, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	sort.Strings(out)
	return out
}

// parseAsset splits an upstream command/agent markdown file into its "---"
// frontmatter and the body. This is a deliberately tiny frontmatter reader, not
// a YAML parser: it keeps only scalar "key: value" pairs (all the engine needs
// is name and description) and ignores multi-line block and list values such as
// the commands' allowed-tools arrays. Dropping those is intentional — they list
// Claude Code tool names, which gophermind neither needs nor honors, and only
// Body (the instructions after the frontmatter) is ever sent to the agent.
func parseAsset(name, kind, content string) Asset {
	a := Asset{Name: name, Kind: kind, Frontmatter: map[string]string{}, Body: content}
	if !strings.HasPrefix(content, "---") {
		return a
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return a
	}
	fm := rest[:end]
	body := rest[end+len("\n---"):]
	a.Body = strings.TrimLeft(body, "\r\n")

	for _, line := range strings.Split(fm, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Skip list/block openers (empty value) and indented continuation lines.
		if val == "" || key == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		a.Frontmatter[key] = val
	}
	a.Description = a.Frontmatter["description"]
	return a
}
