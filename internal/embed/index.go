package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// chunkLines is the default number of lines per indexed chunk.
const chunkLines = 50

// maxIndexFiles caps how many files BuildIndex will read, to bound cost.
const maxIndexFiles = 2000

// Index is a persisted set of embedding vectors over a repo's files.
type Index struct {
	Model   string   `json:"model,omitempty"`
	Vectors []Vector `json:"vectors"`
}

// BuildIndex walks root, chunks files whose extension is in exts (all files when
// exts is empty), embeds each chunk via p, and returns the index. Chunk ids are
// "path#chunkN".
func BuildIndex(ctx context.Context, p Provider, root string, exts []string) (*Index, error) {
	extSet := map[string]bool{}
	for _, e := range exts {
		extSet[strings.ToLower(e)] = true
	}

	var ids, texts []string
	files := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(extSet) > 0 && !extSet[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if files >= maxIndexFiles {
			return filepath.SkipAll
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		files++
		rel, _ := filepath.Rel(root, path)
		for i, chunk := range chunkText(string(data), chunkLines) {
			if strings.TrimSpace(chunk) == "" {
				continue
			}
			ids = append(ids, fmt.Sprintf("%s#%d", rel, i))
			texts = append(texts, chunk)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return &Index{}, nil
	}

	vecs, err := p.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("provider returned %d embeddings for %d chunks", len(vecs), len(texts))
	}
	idx := &Index{Vectors: make([]Vector, 0, len(texts))}
	for i := range texts {
		idx.Vectors = append(idx.Vectors, Vector{ID: ids[i], Text: texts[i], Values: vecs[i]})
	}
	return idx, nil
}

// chunkText splits text into chunks of at most n lines.
func chunkText(text string, n int) []string {
	if n <= 0 {
		n = chunkLines
	}
	lines := strings.Split(text, "\n")
	var chunks []string
	for i := 0; i < len(lines); i += n {
		end := i + n
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, strings.Join(lines[i:end], "\n"))
	}
	return chunks
}

// Save writes the index to path as JSON.
func (idx *Index) Save(path string) error {
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadIndex reads an index from path.
func LoadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return &idx, nil
}
