package ui

import (
	"fmt"
	"sort"
	"strings"
)

// WorkerStatus is a snapshot of one fleet worker for the status view.
type WorkerStatus struct {
	ID      int
	Task    string
	State   string // "running" | "done" | "failed" | "idle"
	Tokens  int
	CostUSD float64
}

// RenderFleetStatus renders a top-style table of fleet workers (sorted by id),
// with a totals footer — an at-a-glance view of a multi-agent run.
func RenderFleetStatus(workers []WorkerStatus) string {
	sorted := make([]WorkerStatus, len(workers))
	copy(sorted, workers)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var b strings.Builder
	b.WriteString("ID   STATE     TOKENS   COST      TASK\n")
	var totTok int
	var totCost float64
	for _, w := range sorted {
		task := w.Task
		if len(task) > 40 {
			task = task[:39] + "…"
		}
		fmt.Fprintf(&b, "%-4d %-9s %-8d $%-8.4f %s\n", w.ID, w.State, w.Tokens, w.CostUSD, task)
		totTok += w.Tokens
		totCost += w.CostUSD
	}
	fmt.Fprintf(&b, "----\n%d workers, %d tokens, $%.4f total\n", len(sorted), totTok, totCost)
	return b.String()
}
