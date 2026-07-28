package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// chunkLines is the default number of lines per indexed chunk.
const chunkLines = 50

// maxIndexFiles caps how many files BuildIndex will read, to bound cost.
const maxIndexFiles = 2000

// maxChunkChars caps a chunk's length in bytes. Line count alone does not bound
// size: 50 lines of dense prose can exceed an embedding model's context window,
// and a server that rejects one oversized input fails the whole batched request.
// Embedding models are commonly 2048-token; at the ~3 chars/token that code and
// English tokenize to, this leaves a wide margin.
const maxChunkChars = 4000

// embedBatchSize caps how many chunks go out per embedding request. The whole
// index used to travel in one request, which meant a large repo could not finish
// inside the provider's request timeout.
const embedBatchSize = 128

// embedAll embeds texts in bounded batches, preserving input order.
func embedAll(ctx context.Context, p Provider, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := p.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if len(vecs) != end-start {
			return nil, fmt.Errorf("provider returned %d embeddings for %d chunks", len(vecs), end-start)
		}
		out = append(out, vecs...)
	}
	return out, nil
}

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

	vecs, err := embedAll(ctx, p, texts)
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

// chunkText splits text into chunks of at most n lines and at most
// maxChunkChars bytes, so no chunk can exceed the embedding model's context.
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
		chunks = append(chunks, splitOversized(strings.Join(lines[i:end], "\n"))...)
	}
	return chunks
}

// splitOversized breaks a chunk longer than maxChunkChars into pieces that fit,
// preferring line boundaries so chunks stay readable as retrieved context.
func splitOversized(chunk string) []string {
	if len(chunk) <= maxChunkChars {
		return []string{chunk}
	}
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, line := range strings.Split(chunk, "\n") {
		// A single line over the cap has no usable boundary — emit whatever has
		// accumulated, then hard-split the line itself.
		if len(line) > maxChunkChars {
			flush()
			out = append(out, splitRunes(line)...)
			continue
		}
		if cur.Len() > 0 && cur.Len()+1+len(line) > maxChunkChars {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	flush()
	return out
}

// splitRunes hard-splits s into maxChunkChars-bounded pieces without cutting a
// multi-byte rune in half (which would emit invalid UTF-8 into the request).
func splitRunes(s string) []string {
	var out []string
	for len(s) > maxChunkChars {
		cut := maxChunkChars
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = maxChunkChars // no rune boundary found; cut anyway rather than loop forever
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		out = append(out, s)
	}
	return out
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
