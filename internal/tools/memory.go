package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gophermind/internal/embed"
)

// RememberFact returns a tool that persists a salient fact to this repo's
// long-term vector memory, retrievable by relevance in future sessions.
func RememberFact(p embed.Provider, memPath string) Tool {
	return rememberTool(p, memPath, "remember_fact",
		"Save a durable fact about this project to long-term memory (embedded), retrievable by relevance in later sessions.")
}

// RememberProfile returns a tool that persists a fact to the user's GLOBAL
// profile memory (shared across repos), so preferences follow the user
// everywhere — distinct from per-repo remember_fact.
func RememberProfile(p embed.Provider, memPath string) Tool {
	return rememberTool(p, memPath, "remember_profile",
		"Save a durable preference or fact about the USER to global profile memory (shared across all repos).")
}

// rememberTool builds a fact-remembering tool over the given store path.
func rememberTool(p embed.Provider, memPath, name, desc string) Tool {
	return Tool{
		Name:        name,
		Description: desc,
		Schema:      object(map[string]any{"text": str("The fact to remember (a concise sentence).")}, "text"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_MODEL to use long-term memory")
			}
			var a struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			text := strings.TrimSpace(a.Text)
			if text == "" {
				return "", fmt.Errorf("text is empty")
			}

			// Load existing memory (missing file = empty store).
			idx, err := embed.LoadIndex(memPath)
			if err != nil {
				idx = &embed.Index{}
			}
			vecs, err := p.Embed(ctx, []string{text})
			if err != nil || len(vecs) == 0 {
				return "", fmt.Errorf("embed fact: %w", err)
			}
			id := "fact-" + strconv.FormatInt(time.Now().UnixNano(), 36)
			idx.Vectors = append(idx.Vectors, embed.Vector{ID: id, Text: text, Values: vecs[0]})
			if err := idx.Save(memPath); err != nil {
				return "", fmt.Errorf("save memory: %w", err)
			}
			return fmt.Sprintf("Remembered (%d facts in memory).", len(idx.Vectors)), nil
		},
	}
}
