package serve

import (
	"os"
	"path/filepath"
	"sort"

	"gophermind/gophermind-lib/runview"
)

// runRoot is the repo root that holds the run folder of id: the run's own repo
// under reposDir when it has one, else root.
func runRoot(reposDir, root, id string) string {
	if reposDir == "" || !runview.ValidRunID(id) {
		return root
	}
	own := filepath.Join(reposDir, id)
	if fi, err := os.Lstat(filepath.Join(own, ".gophermind", id)); err == nil && fi.IsDir() {
		return own
	}
	return root
}

// repoFor is where run id builds: its own repo when the server makes one per
// run, else the shared root.
func (m *briefManager) repoFor(id string) string {
	if m.repos != "" {
		return filepath.Join(m.repos, id)
	}
	return m.root
}

// listRuns lists the runs under root and under every run repo in reposDir,
// most recently updated first.
func listRuns(reposDir, root string) ([]runview.RunSummary, error) {
	runs, err := runview.Runs(root)
	if err != nil || reposDir == "" {
		return runs, err
	}
	ents, _ := os.ReadDir(reposDir)
	for _, e := range ents {
		if !e.IsDir() || !runview.ValidRunID(e.Name()) {
			continue
		}
		if more, err := runview.Runs(filepath.Join(reposDir, e.Name())); err == nil {
			runs = append(runs, more...)
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].UpdatedAt != runs[j].UpdatedAt {
			return runs[i].UpdatedAt > runs[j].UpdatedAt
		}
		return runs[i].RunID > runs[j].RunID
	})
	return runs, nil
}

// newestRun is the id of the most recently updated run, or "".
func newestRun(reposDir, root string) string {
	if reposDir == "" {
		return runview.NewestRunID(root)
	}
	runs, _ := listRuns(reposDir, root)
	if len(runs) == 0 {
		return ""
	}
	return runs[0].RunID
}
