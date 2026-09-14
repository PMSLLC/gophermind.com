package phaseflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The agent catalog is the set of agent types `/project` assigns tasks to. Each
// is a <name>.prompt.md file: frontmatter (name, description, default_model,
// keywords) plus the prompt body. The catalog is seeded from the embedded
// PhaseFlow agents and is user-editable, so teams can tune roles, models, and
// prompts per project.

// Model tiers used as an agent's default_model. They map to gophermind's
// existing speed/strong model configuration; a concrete model name is also
// allowed in a catalog file.
const (
	ModelSpeed  = "speed"
	ModelStrong = "strong"
)

// CatalogAgent is one agent type from the catalog.
type CatalogAgent struct {
	Name         string
	Description  string
	DefaultModel string
	Keywords     []string
	Body         string
}

// CatalogDir returns the agent-catalog directory, .planning/agents.
func CatalogDir(root string) string {
	return filepath.Join(PlanningDir(root), "agents")
}

// LoadCatalog reads every <name>.prompt.md in the catalog dir. A missing dir
// yields an empty catalog (found=false), not an error.
func LoadCatalog(root string) (agents []CatalogAgent, found bool, err error) {
	dir := CatalogDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".prompt.md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, false, err
		}
		agents = append(agents, parseCatalogAgent(strings.TrimSuffix(e.Name(), ".prompt.md"), string(data)))
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, len(agents) > 0, nil
}

// parseCatalogAgent parses a catalog prompt.md into a CatalogAgent, falling back
// to the file's base name and a strong-model default when frontmatter is absent.
func parseCatalogAgent(base, content string) CatalogAgent {
	a := parseAsset(base, "catalog", content)
	ca := CatalogAgent{
		Name:         firstNonEmptyStr(a.Frontmatter["name"], base),
		Description:  a.Description,
		DefaultModel: firstNonEmptyStr(a.Frontmatter["default_model"], ModelStrong),
		Body:         a.Body,
	}
	if kw := strings.TrimSpace(a.Frontmatter["keywords"]); kw != "" {
		for _, k := range strings.Split(kw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				ca.Keywords = append(ca.Keywords, k)
			}
		}
	}
	return ca
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// SeedCatalog writes the embedded PhaseFlow agents into the catalog dir as
// <short>.prompt.md, each tagged with a default model tier. It never overwrites
// an existing file, so user edits survive re-seeding. It returns how many files
// it created.
func SeedCatalog(root string) (int, error) {
	dir := CatalogDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	created := 0
	for _, full := range AgentNames() {
		short := strings.TrimPrefix(full, "phase-")
		path := filepath.Join(dir, short+".prompt.md")
		if _, err := os.Stat(path); err == nil {
			continue // keep user edits
		}
		asset, ok := AgentPrompt(full)
		if !ok {
			continue
		}
		content := renderCatalogFile(short, asset.Description, defaultModelFor(short, asset.Description), asset.Body)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// renderCatalogFile builds a catalog prompt.md: normalized frontmatter plus the
// upstream agent body.
func renderCatalogFile(name, desc, model, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\ndefault_model: %s\n---\n\n%s",
		name, desc, model, body)
}

// speedRoles are catalog roles cheap enough to default to the speed tier; every
// other role defaults to strong. Kept small and explicit — the user can retune
// any file's default_model.
var speedRoles = map[string]bool{
	"doc-classifier": true,
	"doc-verifier":   true,
	"statusline":     true,
	"intel-updater":  true,
}

// defaultModelFor picks an initial model tier for a seeded agent: speed for the
// small, mechanical roles, strong otherwise.
func defaultModelFor(name, _ string) string {
	if speedRoles[name] {
		return ModelSpeed
	}
	return ModelStrong
}
