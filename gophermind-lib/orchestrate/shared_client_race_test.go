package orchestrate

import (
	"sync"
	"testing"

	"gophermind/gophermind-lib/llm"
)

// Two tasks in one wave each build their own "isolated" task agent and set
// their own model. If those agents share one *llm.Client, the second SetModel
// overwrites the first and a task runs on a model it never chose, while its
// recorded Attempt still names the model it asked for.
//
// This is a correctness bug before it is a data race: per-task model fallback
// is meaningless if a sibling can change the model out from under a request.
func TestConcurrentTaskAgentsDoNotShareAModel(t *testing.T) {
	base := llm.New("http://127.0.0.1:1", "", "base-model", 0, false)
	r := &Runner{client: base}

	const n = 8
	models := make([]string, n)
	for i := range models {
		models[i] = "model-" + string(rune('a'+i))
	}

	var wg sync.WaitGroup
	got := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ag := r.newTaskAgent(models[i], "sys", 1)
			// Whatever this agent reports as its model must be the one it was
			// built with, no matter what its siblings did.
			got[i] = ag.LLM().Model
		}(i)
	}
	wg.Wait()

	for i := range models {
		if got[i] != models[i] {
			t.Errorf("task %d ran as %q, want %q: task agents share one client", i, got[i], models[i])
		}
	}
}
