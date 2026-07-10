package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gophermind/internal/embed"
)

// semanticSnippetMax caps how many characters of a matched chunk are shown.
const semanticSnippetMax = 300

// EmbedIndex returns a gated tool that builds a semantic index over the repo's
// files (chunked + embedded) and saves it to indexPath, so semantic_search can
// find relevant code without exhaustive grep. A nil provider means embeddings
// are unconfigured.
func EmbedIndex(root string, p embed.Provider, indexPath string) Tool {
	return Tool{
		Name:        "embed_index",
		Description: "Build a semantic (embeddings) index over the repository's files and save it, so semantic_search can retrieve relevant code by meaning. Specify file extensions to include.",
		Schema: object(map[string]any{
			"exts": map[string]any{"type": "array", "description": "File extensions to index (e.g. [\".go\",\".md\"]); empty means all files.", "items": map[string]any{"type": "string"}},
		}),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_URL and GOPHERMIND_EMBED_MODEL")
			}
			var a struct {
				Exts []string `json:"exts"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			idx, err := embed.BuildIndex(ctx, p, root, a.Exts)
			if err != nil {
				return "", fmt.Errorf("build index: %w", err)
			}
			if err := idx.Save(indexPath); err != nil {
				return "", fmt.Errorf("save index: %w", err)
			}
			return fmt.Sprintf("Built semantic index: %d chunks, saved to %s", len(idx.Vectors), indexPath), nil
		},
	}
}

// SemanticSearch returns a read-only tool that embeds a query and returns the
// most relevant indexed chunks (by cosine similarity). Requires a prior
// embed_index run.
func SemanticSearch(root string, p embed.Provider, indexPath string) Tool {
	return Tool{
		Name:        "semantic_search",
		Description: "Search the semantic index (built by embed_index) for chunks most relevant to a query, returning file references and snippets. Read-only.",
		Schema: object(map[string]any{
			"query": str("The natural-language query to search for."),
			"k":     map[string]any{"type": "integer", "description": "How many results to return (default 5)."},
		}, "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_URL and GOPHERMIND_EMBED_MODEL")
			}
			var a struct {
				Query string `json:"query"`
				K     int    `json:"k"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.Query) == "" {
				return "", fmt.Errorf("query is empty")
			}
			k := a.K
			if k <= 0 {
				k = 5
			}
			idx, err := embed.LoadIndex(indexPath)
			if err != nil {
				return "", fmt.Errorf("no index found (run embed_index first): %w", err)
			}
			qv, err := p.Embed(ctx, []string{a.Query})
			if err != nil || len(qv) == 0 {
				return "", fmt.Errorf("embed query: %w", err)
			}
			hits := embed.TopK(qv[0], idx.Vectors, k)
			var b strings.Builder
			for _, h := range hits {
				snippet := h.Text
				if len(snippet) > semanticSnippetMax {
					snippet = snippet[:semanticSnippetMax] + "…"
				}
				fmt.Fprintf(&b, "── %s (score %.3f)\n%s\n\n", h.ID, h.Score, snippet)
			}
			if b.Len() == 0 {
				b.WriteString("(no results)\n")
			}
			return b.String(), nil
		},
	}
}
