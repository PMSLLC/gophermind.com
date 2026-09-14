package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// RunResumable executes steps in order, checkpointing the count of completed
// steps to progressPath after each. If a run is interrupted (error/cancel) and
// re-invoked with the same progressPath, already-completed steps are skipped —
// no lost work. On full success the progress file is cleared.
func (a *Agent) RunResumable(ctx context.Context, steps []string, run TurnFunc, progressPath string) error {
	done := loadProgress(progressPath)
	for i, step := range steps {
		if i < done {
			continue // already completed in a prior run
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.onEvent(Event{Type: "assistant", Text: fmt.Sprintf("▶ step %d/%d", i+1, len(steps))})
		if _, err := run(ctx, step); err != nil {
			return fmt.Errorf("step %d failed: %w", i+1, err)
		}
		saveProgress(progressPath, i+1)
	}
	_ = os.Remove(progressPath) // clear on full completion
	return nil
}

func loadProgress(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var p struct {
		Completed int `json:"completed"`
	}
	_ = json.Unmarshal(data, &p)
	return p.Completed
}

func saveProgress(path string, completed int) {
	data, _ := json.Marshal(struct {
		Completed int `json:"completed"`
	}{completed})
	_ = os.WriteFile(path, data, 0o644)
}
