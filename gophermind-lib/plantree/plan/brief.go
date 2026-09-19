package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gophermind/gophermind-lib/plantree"
)

const excerptCutMarker = "\n[excerpt cut to fit the budget]\n"

// ReadBrief returns the brief RunPass1 stored beside the tree. A resumed run
// must be given a brief identical to it.
func ReadBrief(repo *plantree.Repo) (string, error) {
	b, err := os.ReadFile(filepath.Join(repo.Dir(), briefFile))
	return string(b), err
}

// BriefChunks returns the stored brief cut exactly as the run cut it (same
// chunk size), so a chunk index means the same text as when provenance was
// recorded. It wraps os.ErrNotExist when there is no stored brief or no run
// state.
func BriefChunks(repo *plantree.Repo) ([]Chunk, error) {
	brief, err := ReadBrief(repo)
	if err != nil {
		return nil, err
	}
	st, found, err := loadState(repo)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("plan: no pass-1 state in %s: %w", statePath(repo), os.ErrNotExist)
	}
	return SplitBrief(brief, st.ChunkBytes), nil
}

// Excerpts joins the chunks at idx (in the order given) into one block of at
// most budget bytes. Whole chunks are used while they fit; the first one that
// does not is cut at a paragraph or line end and marked. Excerpts are context
// for a model, not requirements, so cutting is allowed here.
func Excerpts(chunks []Chunk, idx []int, budget int) string {
	var b strings.Builder
	for _, i := range idx {
		if i < 0 || i >= len(chunks) {
			continue
		}
		room := budget - b.Len()
		head := fmt.Sprintf("[part %d of %d]\n", i+1, len(chunks))
		room -= len(head)
		if room < 100 {
			break
		}
		text := strings.TrimRight(chunks[i].Text, "\n")
		if len(text)+2 <= room {
			b.WriteString(head)
			b.WriteString(text)
			b.WriteString("\n\n")
			continue
		}
		limit := room - len(excerptCutMarker)
		if limit < 1 {
			break
		}
		cut := cutPoint(text, limit)
		b.WriteString(head)
		b.WriteString(strings.TrimRight(text[:cut], "\n"))
		b.WriteString(excerptCutMarker)
		break
	}
	return strings.TrimRight(b.String(), "\n")
}
