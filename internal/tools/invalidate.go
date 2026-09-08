package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gophermind/internal/embed"
)

// invalidateThreshold is the cosine similarity a query must reach to retire a
// fact. Lower than supersedeThreshold because this path is deliberate: the
// model is saying "this stopped being true", and the description it gives
// rarely restates the stored wording. The retired text is echoed back, so a
// wrong match shows up in the transcript rather than disappearing silently.
const invalidateThreshold = 0.75

// InvalidateFact returns a tool that closes the validity window of a remembered
// fact the agent knows is no longer true, for the case auto-supersede cannot
// catch: the new truth is worded nothing like the old one.
func InvalidateFact(p embed.Provider, memPath string) Tool {
	return Tool{
		Name:        "invalidate_fact",
		Description: "Mark a previously remembered fact as no longer true, so it stops being retrieved. Describe the outdated fact; the closest match is retired and reported back.",
		Schema:      object(map[string]any{"query": str("A description of the fact that is no longer true.")}, "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_MODEL to use long-term memory")
			}
			var a struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			query := strings.TrimSpace(a.Query)
			if query == "" {
				return "", fmt.Errorf("query is empty")
			}

			idx, err := embed.LoadIndex(memPath)
			if err != nil {
				return "No facts in memory yet; nothing to invalidate.", nil
			}
			qv, err := p.Embed(ctx, []string{query})
			if err != nil || len(qv) == 0 {
				return "", fmt.Errorf("embed query: %w", err)
			}

			// TopK already skips facts whose window has closed, so the best hit
			// is the best *live* fact — a second invalidation of the same fact
			// finds nothing rather than retiring the next one down.
			hits := embed.TopK(qv[0], idx.Vectors, 1)
			if len(hits) == 0 || hits[0].Score < invalidateThreshold {
				return fmt.Sprintf("No live fact matched %q closely enough to invalidate.", query), nil
			}

			stamp := time.Now().UTC().Format(time.RFC3339)
			for i := range idx.Vectors {
				if idx.Vectors[i].ID != hits[0].ID {
					continue
				}
				idx.Vectors[i].ValidUntil = stamp
				if err := idx.Save(memPath); err != nil {
					return "", fmt.Errorf("save memory: %w", err)
				}
				return fmt.Sprintf("Invalidated as of %s: %s", stamp, idx.Vectors[i].Text), nil
			}
			return fmt.Sprintf("No live fact matched %q closely enough to invalidate.", query), nil
		},
	}
}
