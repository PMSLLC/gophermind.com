package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gophermind/gophermind-lib/lockfile"
	"gophermind/gophermind-lib/plantree"
)

const provenanceFile = "provenance.json"

// provenance records which nodes each chunk of the brief produced or reused, so
// pass 2 can show a task the part of the brief that gave rise to it. It lives
// beside the tree rather than in the nodes, whose schema is fixed.
type provenance struct {
	Chunks map[string][]string `json:"chunks"` // chunk index (decimal) to node ids
}

func provenancePath(repo *plantree.Repo) string {
	return filepath.Join(repo.Dir(), "_state", provenanceFile)
}

func loadProvenance(repo *plantree.Repo) (provenance, error) {
	b, err := os.ReadFile(provenancePath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return provenance{Chunks: map[string][]string{}}, nil
	}
	if err != nil {
		return provenance{}, err
	}
	var p provenance
	if err := json.Unmarshal(b, &p); err != nil {
		return provenance{}, fmt.Errorf("plan: reading %s (delete that file to drop the brief excerpts pass 2 shows): %w", provenancePath(repo), err)
	}
	if p.Chunks == nil {
		p.Chunks = map[string][]string{}
	}
	return p, nil
}

// recordProvenance sets the ids for one chunk, replacing any earlier list, so
// replaying a chunk after a crash leaves the same record.
func recordProvenance(repo *plantree.Repo, chunk int, ids []string) error {
	p, err := loadProvenance(repo)
	if err != nil {
		return err
	}
	if ids == nil {
		ids = []string{}
	}
	p.Chunks[strconv.Itoa(chunk)] = ids
	if err := os.MkdirAll(filepath.Dir(provenancePath(repo)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return lockfile.WriteAtomic(provenancePath(repo), append(b, '\n'), 0o644)
}

// chunksFor returns, in brief order, the indexes of the chunks that produced or
// reused any of ids.
func (p provenance) chunksFor(ids []string) []int {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []int
	for key, list := range p.Chunks {
		n, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		for _, id := range list {
			if want[id] {
				out = append(out, n)
				break
			}
		}
	}
	sort.Ints(out)
	return out
}
