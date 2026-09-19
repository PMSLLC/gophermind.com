package plan

import (
	"errors"
	"os"

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
// brief text rather than nothing. When there is no stored brief or no pass-1
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
	return Excerpts(chunks, p.chunksFor(withAncestors(nodeIDs)), budget), nil
}

// withAncestors returns ids followed by every ancestor of each, once, so a
// lookup finds the chunk that produced a node's task or phase when the node
// itself was never recorded. An id that does not parse contributes only
// itself.
func withAncestors(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		for cur := id; cur != ""; {
			if !seen[cur] {
				seen[cur] = true
				out = append(out, cur)
			}
			parent, err := plantree.ParentID(cur)
			if err != nil {
				break
			}
			cur = parent
		}
	}
	return out
}
