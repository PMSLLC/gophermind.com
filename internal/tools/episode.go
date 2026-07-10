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

// RecordEpisode returns a tool that persists an episodic memory — what a task
// tried and whether it worked, plus a lesson — to a vector store, so the agent
// can recall what worked/failed on similar tasks in future sessions.
func RecordEpisode(p embed.Provider, path string) Tool {
	return Tool{
		Name:        "record_episode",
		Description: "Record what a task attempted, its outcome (success/failure), and a lesson learned, to episodic memory for recall on similar future tasks.",
		Schema: object(map[string]any{
			"task":    str("What the task was."),
			"outcome": str("The outcome, e.g. success or failure."),
			"lesson":  str("Optional lesson learned / what to do differently."),
		}, "task", "outcome"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if p == nil {
				return "", fmt.Errorf("embeddings are not configured; set GOPHERMIND_EMBED_MODEL for episodic memory")
			}
			var a struct {
				Task    string `json:"task"`
				Outcome string `json:"outcome"`
				Lesson  string `json:"lesson"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if strings.TrimSpace(a.Task) == "" {
				return "", fmt.Errorf("task is empty")
			}
			text := fmt.Sprintf("Task: %s | Outcome: %s", strings.TrimSpace(a.Task), strings.TrimSpace(a.Outcome))
			if l := strings.TrimSpace(a.Lesson); l != "" {
				text += " | Lesson: " + l
			}
			idx, err := embed.LoadIndex(path)
			if err != nil {
				idx = &embed.Index{}
			}
			vecs, err := p.Embed(ctx, []string{text})
			if err != nil || len(vecs) == 0 {
				return "", fmt.Errorf("embed episode: %w", err)
			}
			idx.Vectors = append(idx.Vectors, embed.Vector{
				ID: "ep-" + strconv.FormatInt(time.Now().UnixNano(), 36), Text: text, Values: vecs[0],
			})
			if err := idx.Save(path); err != nil {
				return "", fmt.Errorf("save episodes: %w", err)
			}
			return fmt.Sprintf("Episode recorded (%d total).", len(idx.Vectors)), nil
		},
	}
}
