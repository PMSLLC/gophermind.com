package plan

import (
	"errors"
	"os"
	"sort"

	"gophermind/gophermind-lib/plantree"
)

// ExcerptsForCapBytes is the default budget of ExcerptsFor, and the most it
// ever returns however large a budget a caller asks for.
const ExcerptsForCapBytes = 4000

// ExcerptsFor returns the part of the brief that produced the given nodes, as
// one block of at most budget bytes (ExcerptsForCapBytes when budget is zero
// or larger than it). It is how a user interface shows "why was this asked":
// the question carries node ids, and this turns them back into brief text.
//
// A node with no provenance of its own (a step a later merge added under a
// recorded task) falls back to its ancestors, so the answer is the task's
// brief text rather than nothing. A node that has provenance never borrows an
// ancestor's, and the plan root is never a fallback: it would hand every
// orphan the same chunk. When there is no stored brief or no pass-1
// state it returns "" and no error: excerpts are context, and a plan built
// without pass 1 simply has none.
func ExcerptsFor(repo *plantree.Repo, nodeIDs []string, budget int) (string, error) {
	if len(nodeIDs) == 0 {
		return "", nil
	}
	if budget < 1 || budget > ExcerptsForCapBytes {
		budget = ExcerptsForCapBytes
	}
	p, err := loadProvenance(repo)
	if err != nil {
		return "", err
	}
	chunks, err := BriefChunks(repo)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	seen := map[int]bool{}
	var idx []int
	for _, id := range nodeIDs {
		own := p.chunksFor([]string{id})
		if len(own) == 0 {
			own = p.chunksFor(ancestorsOf(id))
		}
		for _, c := range own {
			if !seen[c] {
				seen[c] = true
				idx = append(idx, c)
			}
		}
	}
	sort.Ints(idx)
	return Excerpts(chunks, idx, budget), nil
}

// ancestorsOf returns every ancestor of id below the plan root, nearest
// first. An id that does not parse has none.
func ancestorsOf(id string) []string {
	var out []string
	for cur := id; ; {
		parent, err := plantree.ParentID(cur)
		if err != nil || parent == "" || parent == plantree.RootID {
			return out
		}
		out = append(out, parent)
		cur = parent
	}
}
