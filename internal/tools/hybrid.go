package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gophermind/internal/embed"

	_ "modernc.org/sqlite"
)

// HybridSearch returns a read-only tool combining keyword (SQLite FTS5/BM25) and
// semantic (embedding cosine) ranking of the indexed chunks via reciprocal rank
// fusion, so results recall exact terms AND concepts together.
func HybridSearch(root string, p embed.Provider, indexPath string) Tool {
	return Tool{
		Name:        "hybrid_search",
		Description: "Search the semantic index combining keyword (BM25) and vector similarity via rank fusion — recalls exact terms and concepts together. Read-only.",
		Schema: object(map[string]any{
			"query": str("The search query."),
			"k":     map[string]any{"type": "integer", "description": "How many results to return (default 5)."},
		}, "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_MODEL")
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
			if err != nil || len(idx.Vectors) == 0 {
				return "", fmt.Errorf("no index found (run embed_index first): %w", err)
			}

			// Keyword ranking via an in-memory FTS5 table.
			keywordIDs, err := ftsRank(ctx, idx.Vectors, a.Query)
			if err != nil {
				return "", err
			}
			// Semantic ranking via embedding cosine.
			qv, err := p.Embed(ctx, []string{a.Query})
			if err != nil || len(qv) == 0 {
				return "", fmt.Errorf("embed query: %w", err)
			}
			var semanticIDs []string
			for _, h := range embed.TopK(qv[0], idx.Vectors, len(idx.Vectors)) {
				semanticIDs = append(semanticIDs, h.ID)
			}

			fused := reciprocalRankFusion([][]string{keywordIDs, semanticIDs}, 60)
			byID := map[string]string{}
			for _, v := range idx.Vectors {
				byID[v.ID] = v.Text
			}
			var b strings.Builder
			for i, id := range fused {
				if i >= k {
					break
				}
				snippet := byID[id]
				if len(snippet) > semanticSnippetMax {
					snippet = snippet[:semanticSnippetMax] + "…"
				}
				fmt.Fprintf(&b, "── %s\n%s\n\n", id, snippet)
			}
			if b.Len() == 0 {
				b.WriteString("(no results)\n")
			}
			return b.String(), nil
		},
	}
}

// ftsRank builds an in-memory FTS5 index from the chunk texts and returns the
// chunk ids matching the query, best (BM25) first.
func ftsRank(ctx context.Context, vecs []embed.Vector, query string) ([]string, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE VIRTUAL TABLE chunks USING fts5(id UNINDEXED, body)"); err != nil {
		return nil, fmt.Errorf("fts5 unavailable: %w", err)
	}
	stmt, err := db.PrepareContext(ctx, "INSERT INTO chunks(id, body) VALUES(?, ?)")
	if err != nil {
		return nil, err
	}
	for _, v := range vecs {
		if _, err := stmt.ExecContext(ctx, v.ID, v.Text); err != nil {
			return nil, err
		}
	}
	stmt.Close()

	rows, err := db.QueryContext(ctx, "SELECT id FROM chunks WHERE chunks MATCH ? ORDER BY bm25(chunks)", ftsQuery(query))
	if err != nil {
		return nil, nil // a malformed MATCH query just yields no keyword hits
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ftsQuery turns a free-text query into a safe FTS5 OR-of-terms match.
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	for i, f := range fields {
		fields[i] = `"` + strings.ReplaceAll(f, `"`, "") + `"`
	}
	return strings.Join(fields, " OR ")
}

// reciprocalRankFusion merges ranked id lists into one ranking. Each id scores
// sum(1/(k+rank)) across the lists it appears in; higher is better.
func reciprocalRankFusion(lists [][]string, k int) []string {
	score := map[string]float64{}
	order := []string{}
	seen := map[string]bool{}
	for _, list := range lists {
		for rank, id := range list {
			score[id] += 1.0 / float64(k+rank+1)
			if !seen[id] {
				seen[id] = true
				order = append(order, id)
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return score[order[i]] > score[order[j]] })
	return order
}
