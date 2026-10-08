package projectrun

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
)

// StatePath is one thing a clean run must clear (or keep). Action is one of
// delete_dir, git_branch_delete, git_reset, delete_file, keep, external.
type StatePath struct{ Action, Scope, Path string }

// stateRunIDRE is the run id shape planner.LookupRun accepts.
var stateRunIDRE = regexp.MustCompile(`^gm-[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{3}$`)

const defaultModCache = "~/.gophermind/gomodcache"

// StatePaths lists, in the order of spec section 9, everything a clean attempt
// must clear. It deletes nothing, creates nothing, runs no git, reads no vault
// and dials nothing; the settings file is read only when it already exists.
func StatePaths(o Options, b *brief.Brief, env Env) ([]StatePath, error) {
	id := b.Front.ID
	if !stateRunIDRE.MatchString(id) {
		return nil, fmt.Errorf("projectrun: %q is not a run id (want gm-YYYY-MM-DD-NNN)", id)
	}
	repo := o.Repo
	if repo == "" {
		repo = b.Front.Repo
	}
	if repo == "" {
		return nil, fmt.Errorf("projectrun: no repo: the brief names none and --repo is not set")
	}
	repo = filepath.Clean(repo)
	cfgDir, err := env.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("projectrun: config dir: %w", err)
	}
	vaultPath, err := env.VaultPath()
	if err != nil {
		return nil, fmt.Errorf("projectrun: vault path: %w", err)
	}
	settingsPath, err := env.SettingsPath()
	if err != nil {
		return nil, fmt.Errorf("projectrun: settings path: %w", err)
	}
	cache := defaultModCache
	if _, serr := os.Stat(settingsPath); serr == nil {
		cfg, lerr := env.LoadSettings(settingsPath)
		if lerr != nil {
			return nil, fmt.Errorf("projectrun: settings: %w", lerr)
		}
		if cfg.Executor.GoModCache != "" {
			cache = cfg.Executor.GoModCache
		}
	}
	if cache == "~" || strings.HasPrefix(cache, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return nil, fmt.Errorf("projectrun: home dir: %w", herr)
		}
		cache = filepath.Join(home, strings.TrimPrefix(cache, "~"))
	}

	g := filepath.Join(repo, ".gophermind")
	names := make([]string, 0, len(b.Front.Secrets))
	for _, s := range b.Front.Secrets {
		names = append(names, s.Name)
	}
	return []StatePath{
		{"delete_dir", "per-project-repo", filepath.Join(g, id)},
		{"delete_dir", "per-project-repo", filepath.Join(g, id+"-scratch")},
		{"keep", "per-project-repo", filepath.Join(repo, ".git", "info", "exclude")},
		{"git_branch_delete", "per-project-repo-git", "gm/" + id},
		{"git_reset", "per-project-repo-git", repo},
		{"delete_file", "per-project-global", filepath.Join(cfgDir, "runs", id+".json")},
		{"keep", "global", vaultPath},
		{"keep", "global", settingsPath},
		{"keep", "global", cache},
		{"external", "external", "declared secrets: " + strings.Join(names, ", ") + "; drop and recreate the databases behind the postgres URLs"},
	}, nil
}

// PrintStatePaths writes "<action>\t<scope>\t<path>" per line.
func PrintStatePaths(w io.Writer, ps []StatePath) {
	for _, p := range ps {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.Action, p.Scope, p.Path)
	}
}
