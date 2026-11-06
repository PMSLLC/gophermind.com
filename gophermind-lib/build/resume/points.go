package resume

import (
	"context"
	"fmt"

	"gophermind/gophermind-lib/build/blackboard"
)

// Point is one place a run can be resumed from.
type Point struct {
	Step  string `json:"step"`
	Label string `json:"label"`
}

// Points lists the resume points a run offers: every planner stage that left
// output, then, once the executor has run, each node that failed, escalated or
// needs revision as node:<id>. The list is empty while any node is claimed or in
// progress. board may be nil when the run has no executor state.
func Points(ctx context.Context, runDir string, board blackboard.Blackboard, runID string) ([]Point, error) {
	pts := []Point{}
	for _, s := range Stages {
		if present(runDir, stageFiles(s)[0]) {
			pts = append(pts, Point{Step: s, Label: s})
		}
	}
	if !present(runDir, executorStatePath) || board == nil {
		return pts, nil
	}
	rows, err := board.List(ctx, runID, blackboard.Filter{})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Status == blackboard.StatusClaimed || r.Status == blackboard.StatusInProgress {
			return []Point{}, nil
		}
	}
	for _, r := range rows {
		switch r.Status {
		case blackboard.StatusFailed, blackboard.StatusEscalated, blackboard.StatusNeedsRevision:
			pts = append(pts, Point{Step: "node:" + r.NodeID, Label: fmt.Sprintf("%s (%s)", r.NodeID, r.Status)})
		}
	}
	return pts, nil
}
