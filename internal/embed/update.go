package embed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// fileOfID returns the file path portion of a chunk id ("path#N" -> "path").
func fileOfID(id string) string {
	if i := strings.LastIndex(id, "#"); i >= 0 {
		return id[:i]
	}
	return id
}

// UpdateIndex incrementally refreshes existing: vectors for changed files are
// dropped and those files re-chunked+embedded (a changed file that no longer
// exists is simply dropped), while unchanged files' vectors are kept. This makes
// re-indexing large repos fast when only a few files changed.
func UpdateIndex(ctx context.Context, p Provider, root string, exts []string, existing *Index, changed []string) (*Index, error) {
	changedSet := map[string]bool{}
	for _, c := range changed {
		changedSet[filepath.ToSlash(c)] = true
	}
	extSet := map[string]bool{}
	for _, e := range exts {
		extSet[strings.ToLower(e)] = true
	}

	out := &Index{Model: existing.Model}
	// Keep vectors whose file did not change.
	for _, v := range existing.Vectors {
		if !changedSet[filepath.ToSlash(fileOfID(v.ID))] {
			out.Vectors = append(out.Vectors, v)
		}
	}

	// Re-embed each changed file that still exists and matches the ext filter.
	var ids, texts []string
	for c := range changedSet {
		if len(extSet) > 0 && !extSet[strings.ToLower(filepath.Ext(c))] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, c))
		if err != nil {
			continue // deleted/unreadable: its vectors were already dropped
		}
		for i, chunk := range chunkText(string(data), chunkLines) {
			if strings.TrimSpace(chunk) == "" {
				continue
			}
			ids = append(ids, c+"#"+itoa(i))
			texts = append(texts, chunk)
		}
	}
	if len(texts) > 0 {
		vecs, err := p.Embed(ctx, texts)
		if err != nil {
			return nil, err
		}
		for i := range texts {
			if i < len(vecs) {
				out.Vectors = append(out.Vectors, Vector{ID: ids[i], Text: texts[i], Values: vecs[i]})
			}
		}
	}
	return out, nil
}

// itoa is a tiny int-to-string to avoid importing strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
