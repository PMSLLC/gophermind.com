// Package skills manages the capability packs injected into an agent's system
// prompt, and where they come from.
//
// The security position, because it drives every choice here: a skill is
// instructions for an agent that runs shell commands and edits files, and
// during an unattended run those tools are auto-approved. A skill fetched from
// a GitHub repository is therefore remote prompt injection with a supply chain
// attached. Three rules follow.
//
// Nothing fetched is enabled by arriving on disk. Enablement is an explicit
// act, recorded per skill, and the zero value of Settings injects nothing.
//
// A source is pinned to a commit, never tracked as a branch. A repository can
// be rewritten after it was reviewed, so a floating ref would give "I read
// these skills" a shelf life of zero. Updating is a decision the caller makes
// against a named new commit.
//
// A repository's own .gophermind/skills is the exception and is always on. It
// was committed to this project deliberately, which is the same consent
// CLAUDE.md already carries.
package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Source is a fetched skill repository, pinned to a commit.
type Source struct {
	URL   string    `json:"url"`
	Ref   string    `json:"ref"`
	SHA   string    `json:"sha"`
	Added time.Time `json:"added"`
}

// Settings is the user's skill configuration: which repositories are
// installed, and which individual skills are switched on.
//
// The zero value is safe and injects nothing fetched.
type Settings struct {
	Sources []Source `json:"sources"`
	// Enabled holds "<source>:<skill>" keys, e.g. "mattpocock/skills:tdd".
	// Scoped rather than bare names so a newly added repository cannot shadow
	// a skill the user already trusted by reusing its name.
	Enabled []string `json:"enabled"`
}

// Skill is one capability pack, as presented to a settings UI.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Source is "owner/repo" for a fetched skill, empty for a repo-local one.
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
	// Key is what Settings.Enabled holds for this skill.
	Key  string `json:"key"`
	body string
}

// unsafeName matches anything that must not appear in a skill name. The name
// is interpolated into a <skill name="..."> tag, so a quote or an angle
// bracket could close the tag and place instructions outside the wrapper that
// marks them as a skill.
var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// LoadSettings reads the skill settings. A missing file is a fresh install and
// yields the zero value, which enables nothing. A file that exists but cannot
// be parsed is an error: guessing at intent here would either enable something
// the user did not ask for or silently disable something they rely on.
func LoadSettings(path string) (Settings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("read skill settings %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Settings{}, nil
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, fmt.Errorf("parse skill settings %s: %w", path, err)
	}
	return s, nil
}

// SaveSettings writes the skill settings, creating the directory if needed.
func SaveSettings(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// ValidateSourceURL accepts only an https URL naming a host and a path, which
// is what "add a GitHub URL" means. A local path, a file:// URL or an ssh
// remote would point a source at this machine, and git's ext:: transport would
// run a command, so none of them are treated as a source.
func ValidateSourceURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("needs a URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("scheme must be https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("URL has no host")
	}
	if strings.Trim(u.Path, "/") == "" {
		return errors.New("URL has no repository path")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return errors.New("URL is not an owner/repo path")
	}
	// The first two segments become the source id, which becomes a directory
	// under the cache. A "." or ".." segment there escapes that directory, and
	// Fetch calls os.RemoveAll and os.Rename on the result. url.Parse does not
	// normalise a path, so "https://host/../.." arrives here intact.
	for _, part := range parts[:2] {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("URL path segment %q is not a valid owner or repo name", part)
		}
	}
	return nil
}

// SourceID reduces a repository URL to the "owner/repo" used as its identity
// in settings, in the cache layout, and in an enablement key.
func SourceID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	p = strings.TrimSuffix(p, ".git")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return p
	}
	return parts[0] + "/" + parts[1]
}

// Key is the settings key for a skill from a source.
func Key(source, name string) string {
	if source == "" {
		return name
	}
	return source + ":" + name
}

// Catalogue lists every skill available to this project: the repository's own
// always-on packs, plus every skill in the fetched cache with its enablement
// state. It never reports an error for an unreadable pack; a pack that cannot
// be read is simply not offered.
func Catalogue(root string, s Settings, cacheDir string) []Skill {
	enabled := make(map[string]bool, len(s.Enabled))
	for _, k := range s.Enabled {
		enabled[k] = true
	}

	var out []Skill
	out = append(out, repoLocal(root)...)
	for i := range out {
		out[i].Enabled = true // committed to this repo, so already consented to
		out[i].Key = out[i].Name
	}

	for _, sk := range fetched(cacheDir) {
		sk.Key = Key(sk.Source, sk.Name)
		sk.Enabled = enabled[sk.Key]
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Inject returns the system-prompt text for every skill that should be active:
// all repo-local packs, plus the fetched skills the user switched on. Returns
// "" when there are none.
func Inject(root string, s Settings, cacheDir string) string {
	var parts []string
	for _, sk := range Catalogue(root, s, cacheDir) {
		if !sk.Enabled || strings.TrimSpace(sk.body) == "" {
			continue
		}
		parts = append(parts, "<skill name=\""+sk.Name+"\">\n"+strings.TrimSpace(sk.body)+"\n</skill>")
	}
	return strings.Join(parts, "\n\n")
}

// repoLocal reads <root>/.gophermind/skills/*.md, the packs committed to this
// project.
func repoLocal(root string) []Skill {
	dir := filepath.Join(root, ".gophermind", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		// A README here documents the directory for whoever reads the repo.
		// It is not a capability pack, and injecting it would spend tokens on
		// every turn explaining the directory to the model.
		if strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		name := safeName(strings.TrimSuffix(e.Name(), ".md"))
		desc, body := splitFrontmatter(string(b))
		out = append(out, Skill{Name: name, Description: desc, body: body})
	}
	return out
}

// fetched walks the cache for <owner>/<repo>@<sha>/<skill>/SKILL.md.
func fetched(cacheDir string) []Skill {
	var out []Skill
	owners, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	for _, owner := range owners {
		if !owner.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(cacheDir, owner.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			repoName, _, _ := strings.Cut(repo.Name(), "@")
			source := owner.Name() + "/" + repoName
			base := filepath.Join(cacheDir, owner.Name(), repo.Name())
			_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
					return nil
				}
				b, readErr := os.ReadFile(p)
				if readErr != nil {
					return nil
				}
				desc, body := splitFrontmatter(string(b))
				name := safeName(frontmatterField(string(b), "name"))
				if name == "" {
					name = safeName(filepath.Base(filepath.Dir(p)))
				}
				out = append(out, Skill{Name: name, Description: desc, Source: source, body: body})
				return nil
			})
		}
	}
	return out
}

// safeName strips anything that could break out of the <skill name="..."> tag
// the body is wrapped in. A name is an identifier, not prose.
func safeName(s string) string {
	return strings.Trim(unsafeName.ReplaceAllString(strings.TrimSpace(s), "-"), "-")
}

var frontmatterRE = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)

// splitFrontmatter returns a skill's description and its body with any YAML
// frontmatter removed. The frontmatter is metadata for a skill loader; left in
// place it would reach the model as literal text inside the skill.
func splitFrontmatter(raw string) (desc, body string) {
	m := frontmatterRE.FindStringSubmatchIndex(raw)
	if m == nil {
		return "", raw
	}
	return frontmatterField(raw, "description"), raw[m[1]:]
}

// frontmatterField pulls one field out of a skill's YAML frontmatter. It
// handles the "|" block form the skills in the wild actually use, without
// taking a YAML dependency for two fields.
func frontmatterField(raw, field string) string {
	m := frontmatterRE.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	var collecting bool
	var lines []string
	for _, line := range strings.Split(m[1], "\n") {
		if strings.HasPrefix(line, field+":") {
			v := strings.TrimSpace(strings.TrimPrefix(line, field+":"))
			if v != "" && v != "|" {
				return strings.Trim(v, `"'`)
			}
			collecting = true
			continue
		}
		if collecting {
			// A block scalar ends at the next unindented key.
			if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}
